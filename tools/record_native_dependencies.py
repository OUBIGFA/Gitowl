"""Register native runtime versions and license texts from verified locked inputs.

Use --check in builds. Updating a dependency requires regenerating and reviewing
this receipt; temporary module-cache paths are never written to the repository.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def json_stream(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        yield value
        text = text[end:]


def inventory(directory):
    """The same sorted POSIX-path/content inventory used by the TS adapter."""
    return [{'path': file.relative_to(directory).as_posix(), 'sha256': sha(file.read_bytes())}
            for file in sorted(directory.rglob('*')) if file.is_file()]


def tree_hash(records):
    return sha(json.dumps(sorted(records, key=lambda item: item['path']), separators=(',', ':'), ensure_ascii=False).encode())


def locked_package(directory, lockfile, key):
    installed = json.loads((directory / 'package.json').read_text(encoding='utf-8'))
    locked = json.loads((ROOT / lockfile).read_text(encoding='utf-8'))['packages'][key]
    if installed['version'] != locked['version']:
        raise RuntimeError('Installed package differs from lock: ' + key)
    return {'package': installed['name'], 'version': installed['version'],
            'lockfile': lockfile, 'lockKey': key,
            'source': locked['resolved'], 'integrity': locked.get('integrity')}


def electron_materials(materials):
    package = ROOT / 'node_modules/electron'
    locked = locked_package(package, 'package-lock.json', 'node_modules/electron')
    version = locked['version']
    name = f'electron-v{version}-win32-x64.zip'
    archive = ROOT / '.local/downloads' / name
    checksums = ROOT / '.local/downloads' / f'electron-v{version}-SHASUMS256.txt'
    base = f'https://github.com/electron/electron/releases/download/v{version}/'
    # Never infer provenance from an executable or silently download a mirror.
    # Registration requires the official release archive and its SHA256 list.
    if not archive.is_file() or not checksums.is_file():
        raise RuntimeError(f'Missing official Electron inputs: cache {base}{name} and {base}SHASUMS256.txt in .local/downloads')
    expected = json.loads((package / 'checksums.json').read_text())[name]
    entries = {line.split()[-1].lstrip('*'): line.split()[0] for line in checksums.read_text().splitlines() if line.strip()}
    if entries.get(name) != expected or sha(archive.read_bytes()) != expected:
        raise RuntimeError('Electron archive differs from official checksums / locked npm package')
    with zipfile.ZipFile(archive) as bundle:
        records = [{'path': item.filename, 'sha256': sha(bundle.read(item))}
                   for item in bundle.infolist() if not item.is_dir()]
        if tree_hash(records) != tree_hash(inventory(package / 'dist')):
            raise RuntimeError('Installed Electron dist differs from the official archive')
        licenses = []
        for name in ('LICENSE', 'LICENSES.chromium.html'):
            path = 'third_party/licenses/native/electron-' + name
            data = bundle.read(name)
            if len(data) <= 8 * 1024 * 1024:
                materials[path] = data
                licenses.append({'path': path, 'runtimePath': name, 'sha256': sha(data)})
            else:
                # Preserve the original 20 MiB notices byte-for-byte while each
                # reviewable UTF-8 input obeys the repository's 10 MiB limit.
                # Runtime still carries the one original official HTML file.
                parts = []
                remaining = data
                while remaining:
                    end = min(len(remaining), 8 * 1024 * 1024)
                    if end < len(remaining):
                        end = remaining.rfind(b'\n', 0, end) + 1
                    if not end:
                        raise RuntimeError('Cannot split Electron notices at a UTF-8 line boundary')
                    part = remaining[:end]
                    part.decode('utf-8')
                    part_path = f'third_party/licenses/native/electron-Chromium-notices-{len(parts) + 1}.html'
                    materials[part_path] = part
                    parts.append({'path': part_path, 'sha256': sha(part)})
                    remaining = remaining[end:]
                licenses.append({'runtimePath': name, 'sha256': sha(data), 'parts': parts})
    return {**locked, 'role': 'original Desktop kernel host', 'platform': 'win32-x64',
            'source': base + archive.name, 'archiveSha256': expected,
            'checksumsSource': base + 'SHASUMS256.txt', 'checksumsSha256': sha(checksums.read_bytes()),
            'executableSha256': sha((package / 'dist/electron.exe').read_bytes()),
            'distTreeSha256': tree_hash(records), 'distFileCount': len(records),
            'license': 'MIT and Chromium bundled third-party notices', 'licenses': licenses}


def bundled_inputs():
    """Register bytes copied by script/build.ts, including optional SDK/FFI assets."""
    specs = [
        ('app/node_modules/dugite/git', 'app/node_modules/dugite', 'app/package-lock.json', 'node_modules/dugite'),
        ('app/node_modules/@github/copilot-sdk-win32-x64', 'app/node_modules/@github/copilot-sdk-win32-x64', 'app/package-lock.json', 'node_modules/@github/copilot-sdk-win32-x64'),
        ('app/node_modules/@koromix/koffi-win32-x64', 'app/node_modules/@koromix/koffi-win32-x64', 'app/package-lock.json', 'node_modules/@koromix/koffi-win32-x64'),
    ]
    records = []
    for path, package, lock, key in specs:
        files = inventory(ROOT / path)
        records.append({**locked_package(ROOT / package, lock, key), 'path': path,
                        'treeSha256': tree_hash(files), 'fileCount': len(files)})
    # Resolve helpers through the exact upstream APIs, not guessed locations.
    result = subprocess.run(['node', '-e', 'console.log(JSON.stringify([require("process-proxy").getProxyCommandPath(),require("printenvz").getPrintenvzPath()]))'], cwd=ROOT, check=True, capture_output=True, text=True)
    proxy, printenvz = json.loads(result.stdout)
    helpers = [
        ('app/node_modules/desktop-trampoline/build/Release/desktop-askpass-trampoline.exe', 'app/node_modules/desktop-trampoline', 'app/package-lock.json', 'node_modules/desktop-trampoline'),
        ('app/node_modules/desktop-trampoline/build/Release/desktop-credential-helper-trampoline.exe', 'app/node_modules/desktop-trampoline', 'app/package-lock.json', 'node_modules/desktop-trampoline'),
        (Path(proxy).relative_to(ROOT).as_posix(), 'node_modules/process-proxy', 'package-lock.json', 'node_modules/process-proxy'),
        (Path(printenvz).relative_to(ROOT).as_posix(), 'node_modules/printenvz', 'package-lock.json', 'node_modules/printenvz'),
    ]
    for path, package, lock, key in helpers:
        records.append({**locked_package(ROOT / package, lock, key), 'path': path,
                        'sha256': sha((ROOT / path).read_bytes())})
    return records


def collect():
    result = subprocess.run(['go', 'list', '-deps', '-json', './...', 'github.com/egoist/mygo/cmd/mygo'], cwd=ROOT / 'app/native', check=True, capture_output=True, text=True, encoding='utf-8')
    cache = Path(os.environ.get('GOMODCACHE', ROOT / '.local/go-mod'))
    materials = {}
    modules = []
    used = {}
    for package in json_stream(result.stdout):
        module = package.get('Module')
        if module and not module.get('Main'):
            used[module['Path']] = module
    for module in used.values():
        name, version = module['Path'], module['Version']
        # Go escapes uppercase module-path letters (ProtonMail ->
        # !proton!mail); use the same canonical cache names as acceptance.
        escaped = ''.join('!' + char.lower() if 'A' <= char <= 'Z' else char
                          for char in name + '@' + version)
        source = cache / escaped
        licenses = []
        for file in sorted(source.iterdir()):
            if file.is_file() and file.name.upper().split('.')[0] in ('LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'PATENTS'):
                destination = 'third_party/licenses/native/' + name.replace('/', '_') + '-' + file.name
                data = file.read_bytes()
                materials[destination] = data
                licenses.append({'path': destination, 'sha256': sha(data)})
        if not licenses:
            raise RuntimeError('No license text found for ' + name)
        modules.append({'module': name, 'version': version, 'source': 'https://pkg.go.dev/' + name + '@' + version, 'licenses': licenses})
    node_version = (ROOT / '.node-version').read_text().strip()
    node_dir = ROOT / '.local/toolchains' / ('node-v' + node_version + '-win-x64')
    git = ROOT / 'app/node_modules/dugite/git'
    embedded = json.loads((ROOT / 'app/node_modules/dugite/script/embedded-git.json').read_text())['win32-x64']
    metadata = {
        'schemaVersion': 2,
        'electron': electron_materials(materials),
        'bundledInputs': bundled_inputs(),
        # These are original source contracts outside the runtime-only adapter.
        # Keep Preview gated until the owner adapts and verifies them together.
        'packaging': {'previewReady': False, 'originalCore': 'desktop/desktop',
                      'presentationComplete': False, 'channel': 'research-preview',
                      'blockers': [
                          'Original Windows cli.js/static github.bat still target GitHubDesktop.exe, not the MyGo launcher; CLI/deep-link acceptance is pending.',
                          'Gitowl OAuth/updates/protocol registration and original native frontend acceptance are not complete.'
                      ]},
        'node': {'role': 'build-tool-only', 'version': node_version, 'source': 'https://nodejs.org/dist/v' + node_version + '/node-v' + node_version + '-win-x64.zip', 'executableSha256': sha((node_dir / 'node.exe').read_bytes()), 'archiveSha256': sha((ROOT / '.local/downloads' / ('node-v' + node_version + '-win-x64.zip')).read_bytes()), 'license': 'MIT and bundled third-party notices', 'licenseSha256': sha((node_dir / 'LICENSE').read_bytes())},
        'git': {'dugiteVersion': json.loads((ROOT / 'app/node_modules/dugite/package.json').read_text())['version'], 'archive': embedded, 'executableSha256': sha((git / 'cmd/git.exe').read_bytes()), 'source': 'https://github.com/desktop/dugite-native', 'license': 'GPL-2.0 and bundled third-party notices', 'licenseSha256': sha((git / 'LICENSE.txt').read_bytes())},
        'goModules': sorted(modules, key=lambda m: m['module']),
        'npmLicenses': 'Generated by the existing script/licenses/update-license-dump.ts allowlist and bundled in licenses/npm-licenses.json.',
        'lockfiles': [{'path': path, 'sha256': sha((ROOT / path).read_text(encoding='utf-8').encode('utf-8'))} for path in ['package-lock.json', 'app/package-lock.json', 'app/native/go.mod', 'app/native/go.sum']],
    }
    materials['third_party/native-runtimes.json'] = (json.dumps(metadata, indent=2) + '\n').encode()
    return materials


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    materials = collect()
    for relative, data in materials.items():
        path = ROOT / relative
        if args.check:
            if not path.is_file() or path.read_bytes() != data:
                raise RuntimeError('Native dependency receipt is stale: ' + relative)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    print(f"Native dependency {'check' if args.check else 'registration'}: {len(materials)} files")


if __name__ == '__main__':
    main()
