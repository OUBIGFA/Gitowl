"""Check the staged Git snapshot before publishing.

The checker deliberately reads Git's index and blob objects. Workspace-only
files cannot hide a staged path, credential, malformed JSON, or invalid link.
It checks repository boundaries; it does not claim that the application builds.
"""

from pathlib import Path, PurePosixPath
import json
import posixpath
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit


MAX_BYTES = 10 * 1024 * 1024
FORBIDDEN_FILE_NAMES = {"agents.md", "claude.md"}
FORBIDDEN_DIRECTORY_NAMES = {".agents", "output", "_temp"}
ALLOWED_BINARY_SUFFIXES = {
    ".car",
    ".dat",
    ".docx",
    ".dylib",
    ".gif",
    ".icns",
    ".ico",
    ".jpg",
    ".jpeg",
    ".nupkg",
    ".node",
    ".otf",
    ".pdf",
    ".png",
    ".ttf",
    ".wasm",
    ".webp",
    ".woff",
    ".woff2",
    ".zip",
}
CREDENTIAL = re.compile(
    r"(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{50,}"
    r"|AKIA[0-9A-Z]{16}|sk-(?:proj-)?[A-Za-z0-9_-]{32,}"
    r"|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)"
)
LINK = re.compile(r"!?\[[^\]\n]*\]\(([^)\n]+)\)")
RESERVED = re.compile(r"^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)", re.I)


def git(*args):
    """Use Git's index and blob APIs instead of the working tree."""
    result = subprocess.run(["git", *args], capture_output=True)
    if result.returncode:
        raise RuntimeError("Git command failed: " + " ".join(args[:2]))
    return result.stdout


def ignored(path):
    """Evaluate project rules without inheriting a developer's global excludes."""
    result = subprocess.run(
        [
            "git",
            "-c",
            "core.excludesFile=",
            "check-ignore",
            "--no-index",
            "-q",
            "--",
            path,
        ],
        capture_output=True,
    )
    if result.returncode not in (0, 1):
        raise RuntimeError("Git ignore check failed")
    return result.returncode == 0


def staged_entries():
    entries = []
    for record in git("ls-files", "--stage", "-z").split(b"\0"):
        if not record:
            continue
        metadata, raw_path = record.split(b"\t", 1)
        mode, blob, stage = metadata.decode("ascii").split()
        entries.append((raw_path.decode("utf-8"), mode, blob, stage))
    return entries


def forbidden_path(path):
    parts = PurePosixPath(path).parts
    if parts and parts[0].casefold() == "docs":
        return "root docs directory"
    if any(part.casefold() in FORBIDDEN_DIRECTORY_NAMES for part in parts):
        return "local-only directory"
    if PurePosixPath(path).name.casefold() in FORBIDDEN_FILE_NAMES:
        return "local-only instruction file"
    return None


def is_allowed_binary(path):
    if path.startswith("app/test/fixtures/"):
        return True
    return PurePosixPath(path).suffix.casefold() in ALLOWED_BINARY_SUFFIXES


def check():
    root = Path(git("rev-parse", "--show-toplevel").decode("utf-8").strip())
    if Path.cwd().resolve() != root.resolve():
        raise RuntimeError("Run the checker from the repository root")

    entries = staged_entries()
    errors = []
    texts = {}
    case_names = {}
    paths = {entry[0] for entry in entries}
    targets = set(paths)

    if not entries:
        errors.append("empty Git index: stage the intended files first")

    for path, mode, blob, stage in entries:
        parts = PurePosixPath(path).parts
        for offset in range(1, len(parts) + 1):
            prefix = "/".join(parts[:offset])
            targets.add(prefix)
            previous = case_names.setdefault(prefix.casefold(), prefix)
            if previous != prefix:
                errors.append(f"{path}: case collision with {previous}")

        for part in parts:
            if (
                RESERVED.match(part)
                or part.endswith((" ", "."))
                or re.search(r'[<>:"\\|?*\x00-\x1f]', part)
            ):
                errors.append(f"{path}: Windows-incompatible path")

        boundary = forbidden_path(path)
        if boundary:
            errors.append(f"{path}: forbidden {boundary}")
        if ignored(path):
            errors.append(f"{path}: ignored path is tracked")

        if mode == "160000":
            if stage != "0":
                errors.append(f"{path}: unresolved submodule index stage")
            continue
        if mode not in ("100644", "100755") or stage != "0":
            errors.append(f"{path}: unsupported mode or unresolved index stage")
            continue

        size = int(git("cat-file", "-s", blob))
        if size > MAX_BYTES:
            errors.append(f"{path}: exceeds 10 MiB file limit")
            continue
        data = git("cat-file", "blob", blob)
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            if not is_allowed_binary(path):
                errors.append(f"{path}: unregistered binary or invalid UTF-8")
            continue

        if "\x00" in text:
            if not is_allowed_binary(path):
                errors.append(f"{path}: unregistered binary or NUL in text")
            continue
        if text.startswith("\ufeff"):
            errors.append(f"{path}: expected UTF-8 without BOM")
        texts[path] = text
        if CREDENTIAL.search(text):
            errors.append(f"{path}: credential pattern detected (value withheld)")
        if path.endswith(".json"):
            try:
                json.loads(text)
            except (ValueError, RecursionError):
                errors.append(f"{path}: invalid JSON")

    ignore_files_match = True
    for path in paths:
        if PurePosixPath(path).name != ".gitignore":
            continue
        staged = texts.get(path)
        worktree = root / path
        if staged is None or not worktree.is_file() or worktree.read_text(encoding="utf-8") != staged:
            errors.append(f"{path} differs from staged .gitignore")
            ignore_files_match = False

    rules = [
        line.strip()
        for line in texts.get(".gitignore", "").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if not rules or rules[-1] != "**/_temp/":
        errors.append("ignore boundary: the last active root rule must be **/_temp/")
    if ignore_files_match:
        probes = (
            "docs/probe.md",
            "AGENTS.md",
            "nested/CLAUDE.md",
            ".agents/probe.txt",
            "nested/.agents/probe.txt",
            "output/probe.txt",
            "nested/output/probe.txt",
            "_temp/probe.txt",
            "references/_temp/probe.txt",
            ".local/private.txt",
            "node_modules/package/index.js",
        )
        for probe in probes:
            if not ignored(probe):
                errors.append(f"ignore boundary: {probe} is exposed")

    for path, text in texts.items():
        if not path.endswith(".md"):
            continue
        for raw in LINK.findall(text):
            destination = raw[1 : raw.index(">")].strip() if raw.startswith("<") and ">" in raw else raw.split()[0]
            url = urlsplit(destination)
            if url.scheme or url.netloc or not url.path:
                continue
            decoded = unquote(url.path)
            resolved = posixpath.normpath(posixpath.join(posixpath.dirname(path), decoded))
            if decoded.startswith("/") or resolved == ".." or resolved.startswith("../") or resolved not in targets:
                errors.append(f"{path}: missing tracked link: {destination}")

    if errors:
        print("Repository check failed:")
        for error in sorted(set(errors)):
            print("- " + error)
        return 1
    print(f"Repository check passed: {len(paths)} staged files.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(check())
    except (OSError, UnicodeError, RuntimeError, ValueError) as exc:
        print(f"Repository check could not complete: {exc}", file=sys.stderr)
        sys.exit(1)
