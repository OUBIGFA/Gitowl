"""Copy and register the Solar icons used by the native UI.

The source is the locked solar-icons archive (one exact upstream revision),
read in place without extracting it. Every icon keeps its upstream geometry;
only the hard-coded black paint becomes currentColor so MyGo ui.Icon can tint
it with the theme. The receipt records the upstream and local SHA-256 of each
file, plus the licenses the icons are distributed under.

    python tools/record_solar_icons.py [--archive PATH] [--check]
"""
import argparse
import hashlib
import json
import re
import tarfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REVISION = 'ab241e1bf578dcfe45b0197aee59bd9a08b5fe68'
ARCHIVE = ROOT / '.local/downloads' / f'solar-icons-{REVISION}.tar.gz'
PREFIX = f'solar-icons-{REVISION}/'
RECEIPT = 'third_party/native-icons.json'
LICENSES = {
    'LICENSE': 'third_party/licenses/native/solar-icons-LICENSE',
    'LICENSE-THIRD-PARTY': 'third_party/licenses/native/solar-icons-LICENSE-THIRD-PARTY',
}
LUCIDE_LICENSE = 'third_party/licenses/native/lucide-LICENSE'

# The Gitowl shell keeps its semantic icon names; each maps to one Solar icon.
APP_DIR = 'app/native/icons'
APP = {
    'accessibility': 'ui/accessibility',
    'arrow-down': 'arrows/arrow-down',
    'arrow-left': 'arrows/arrow-left',
    'arrow-up': 'arrows/arrow-up',
    'arrow-up-right': 'arrows/arrow-right-up',
    'bell': 'notifications/bell',
    'bot': 'devices/bot',
    'check': 'ui/check',
    'chevron-down': 'arrows/alt-arrow-down',
    'chevron-right': 'arrows/alt-arrow-right',
    'circle-check': 'ui/check-circle',
    'cloud-upload': 'weather/cloud-upload',
    'copy': 'ui/copy',
    'download': 'arrows-action/download-minimalistic',
    'ellipsis': 'ui/menu-dots',
    'file': 'files/file',
    'file-code': 'files/file-code',
    'file-text': 'files/file-text',
    'folder': 'folders/folder',
    'folder-open': 'folders/folder-open',
    'git-branch': 'it/git-branch',
    'history': 'time/history',
    'image': 'video/gallery',
    'palette': 'tools/palette',
    'panel-left-close': 'it/panel-left-close',
    'panel-left-open': 'it/panel-left-open',
    'plug': 'devices/plug',
    'plus': 'ui/add',
    'refresh-cw': 'arrows/refresh',
    'search': 'search/magnifier',
    'server': 'devices/server',
    'settings': 'settings/settings',
    'shield-check': 'security/shield-check',
    'sliders-horizontal': 'settings/tuning-2',
    'tag': 'money/tag',
    'user-round': 'users/user-rounded',
    'x': 'ui/close',
}

# The Markdown editor names its files after the Solar icons themselves.
EDITOR_DIR = 'app/native/internal/arcmd/editor/icons'
EDITOR = [
    'arrows/alt-arrow-down', 'arrows/alt-arrow-right', 'arrows/arrow-down', 'arrows/arrow-up',
    'arrows/double-alt-arrow-left', 'arrows/square-alt-arrow-right', 'arrows/transfer-horizontal',
    'arrows-action/arrow-down-to-line', 'arrows-action/arrow-left-to-line',
    'arrows-action/arrow-right-to-line', 'arrows-action/arrow-up-to-line', 'arrows-action/move-vertical',
    'business/table', 'business/table-cells-merge', 'business/table-cells-split',
    'devices/lightbulb', 'faces/smile-circle', 'it/code', 'it/code-square', 'it/workflow',
    'list/list', 'list/list-check', 'list/list-indent-decrease', 'list/list-indent-increase',
    'list/list-ordered', 'list/list-unordered', 'list/sort-from-bottom-to-top', 'list/sort-from-top-to-bottom',
    'messages/chat-square-warning', 'messages/pen', 'messages/pen-2', 'school/bookmark',
    'security/lock', 'security/lock-unlocked', 'settings/columns-2',
    'text-formatting/link', 'text-formatting/paragraph', 'text-formatting/quote',
    'text-formatting/text-align-center', 'text-formatting/text-align-left', 'text-formatting/text-align-right',
    'text-formatting/text-bold', 'text-formatting/text-cross', 'text-formatting/text-format',
    'text-formatting/text-italic', 'text-formatting/text-underline',
    'tools/paint-bucket', 'tools/palette', 'ui/add', 'ui/add-square', 'ui/check', 'ui/close', 'ui/copy',
    'ui/copy-add', 'ui/danger', 'ui/danger-triangle', 'ui/forbidden-circle', 'ui/grip-vertical',
    'ui/info-circle', 'ui/minus', 'ui/scissors', 'ui/trash-bin-minimalistic', 'video/gallery',
]
# Linear has no filled state; a set bookmark uses the same glyph's Bold style.
EDITOR_STYLED = {'bookmark-bold': ('school/bookmark', 'Bold')}

