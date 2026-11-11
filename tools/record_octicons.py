"""Register/copy the exact locked upstream Octicons SVGs for native icon previews.

These are original mature-library assets, never generated/hand-drawn paths. The
production UI still uses Lucide; upstream development icon previews retain their
own library instead of substituting text or unrelated shapes.
"""
import argparse
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def collect():
    package = ROOT / 'node_modules/@primer/octicons'
    metadata = json.loads((package / 'package.json').read_text(encoding='utf-8'))
    locked = json.loads((ROOT / 'package-lock.json').read_text(encoding='utf-8'))['packages']['node_modules/@primer/octicons']
    if metadata['version'] != locked['version']:
        raise RuntimeError('Octicons differs from its locked package')
    materials = {}
    icons = []
    for file in sorted((package / 'build/svg').glob('*.svg')):
        path = 'app/native/octicons/' + file.name
        data = file.read_bytes()
        materials[path] = data
        icons.append({'id': file.stem, 'path': path, 'sha256': sha(data)})
    if not icons:
        raise RuntimeError('No official Octicons SVGs found')
    license_path = 'third_party/licenses/native/primer-octicons-LICENSE'
    license_data = (package / 'LICENSE').read_bytes()
    materials[license_path] = license_data
    receipt = {
        'package': '@primer/octicons', 'version': locked['version'],
        'source': locked['resolved'], 'integrity': locked['integrity'],
        'license': 'MIT', 'licenseFile': license_path, 'licenseSha256': sha(license_data),
        'role': 'original upstream development icon preview, rendered with MyGo ui.Icon',
        'icons': icons,
    }
    materials['third_party/octicons.json'] = (json.dumps(receipt, indent=2) + '\n').encode()
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
                raise RuntimeError('Native Octicons input is stale: ' + relative)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    print(f"Native Octicons {'check' if args.check else 'registration'}: {len(materials) - 2} original SVGs")


if __name__ == '__main__':
    main()
