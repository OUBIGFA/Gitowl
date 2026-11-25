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
    runtime_inputs = []
    for name, runtime_path in [('dugite', 'app/node_modules/dugite/git'),
                               ('@github/copilot-sdk-win32-x64', 'app/node_modules/@github/copilot-sdk-win32-x64')]:
        files = inventory(ROOT / runtime_path)
        runtime_inputs.append({**locked_package(ROOT / 'app/node_modules' / name, 'app/package-lock.json', 'node_modules/' + name),
                               'path': runtime_path, 'treeSha256': tree_hash(files), 'fileCount': len(files),
                               'delivery': 'on-demand' if name.startswith('@github/copilot') else 'bundled'})
    sqlite_manifest = json.loads((cache / 'github.com/egoist/mygo@v0.10.0/plugins/sqlite/internal/sqlitecore/mygo-plugin.json').read_text())
    sqlite = sqlite_manifest['libraries'][0]['files']['windows-amd64']
    magick_root = ROOT / '.local/imagemagick-7.1.2-36'
    magick_archive = ROOT / '.local/downloads/ImageMagick-7.1.2-36-portable-Q8-x64.7z'
    magick_archive_sha = '90e3a8ee011710cf4e570c040eaf430daf6e69b1fab084fc3214cb695f961bdc'
    if sha(magick_archive.read_bytes()) != magick_archive_sha:
        raise RuntimeError('ImageMagick archive differs from the official release digest')
    for name in ['LICENSE.txt', 'NOTICE.txt']:
        materials['third_party/licenses/native/imagemagick-' + name] = (magick_root / name).read_bytes()
    metadata = {
        'schemaVersion': 4,
        'host': 'go',
        'bundledInputs': runtime_inputs,
        'nativeLibraries': [
            {'name': 'Sqlitecore', 'version': 'MyGo-v0.10.0/SQLite-3.53.4', 'source': sqlite['url'],
             'sha256': sqlite['sha256'], 'sourceHash': sqlite['sourceHash'],
             'localPath': '.local/downloads/' + sqlite['url'].split('/')[-1]},
            {'name': 'ImageMagick', 'version': '7.1.2-36-Q8-x64',
             'source': 'https://github.com/ImageMagick/ImageMagick/releases/download/7.1.2-36/' + magick_archive.name,
             'archiveSha256': magick_archive_sha, 'sha256': sha((magick_root / 'magick.exe').read_bytes()),
             'localPath': '.local/imagemagick-7.1.2-36/magick.exe',
             'licenses': ['third_party/licenses/native/imagemagick-LICENSE.txt', 'third_party/licenses/native/imagemagick-NOTICE.txt']},
        ],
        # These are original source contracts outside the runtime-only adapter.
        # Keep Preview gated until the owner adapts and verifies them together.
        'packaging': {'previewReady': False, 'originalCore': 'desktop/desktop',
                      'presentationComplete': False, 'channel': 'research-preview',
                      'blockers': [
                          'The native CLI and protocol adapters are implemented; full upstream feature parity and source-bound frontend acceptance are not certified.',
                          'A public Gitowl update feed and signed distribution are not configured; real account OAuth/Copilot acceptance remains pending.'
                      ]},
        'node': {'role': 'build-tool-only', 'version': node_version, 'source': 'https://nodejs.org/dist/v' + node_version + '/node-v' + node_version + '-win-x64.zip', 'executableSha256': sha((node_dir / 'node.exe').read_bytes()), 'archiveSha256': sha((ROOT / '.local/downloads' / ('node-v' + node_version + '-win-x64.zip')).read_bytes()), 'license': 'MIT and bundled third-party notices', 'licenseSha256': sha((node_dir / 'LICENSE').read_bytes())},
        'git': {'dugiteVersion': json.loads((ROOT / 'app/node_modules/dugite/package.json').read_text())['version'], 'archive': embedded, 'executableSha256': sha((git / 'cmd/git.exe').read_bytes()), 'source': 'https://github.com/desktop/dugite-native', 'license': 'GPL-2.0 and bundled third-party notices', 'licenseSha256': sha((git / 'LICENSE.txt').read_bytes())},
        'goModules': sorted(modules, key=lambda m: m['module']),
        'npmLicenses': 'Generated by the existing script/licenses/update-license-dump.ts allowlist and bundled in licenses/npm-licenses.json.',
        'lockfiles': [{'path': path, 'sha256': sha((ROOT / path).read_text(encoding='utf-8').encode('utf-8'))} for path in ['package-lock.json', 'app/package-lock.json', 'app/native/go.mod', 'app/native/go.sum']],
    }
    metadata['node']['role'] = 'build-tool-only'
    metadata['npmLicenses'] = 'Only the pinned structured-clone codec is retained for one-time legacy migration, with its package notices; no JavaScript business runtime is shipped.'
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
