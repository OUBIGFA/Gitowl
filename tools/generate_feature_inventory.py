#!/usr/bin/env python3
"""Generate the declaration inventory; run --check to reject stale bytes.

This is deliberately a source-structure parser, not a TypeScript compiler. It
accepts top-level exported declarations, one enum member / union alternative /
IPC property per line, and indented continuation lines. Comments are ignored;
strings must be unescaped single/double-quoted literals. Unsupported declaration
shapes fail closed. Runtime availability and callback bodies are not inferred.
"""

import argparse
from collections import Counter
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
BASELINE = "a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7"
OUTPUT = "references/upstream-features.json"
SOURCES = {
    "menu": "app/src/main-process/menu/menu-event.ts",
    "popup": "app/src/models/popup.ts",
    "preferences": "app/src/models/preferences.ts",
    "ipc": "app/src/lib/ipc-shared.ts",
    "url": "app/src/lib/parse-app-url.ts",
    "retry": "app/src/models/retry-actions.ts",
    "banner": "app/src/models/banner.ts",
    "state": "app/src/lib/app-state.ts",
}
IDENT = r"[A-Za-z_$][\w$]*"
LITERAL = r"(?:'[^'\\\r\n]*'|\"[^\"\\\r\n]*\")"


class ParseError(ValueError):
    """The source no longer conforms to the explicitly supported grammar."""


def uncomment(text):
    """Preserve lines and quoted text so comment contents cannot invent entries."""
    pattern = re.compile(r"'[^'\\\r\n]*(?:\\.[^'\\\r\n]*)*'|\"[^\"\\\r\n]*(?:\\.[^\"\\\r\n]*)*\"|//[^\n]*|/\*[\s\S]*?\*/")
    return pattern.sub(
        lambda m: re.sub(r"[^\n]", " ", m[0]) if m[0].startswith("/") else m[0],
        text,
    )


def declaration(text, kind, symbol, block=False):
    """Require an exact top-level header and an explicit unindented boundary."""
    suffix = r" =" if kind == "type" else ""
    opening = r" \{" if block else ""
    header = rf"^export {kind} {re.escape(symbol)}{suffix}{opening}[ \t]*\n"
    matches = list(re.finditer(header, text, re.M))
    if len(matches) != 1:
        raise ParseError(f"expected one supported {kind} declaration: {symbol}")
    tail = text[matches[0].end():]
    end = re.search(r"^\S", tail, re.M)
    body = tail[:end.start()] if end else tail
    if block and (end is None or not re.match(r"\}[ \t]*(?:\n|$)", tail[end.start():])):
        raise ParseError(f"missing standalone closing brace: {symbol}")
    return body


def unique(items, symbol):
    if not items or len(items) != len(set(items)):
        raise ParseError(f"empty or duplicate entries: {symbol}")
    return items


def union(text, symbol):
    body = declaration(text, "type", symbol)
    values = []
    for line in body.splitlines():
        if not line.strip():
            continue
        match = re.fullmatch(rf"\s+\|\s+({LITERAL}|{IDENT})\s*", line)
        if not match:
            raise ParseError(f"unsupported union alternative in {symbol}: {line.strip()}")
        values.append(match[1])
    return unique(values, symbol)


def enum_members(text, symbol):
    body = declaration(text, "enum", symbol, block=True)
    result = []
    next_number = 0
    for line in body.splitlines():
        if not line.strip():
            continue
        match = re.fullmatch(rf"\s+({IDENT})(?:\s*=\s*({LITERAL}|\d+))?,\s*", line)
        if not match:
            raise ParseError(f"unsupported enum member in {symbol}: {line.strip()}")
        name, value = match.groups()
        if value is None:
            if next_number is None:
                raise ParseError(f"implicit member after string value: {symbol}.{name}")
            value = next_number
        elif value.isdigit():
            value = int(value)
        else:
            value = value[1:-1]
        next_number = value + 1 if isinstance(value, int) else None
        result.append((name, value))
    unique([name for name, _ in result], symbol)
    return result


def ipc_members(text, symbol):
    """Read only direct properties, validating balanced multiline signatures.

    Properties use exactly two spaces. Continuations are deeper, except a
    closing function parenthesis. Index signatures, intersections and optional
    keys intentionally require a parser update rather than silently disappearing.
    """
    body = declaration(text, "type", symbol, block=True)
    properties = []
    for line in body.splitlines():
        if not line.strip():
            continue
        match = re.fullmatch(rf"  ({LITERAL}|{IDENT}): (.+)", line)
        if match:
            properties.append([match[1].strip("'\""), match[2]])
        elif properties and (line.startswith("    ") or line.startswith("  )")):
            properties[-1][1] += " " + line.strip()
        else:
            raise ParseError(f"unsupported IPC property in {symbol}: {line.strip()}")
    unique([key for key, _ in properties], symbol)
    for key, signature in properties:
        stack = []
        # The current IPC signatures contain identifiers, generic types and
        # function/object/array types. Literal/template types need explicit work.
        tokens = re.findall(rf"=>|{IDENT}|[<>{{}}()\[\]:;,?.|]", signature)
        if "".join(tokens) != re.sub(r"\s", "", signature):
            raise ParseError(f"unsupported signature: {symbol}.{key}")
        for token in tokens:
            if token in ("(", "[", "{", "<"):
                stack.append(token)
            elif token in (")", "]", "}", ">"):
                if not stack or stack.pop() != {")": "(", "]": "[", "}": "{", ">": "<"}[token]:
                    raise ParseError(f"unbalanced signature: {symbol}.{key}")
        if stack or not (signature.startswith("(") and "=>" in tokens or
                         re.fullmatch(rf"{IDENT}<.*>", signature)):
            raise ParseError(f"unsupported callable signature: {symbol}.{key}")
    return properties