# Solar has no numbered headings, sigma or wrap-text glyph. These editor
# icons stay Lucide's, drawn at Solar's 1.5 stroke so the weights match.
RETAINED_LUCIDE = ['heading-1', 'heading-2', 'heading-3', 'heading-4', 'heading-5', 'sigma', 'wrap-text']


def sha(data):
    return hashlib.sha256(data).hexdigest()


def tint(data, name):
    text = data.decode('utf-8').replace('\r\n', '\n')
    text = text.replace('stroke="black"', 'stroke="currentColor"').replace('fill="black"', 'fill="currentColor"')
    if re.search(r'"(black|white|#[0-9a-fA-F]{3,8})"', text) or 'currentColor' not in text:
        raise RuntimeError(f'{name}: unexpected paint in the upstream SVG')
    return text.encode('utf-8')


def entries():
    for name, source in APP.items():
        yield APP_DIR, name, source, 'Linear'
    for source in EDITOR:
        yield EDITOR_DIR, source.split('/')[1], source, 'Linear'
    for name, (source, style) in EDITOR_STYLED.items():
        yield EDITOR_DIR, name, source, style


def collect(archive):
    materials = {}
    files = []
    with tarfile.open(archive, 'r:gz') as tar:
        def read(member):
            handle = tar.extractfile(PREFIX + member)
            if handle is None:
                raise RuntimeError('Missing in the Solar archive: ' + member)
            return handle.read()

        for directory, name, source, style in entries():
            category, base = source.split('/')
            upstream = f'packages/core/svgs/{category}/{style}/{base}.svg'
            original = read(upstream)
            data = tint(original, upstream)
            path = f'{directory}/{name}.svg'
            if path in materials:
                raise RuntimeError('Duplicate icon: ' + path)
            materials[path] = data
            files.append({'path': path, 'source': upstream, 'upstreamSha256': sha(original), 'sha256': sha(data)})
        licenses = {}
        for member, path in LICENSES.items():
            data = read(member)
            materials[path] = data
            licenses[member] = {'path': path, 'sha256': sha(data)}
    lucide = (ROOT / LUCIDE_LICENSE).read_bytes()
    receipt = {
        'name': 'solar-icons',
        'style': 'Linear',
        'source': 'https://github.com/saoudi-h/solar-icons',
        'revision': REVISION,
        'archive': f'solar-icons-{REVISION}.tar.gz',
        'archiveSha256': sha(Path(archive).read_bytes()),
        'license': 'MIT (Hakim Saoudi); icon artwork CC BY 4.0',
        'attribution': 'Solar Icons Set by 480 Design (https://www.figma.com/community/file/1166831539721848736), CC BY 4.0',
        'licenseFile': licenses['LICENSE']['path'],
        'licenseSha256': licenses['LICENSE']['sha256'],
        'thirdPartyLicenseFile': licenses['LICENSE-THIRD-PARTY']['path'],
        'thirdPartyLicenseSha256': licenses['LICENSE-THIRD-PARTY']['sha256'],
        'modifications': 'black stroke/fill paint replaced by currentColor for MyGo ui.Icon tinting; geometry unchanged; line endings LF',
        'generator': 'tools/record_solar_icons.py',
        'files': files,
        'retainedLucide': {
            'reason': 'no Solar equivalent; drawn at stroke-width 1.5 to match Solar Linear',
            'path': 'app/native/internal/arcmd/editor/icons.go',
            'icons': RETAINED_LUCIDE,
            'licenseFile': LUCIDE_LICENSE,
            'licenseSha256': sha(lucide),
        },
    }
    materials[RECEIPT] = (json.dumps(receipt, indent=2, ensure_ascii=False) + '\n').encode('utf-8')
    return materials


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--archive', default=str(ARCHIVE))
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    materials = collect(args.archive)
    for directory in (APP_DIR, EDITOR_DIR):
        stale = [p for p in (ROOT / directory).glob('*.svg') if f'{directory}/{p.name}' not in materials]
        if stale and args.check:
            raise RuntimeError('Unregistered native icon: ' + str(stale[0].relative_to(ROOT)))
        for path in stale:
            path.unlink()
    for relative, data in materials.items():
        path = ROOT / relative
        if args.check:
            if not path.is_file() or path.read_bytes() != data:
                raise RuntimeError('Native Solar icon input is stale: ' + relative)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    print(f'{len(materials)} files {"verified" if args.check else "written"}')


if __name__ == '__main__':
    main()
