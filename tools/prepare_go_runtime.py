"""Assemble verified native runtime resources; Node is a build tool only."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import uuid
import sys

ROOT = Path(__file__).resolve().parents[1]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def inventory(directory):
    records = [{'path': p.relative_to(directory).as_posix(), 'sha256': sha(p.read_bytes())}
               for p in directory.rglob('*') if p.is_file()]
    return sorted(records, key=lambda item: item['path'])


def validation_hash():
    """Bind validation to native code/assets, patched toolkit and real resources."""
    # Use the same repository boundary as review/build inputs. This includes
    # untracked source during development, but excludes ignored scratch files.
    names = subprocess.check_output(
        ['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', 'app/native'],
        cwd=ROOT).decode('utf-8').split('\0')
    paths = {ROOT / name for name in names if name and (ROOT / name).is_file()}
    paths.update(ROOT / p for p in (
        'tools/test_go_kernel.py', 'tools/prepare_go_runtime.py', 'tools/prepare_mygo.py',
        'references/native-legacy-fixture.cjs', 'third_party/mygo.json',
        'output/native-runtime/runtime-receipt.json'))
    toolkit = json.loads((ROOT / 'third_party/mygo.json').read_text(encoding='utf-8'))
    paths.update(ROOT / patch['path'] for patch in toolkit['patches'])
    return sha(json.dumps({p.relative_to(ROOT).as_posix(): sha(p.read_bytes()) for p in paths}, sort_keys=True).encode())


def prepare():
    manifest = json.loads((ROOT / 'third_party/native-runtimes.json').read_text(encoding='utf-8'))
    if manifest['host'] != 'go' or manifest['schemaVersion'] != 4:
        raise RuntimeError('Register the Go runtime dependencies before packaging')
    for item in manifest['bundledInputs']:
        directory = ROOT / item['path']
        if 'treeSha256' in item:
            digest = sha(json.dumps(inventory(directory), separators=(',', ':'), ensure_ascii=False).encode())
        else:
            digest = sha(directory.read_bytes())
        if digest != item.get('treeSha256', item.get('sha256')):
            raise RuntimeError('Registered runtime input changed: ' + item['path'])
    for item in manifest['nativeLibraries']:
        if sha((ROOT / item['localPath']).read_bytes()) != item['sha256']:
            raise RuntimeError('Native library checksum differs: ' + item['name'])
    for lock in manifest['lockfiles']:
        if sha((ROOT / lock['path']).read_text(encoding='utf-8').encode()) != lock['sha256']:
            raise RuntimeError('Dependency lock registration is stale: ' + lock['path'])
    # Resource assembly depends only on copied assets and their generator. Go
    # source changes are bound by the separate validation hash, not this cache.
    inputs = {}
    for path in ['tools/prepare_go_runtime.py', 'third_party/native-runtimes.json','LICENSE',
                 'app/app-info.ts', 'references/native-imagemagick-policy.xml',
                 'app/src/native-kernel/legacy-export.cjs', 'app/src/native-kernel/legacy-export.html',
                 'app/src/native-kernel/legacy-export-renderer.cjs']:
        inputs[path] = sha((ROOT / path).read_bytes())
    for item in inventory(ROOT / 'third_party/licenses/native'):
        inputs['third_party/licenses/native/' + item['path']] = item['sha256']
    input_hash = sha(json.dumps(inputs, sort_keys=True).encode())
    output = ROOT / 'output/native-runtime'
    output.mkdir(parents=True, exist_ok=True)
    sqlite = next(item for item in manifest['nativeLibraries'] if item['name'] == 'Sqlitecore')
    shutil.copy2(ROOT / sqlite['localPath'], output / 'Sqlitecore.dll')
    target = output / 'backend'
    receipt_path = output / 'runtime-receipt.json'
    if receipt_path.exists():
        receipt = json.loads(receipt_path.read_text(encoding='utf-8'))
        if receipt.get('host') == 'go' and receipt.get('inputHash') == input_hash and receipt['files'] == inventory(target):
            print('Reused identical verified Go runtime resources')
            return
    work = ROOT / '_temp/native-go-runtime' / str(uuid.uuid4())
    staging = work / 'backend'
    staging.mkdir(parents=True)
    shutil.copytree(ROOT / 'app/node_modules/dugite/git', staging / 'git')
    codec = staging / 'imagemagick'
    codec.mkdir()
    for name in ('magick.exe', 'LICENSE.txt', 'NOTICE.txt', 'sRGB.icc', 'colors.xml'):
        shutil.copy2(ROOT / '.local/imagemagick-7.1.2-36' / name, codec / name)
    shutil.copy2(ROOT / 'references/native-imagemagick-policy.xml', codec / 'policy.xml')
    lock = json.loads((ROOT / 'app/package-lock.json').read_text(encoding='utf-8'))['packages']['node_modules/@github/copilot-sdk-win32-x64']
    component = ROOT / 'app/node_modules/@github/copilot-sdk-win32-x64'
    files = inventory(component)
    for item in files:
        item['size'] = (component / item['path']).stat().st_size
    (staging / 'copilot-runtime.json').write_text(json.dumps({
        'schemaVersion': 1, 'package': '@github/copilot-sdk-win32-x64', 'version': lock['version'],
        'platform': 'win32', 'arch': 'x64', 'url': lock['resolved'], 'integrity': lock['integrity'], 'files': files,
    }, separators=(',', ':')) + '\n', encoding='utf-8')
    # Build-time app-info owns the existing OAuth registration. Do not print
    # its values or place machine paths into the generated product metadata.
    node = ROOT / '.local/toolchains' / ('node-v' + (ROOT / '.node-version').read_text().strip() + '-win-x64/node.exe')
    env = dict(os.environ, TS_NODE_PROJECT='script/tsconfig.json', NODE_ENV='production', RELEASE_CHANNEL='production')
    result = subprocess.run([str(node), '-r', 'ts-node/register', '-e',
        "const r=require('./app/app-info').getReplacements();process.stdout.write(JSON.stringify({clientID:JSON.parse(r.__OAUTH_CLIENT_ID__),clientSecret:JSON.parse(r.__OAUTH_SECRET__),oauthCallbackScheme:r.__DEV_SECRETS__?'x-github-desktop-dev-auth':'x-github-desktop-auth'}))"],
        cwd=ROOT, env=env, check=True, capture_output=True)
    (staging / 'native-launch.json').write_bytes(result.stdout + b'\n')
    migration = staging / 'migration'
    migration.mkdir()
    for name in ('legacy-export.cjs', 'legacy-export.html', 'legacy-export-renderer.cjs'):
        shutil.copy2(ROOT / 'app/src/native-kernel' / name, migration / name)
    # Only the old installed Chromium reader uses this structured-clone codec.
    # Its standard package is copied intact, without a Node executable.
    pending = ['typeson-registry']
    copied = set()
    while pending:
        name = pending.pop()
        if name in copied:
            continue
        copied.add(name)
        source = ROOT / 'app/node_modules' / name
        package = json.loads((source / 'package.json').read_text(encoding='utf-8'))
        shutil.copytree(source, migration / 'node_modules' / name, ignore=shutil.ignore_patterns('node_modules', '*.map', '*.d.ts', 'test', 'tests'))
        pending.extend(package.get('dependencies', {}))
    licenses = staging / 'licenses'
    shutil.copytree(ROOT / 'third_party/licenses/native', licenses / 'native', ignore=shutil.ignore_patterns('electron-*', 'npm_*', 'node-LICENSE'))
    shutil.copy2(ROOT / 'LICENSE', licenses / 'Gitowl-LICENSE.txt')
    files = inventory(staging)
    prohibited = {'node.exe', 'electron.exe', 'main.js', 'renderer.js', 'koffi.node', 'keytar.node'}
    if any(Path(item['path']).name in prohibited for item in files):
        raise RuntimeError('A retired JavaScript runtime entered the native package')
    output.mkdir(parents=True, exist_ok=True)
    for path in (target, staging):
        if not path.resolve().is_relative_to(ROOT):
            raise RuntimeError('Runtime path leaves the workspace')
    if target.exists():
        target.rename(work / 'retired')
    staging.rename(target)
    receipt_path.write_text(json.dumps({'schemaVersion': 4, 'host': 'go', 'inputHash': input_hash,
        'originalCore': 'desktop/desktop', 'files': files}, indent=2) + '\n', encoding='utf-8')
    print(f'Prepared native runtime resources: {len(files)} files, no Node or Electron executable')


if __name__ == '__main__':
    if '--validation-hash' in sys.argv:
        print(validation_hash())
    else:
        prepare()