def inventory(root):
    """Extract stable declaration identities; presence never implies migration."""
    texts = {key: uncomment((root / path).read_text(encoding="utf-8"))
             for key, path in SOURCES.items()}
    entries = []

    def add(source, category, symbol, member, **details):
        entries.append({"id": f"{symbol}:{member}", "source_file": SOURCES[source],
                        "symbol": symbol, "member": member, "category": category,
                        **details})

    menu = union(texts["menu"], "MenuEvent")
    if "TestMenuEvent" not in menu:
        raise ParseError("MenuEvent must include the supported TestMenuEvent alias")
    alias = re.findall(r"^export type TestMenuEvent = typeof TestMenuEvents\[number\]\s*$",
                       texts["menu"], re.M)
    arrays = re.findall(r"^const TestMenuEvents = \[\n([\s\S]*?)^\] as const\s*$",
                        texts["menu"], re.M)
    if len(alias) != 1 or len(arrays) != 1:
        raise ParseError("unsupported TestMenuEvent alias or const array")
    tests = []
    for line in arrays[0].splitlines():
        if not line.strip():
            continue
        match = re.fullmatch(rf"\s+({LITERAL}),\s*", line)
        if not match:
            raise ParseError("unsupported TestMenuEvents array element")
        tests.append(match[1][1:-1])
    unique(tests, "TestMenuEvents")
    literals = []
    for item in menu:
        if item == "TestMenuEvent":
            continue
        if not re.fullmatch(LITERAL, item):
            raise ParseError(f"unsupported MenuEvent reference: {item}")
        literals.append(item[1:-1])
    unique(literals, "MenuEvent literals")
    for member in sorted(set(literals + tests)):
        add("menu", "menu-event", "MenuEvent", member, test_only=member in tests)
    for source, symbol, category in (
        ("popup", "PopupType", "popup"),
        ("preferences", "PreferencesTab", "preferences-tab"),
        ("retry", "RetryActionType", "failure-recovery"),
        ("banner", "BannerType", "banner"),
        ("state", "FoldoutType", "foldout"),
        ("state", "RepositorySectionTab", "repository-tab"),
    ):
        for name, value in enum_members(texts[source], symbol):
            add(source, category, symbol, name, value=value)
    for symbol, category in (("RequestChannels", "ipc-request"),
                             ("RequestResponseChannels", "ipc-request-response")):
        for name, signature in ipc_members(texts["ipc"], symbol):
            add("ipc", category, symbol, name, signature=signature)
    for symbol in union(texts["url"], "URLActionType"):
        if not re.fullmatch(IDENT, symbol):
            raise ParseError("URLActionType must reference named interfaces")
        body = declaration(texts["url"], "interface", symbol, block=True)
        names = re.findall(rf"^  readonly name: ({LITERAL})[ \t]*$", body, re.M)
        if len(names) != 1:
            raise ParseError(f"expected a single literal name discriminant: {symbol}")
        # A deep-link action includes its payload, not just the action name.
        # Losing OAuth state or the target branch must also invalidate the contract.
        fields = []
        for line in body.splitlines():
            if not line.strip():
                continue
            match = re.fullmatch(rf"  readonly ({IDENT})(\??): (\S(?:.*\S)?)[ \t]*", line)
            if not match:
                raise ParseError(f"unsupported URL action property: {symbol}: {line.strip()}")
            fields.append({"name": match[1], "optional": bool(match[2]),
                           "type": re.sub(r"\s+", " ", match[3])})
        unique([field["name"] for field in fields], symbol)
        add("url", "url-action", symbol, names[0][1:-1], union="URLActionType", fields=fields)
    unique([entry["id"] for entry in entries], "inventory IDs")
    entries.sort(key=lambda entry: (entry["category"], entry["id"]))
    return {
        "schema_version": 1,
        "upstream_revision": BASELINE,
        "source_baseline": {
            "kind": "local-git-baseline",
            "revision": BASELINE,
            "remote_content_verified": False,
            "description": "Declared local Git comparison baseline; entries are extracted from working-tree sources. No online upstream identity verification is claimed.",
        },
        "migration_status": "not-assessed",
        "scope": {
            "parser": "Restricted TypeScript source structures; unsupported declarations fail. See tools/generate_feature_inventory.py.",
            "context_menus": "Covers RequestResponseChannels:show-contextual-menu transport and shared menu/popup declarations. Dynamic IMenuItem action callbacks in app/src/ui/** are not enumerated; app/src/lib/menu-item.ts dispatches them by index.",
            "failure_recovery": "Covers all PopupType, BannerType and RetryActionType members plus IPC error channels; runtime retry execution and availability are not verified.",
            "exclusions": ["Dynamic context-menu callback bodies", "Popup, banner and foldout payloads and callback implementations", "Platform and account availability conditions", "Keyboard shortcut bindings", "Runtime behavior and MyGo migration acceptance"],
        },
        "counts": dict(sorted(Counter(entry["category"] for entry in entries).items())),
        "features": entries,
    }


def render(root):
    return (json.dumps(inventory(root), ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="compare exact generated UTF-8 bytes without writing")
    parser.add_argument("--root", type=Path, default=ROOT, help="source repository root")
    args = parser.parse_args(argv)
    target = args.root / OUTPUT
    try:
        expected = render(args.root)
        if args.check:
            if not target.exists() or target.read_bytes() != expected:
                print(f"stale feature inventory: {OUTPUT}; run tools/generate_feature_inventory.py", file=sys.stderr)
                return 1
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(expected)
        print(f"feature inventory {'checked' if args.check else 'generated'}: {len(json.loads(expected)['features'])} entries")
        return 0
    except (OSError, ValueError) as error:
        print(f"feature inventory error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
