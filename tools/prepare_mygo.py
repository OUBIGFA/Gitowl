"""Apply registered MyGo patches to an ignored, verified source copy.

Never alter GOMODCACHE or the checked-in Go module. The generated modfile is
used by tests and packaging so both consume the exact same patched source.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def copy_source(source, destination):
    # Earlier local copies may have inherited Go's read-only cache attributes.
    target = Path(destination)
    if target.is_file():
        target.chmod(stat.S_IREAD | stat.S_IWRITE)
    return shutil.copyfile(source, destination)


def prepare():
    manifest = json.loads((ROOT / 'third_party/mygo.json').read_text(encoding='utf-8'))
    module = manifest['module'] + '@' + manifest['version']
    cache = Path(os.environ.get('GOMODCACHE', ROOT / '.local/go-mod'))
    source = cache / module
    archive = cache / ('cache/download/' + manifest['module'] + '/@v/' + manifest['version'] + '.zip')
    if not source.is_dir() or not archive.is_file():
        subprocess.run(['go', 'mod', 'download', module], cwd=ROOT / 'app/native', check=True)
    if digest(archive) != manifest['archive_sha256']:
        raise RuntimeError('MyGo module archive does not match the registered source')
    patches = manifest['patches']
    files = [entry for patch in patches for entry in patch['files']]
    for patch in patches:
        if digest(ROOT / patch['path']) != patch['sha256']:
            raise RuntimeError('MyGo patch does not match its registered checksum: ' + patch['path'])
    for entry in files:
        if entry['before'] is not None and digest(source / entry['path']) != entry['before']:
            raise RuntimeError('Review MyGo patch against changed upstream file: ' + entry['path'])
    output = ROOT / ('.local/mygo-gitowl-' + manifest['version'].removeprefix('v'))
    stamp = output / '.gitowl-patch'
    fingerprint = ':'.join([manifest['archive_sha256']] + [patch['sha256'] for patch in patches])
    valid = stamp.is_file() and stamp.read_text(encoding='ascii') == fingerprint
    valid = valid and all((output / item['path']).is_file() and digest(output / item['path']) == item['after'] for item in files)
    if not valid:
        # Copy bytes, not cache permissions: Go makes cached sources read-only.
        shutil.copytree(source, output, dirs_exist_ok=True, copy_function=copy_source)
        for entry in files:
            if entry['before'] is None:
                (output / entry['path']).unlink(missing_ok=True)
        directory = '--directory=' + output.relative_to(ROOT).as_posix()
        for patch in patches:
            path = str(ROOT / patch['path'])
            subprocess.run(['git', 'apply', '--check', directory, path], cwd=ROOT, check=True)
            subprocess.run(['git', 'apply', directory, path], cwd=ROOT, check=True)
        for entry in files:
            if digest(output / entry['path']) != entry['after']:
                raise RuntimeError('Unexpected patched source: ' + entry['path'])
        stamp.write_text(fingerprint, encoding='ascii')
    native = ROOT / 'app/native'
    modfile = ROOT / '.local/gitowl-native.mod'
    shutil.copyfile(native / 'go.mod', modfile)
    shutil.copyfile(native / 'go.sum', modfile.with_suffix('.sum'))
    subprocess.run(['go', 'mod', 'edit', '-modfile=' + str(modfile), '-replace=' + manifest['module'] + '=' + str(output)], cwd=native, check=True)
    return modfile


if __name__ == '__main__':
    print(prepare())
