"""Validate native business/UI behavior against the prepared Go resources."""
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid
from prepare_go_runtime import validation_hash

ROOT = Path(__file__).resolve().parents[1]


def main():
    env = dict(os.environ, PYTHONUTF8='1', GOWORK='off', TEST_ENV='1',
               GOMODCACHE=str(ROOT / '.local/go-mod'), GOCACHE=str(ROOT / '.local/go-build'))
    manifest = json.loads((ROOT / 'third_party/native-runtimes.json').read_text(encoding='utf-8'))
    sqlite = next(item for item in manifest['nativeLibraries'] if item['name'] == 'Sqlitecore')
    env['MYGO_NATIVE_SQLITECORE'] = str(ROOT / sqlite['localPath'])
    env['GITOWL_MAGICK'] = str(ROOT / 'output/native-runtime/backend/imagemagick/magick.exe')
    env['GITOWL_TEST_GIT'] = str(ROOT / 'output/native-runtime/backend/git/cmd/git.exe')
    env['GITOWL_COPILOT_ARCHIVE'] = str(ROOT / '.local/downloads/copilot-sdk-win32-x64-1.0.27-preview.1.tgz')
    env['GITOWL_COPILOT_MANIFEST'] = str(ROOT / 'output/native-runtime/backend/copilot-runtime.json')
    for key in ['GITOWL_KERNEL_ENTRY', 'GITOWL_KERNEL_NODE', 'GITOWL_NODE_RUNTIME', 'GITOWL_NATIVE_RUNTIME']:
        env.pop(key, None)
    subprocess.run([sys.executable, 'tools/prepare_go_runtime.py'], cwd=ROOT, env=env, check=True)
    # Use this run's original codec inventory and embedded Git even for tests
    # that fall back to PATH. All repositories/profiles remain isolated.
    env['PATH'] = str(ROOT / 'output/native-runtime/backend/git/cmd') + os.pathsep + env.get('PATH', '')
    fixture = ROOT / '_temp/native-validation' / str(uuid.uuid4()) / 'legacy'
    (fixture / 'backend-data').mkdir(parents=True)
    version = (ROOT / '.node-version').read_text().strip()
    node = ROOT / '.local/toolchains' / f'node-v{version}-win-x64/node.exe'
    subprocess.run([str(node), '--conditions=import', '--experimental-webstorage', '--localstorage-file=' + str(fixture / 'backend-data/settings.sqlite'),
                    'references/native-legacy-fixture.cjs', str(fixture)], cwd=ROOT, env=env, check=True)
    env['GITOWL_LEGACY_FIXTURE'] = str(fixture)
    modfile = subprocess.check_output([sys.executable, 'tools/prepare_mygo.py'], cwd=ROOT, env=env, text=True).strip()
    source_hash = validation_hash()
    command = ['go', '-C', 'app/native', 'test', '-json', '-modfile=' + modfile, './...']
    subprocess.run(command, cwd=ROOT, env=env, check=True)
    if validation_hash() != source_hash:
        raise RuntimeError('Native validation inputs changed during tests; rerun the affected checks')
    receipt = json.loads((ROOT / 'output/native-runtime/runtime-receipt.json').read_text(encoding='utf-8'))
    (ROOT / 'output/native-runtime/go-runtime-validation.json').write_text(json.dumps({
        'host': 'go', 'inputHash': receipt['inputHash'], 'validationHash': source_hash, 'passed': True,
        'checks': ['direct-git', 'bounded-object-cache', 'unchanged-refresh', 'sqlite-migration',
                   'native-codecs', 'copilot-go-sdk', 'native-ui'],
    }, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
