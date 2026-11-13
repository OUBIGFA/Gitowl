"""Validate reviewable native packaging inputs, not generated executables."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import struct
import unittest

ROOT = Path(__file__).resolve().parents[2]


class NativePackagingTests(unittest.TestCase):
    def test_subset_preview_cannot_overwrite_the_fixed_delivery_installer(self):
        shell = shutil.which('powershell.exe') or shutil.which('pwsh')
        if shell is None:
            self.skipTest('PowerShell is not available on this platform')
        installer = ROOT / 'output/release/Gitowl-0.0.1-Setup.exe'
        before = installer.read_bytes() if installer.is_file() else None
        result = subprocess.run(
            [shell, '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(ROOT / 'script/build-native.ps1')],
            cwd=ROOT, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            b'complete Desktop kernel presentation parity',
            result.stderr.replace(b'\r\n', b''),
        )
        self.assertEqual(installer.read_bytes() if installer.is_file() else None, before)

    def test_preview_cannot_package_the_retired_research_runtime(self):
        shell = shutil.which('powershell.exe') or shutil.which('pwsh')
        if shell is None:
            self.skipTest('PowerShell is not available on this platform')
        installer = ROOT / 'output/release/Gitowl-0.0.1-Setup.exe'
        before = installer.read_bytes() if installer.is_file() else None
        result = subprocess.run(
            [shell, '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(ROOT / 'script/build-native.ps1'), '-Preview'],
            cwd=ROOT, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'original Desktop CLI/OAuth acceptance is pending', result.stderr)
        self.assertEqual(installer.read_bytes() if installer.is_file() else None, before)

    def test_runtime_resources_and_preview_gate_are_original_electron_only(self):
        config = json.loads((ROOT / 'app/native/mygo.json').read_text(encoding='utf-8'))
        self.assertIn('../../output/native-runtime/desktop-kernel', config['resources'])
        self.assertNotIn('../../output/native-runtime/service', config['resources'])
        manifest = json.loads((ROOT / 'third_party/native-runtimes.json').read_text(encoding='utf-8'))
        self.assertEqual(manifest['schemaVersion'], 2)
        self.assertEqual(manifest['node']['role'], 'build-tool-only')
        self.assertEqual(manifest['packaging']['originalCore'], 'desktop/desktop')
        self.assertFalse(manifest['packaging']['presentationComplete'])
        self.assertFalse(manifest['packaging']['previewReady'])
        self.assertTrue(manifest['packaging']['blockers'])
        electron = manifest['electron']
        locked = json.loads((ROOT / 'package-lock.json').read_text(encoding='utf-8'))['packages']['node_modules/electron']
        self.assertEqual(electron['version'], locked['version'])
        self.assertEqual(electron['integrity'], locked['integrity'])
        base = 'https://github.com/electron/electron/releases/download/v' + electron['version'] + '/'
        self.assertEqual(electron['source'], base + 'electron-v' + electron['version'] + '-win32-x64.zip')
        self.assertEqual(electron['checksumsSource'], base + 'SHASUMS256.txt')
        for key in ('archiveSha256', 'checksumsSha256', 'executableSha256', 'distTreeSha256'):
            self.assertRegex(electron[key], r'\A[0-9a-f]{64}\Z')
        self.assertGreater(electron['distFileCount'], 20)
        self.assertEqual({item['runtimePath'] for item in electron['licenses']}, {'LICENSE', 'LICENSES.chromium.html'})
        for item in electron['licenses']:
            if 'parts' in item:
                data = b''
                for part in item['parts']:
                    content = (ROOT / part['path']).read_bytes()
                    self.assertLess(len(content), 10 * 1024 * 1024)
                    content.decode('utf-8')
                    self.assertEqual(hashlib.sha256(content).hexdigest(), part['sha256'])
                    data += content
            else:
                data = (ROOT / item['path']).read_bytes()
            self.assertEqual(hashlib.sha256(data).hexdigest(), item['sha256'])
        packages = {item['package'] for item in manifest['bundledInputs']}
        self.assertTrue({'dugite', 'desktop-trampoline', 'process-proxy', 'printenvz', '@github/copilot-sdk-win32-x64', '@koromix/koffi-win32-x64'} <= packages)
        for item in manifest['bundledInputs']:
            lock = json.loads((ROOT / item['lockfile']).read_text(encoding='utf-8'))['packages'][item['lockKey']]
            self.assertEqual(item['version'], lock['version'])
            self.assertEqual(item['source'], lock['resolved'])
            self.assertEqual(item['integrity'], lock.get('integrity'))

    def test_fixed_product_identity_and_catalogs(self):
        config = json.loads((ROOT / 'app/native/mygo.json').read_text(encoding='utf-8'))
        self.assertEqual(config['name'], 'Gitowl')
        self.assertEqual(config['identifier'], 'io.github.oubigfa.gitowl')
        self.assertEqual(config['version'], '0.0.1')
        self.assertEqual(config['i18n']['source'], 'en')
        self.assertFalse(config['linux']['webview'])
        catalogs = {}
        for tag in ('en', 'zh-Hans', 'zh-Hant'):
            catalogs[tag] = json.loads((ROOT / 'app/native/locales' / (tag + '.json')).read_text(encoding='utf-8'))
        keys = lambda catalog: {key for key in catalog if not key.startswith('@')}
        self.assertEqual(keys(catalogs['en']), keys(catalogs['zh-Hans']))
        self.assertEqual(keys(catalogs['en']), keys(catalogs['zh-Hant']))
        for catalog in catalogs.values():
            self.assertTrue(all(isinstance(v, str) and v for k, v in catalog.items() if not k.startswith('@')))

    def test_mygo_module_lock_matches_registration(self):
        mygo = json.loads((ROOT / 'third_party/mygo.json').read_text(encoding='utf-8'))
        go_mod = (ROOT / 'app/native/go.mod').read_text(encoding='utf-8')
        # Check version declarations without requiring a toolchain download;
        # accept both single-line and grouped Go require directives.
        module = re.escape(mygo['module'])
        versions = re.findall(r'(?m)^\s*(?:require\s+)?' + module + r'\s+(v\S+)', go_mod)
        self.assertEqual(versions, [mygo['version']])
        self.assertEqual(re.findall(r'(?m)^go\s+(\S+)', go_mod), [mygo['go']])
        self.assertNotRegex(go_mod, r'(?m)^\s*(?:replace\s+)?' + module + r'(?:\s+v\S+)?\s+=>')
        self.assertRegex(mygo['version'], r'\Av\d+\.\d+\.\d+\Z')
        self.assertRegex(mygo['revision'], r'\A[0-9a-f]{40}\Z')
        self.assertRegex(mygo['archive_sha256'], r'\A[0-9a-f]{64}\Z')

    def test_registered_patch_and_license_bytes(self):
        mygo = json.loads((ROOT / 'third_party/mygo.json').read_text(encoding='utf-8'))
        self.assertTrue(mygo['patches'])
        paths = []
        for patch in mygo['patches']:
            self.assertEqual(hashlib.sha256((ROOT / patch['path']).read_bytes()).hexdigest(), patch['sha256'])
            self.assertTrue(all(item['after'] for item in patch['files']))
            paths.extend(item['path'] for item in patch['files'])
        self.assertEqual(len(paths), len(set(paths)), 'Patches must have disjoint upstream inputs')
        runtime = json.loads((ROOT / 'third_party/native-runtimes.json').read_text(encoding='utf-8'))
        self.assertEqual(runtime['node']['version'], (ROOT / '.node-version').read_text().strip())
        modules = {module['module']: module for module in runtime['goModules']}
        self.assertEqual(modules['github.com/egoist/mygo']['version'], mygo['version'])
        for module in modules.values():
            self.assertTrue(module['licenses'])
            for record in module['licenses']:
                self.assertEqual(hashlib.sha256((ROOT / record['path']).read_bytes()).hexdigest(), record['sha256'])
        for record in runtime['lockfiles']:
            self.assertEqual(hashlib.sha256((ROOT / record['path']).read_text(encoding='utf-8').encode('utf-8')).hexdigest(), record['sha256'])

    def test_registered_native_icons(self):
        manifest = json.loads((ROOT / 'third_party/native-icons.json').read_text(encoding='utf-8'))
        self.assertEqual(manifest['name'], 'lucide-static')
        self.assertTrue(manifest['files'])
        for item in manifest['files']:
            self.assertEqual(hashlib.sha256((ROOT / item['path']).read_bytes()).hexdigest(), item['sha256'])
        self.assertEqual(hashlib.sha256((ROOT / manifest['licenseFile']).read_bytes()).hexdigest(), manifest['licenseSha256'])

    def test_logo_is_the_user_supplied_original(self):
        research = json.loads((ROOT / 'third_party/research-sources.json').read_text(encoding='utf-8'))
        logo = research['product_logo']
        self.assertEqual(hashlib.sha256((ROOT / logo['tracked_source']).read_bytes()).hexdigest(), logo['sha256'])

    def test_brand_icon_sizes_and_package_resources(self):
        manifest = json.loads((ROOT / 'references/native-brand-icons.json').read_text(encoding='utf-8'))
        self.assertEqual(hashlib.sha256((ROOT / manifest['source']).read_bytes()).hexdigest(), manifest['sourceSha256'])
        sizes = set()
        for asset in manifest['assets']:
            data = (ROOT / asset['path']).read_bytes()
            self.assertEqual(hashlib.sha256(data).hexdigest(), asset['sha256'])
            if asset['path'].endswith('.png'):
                self.assertEqual(data[:8], b'\x89PNG\r\n\x1a\n')
                self.assertEqual(struct.unpack('>II', data[16:24]), (asset['size'], asset['size']))
                sizes.add(asset['size'])
            else:
                reserved, kind, count = struct.unpack('<HHH', data[:6])
                self.assertEqual((reserved, kind), (0, 1))
                ico_sizes = {data[6 + 16 * i] or 256 for i in range(count)}
        self.assertEqual(sizes, {16, 20, 24, 28, 32, 40, 48, 64, 96, 128, 192, 256, 512, 1024})
        self.assertEqual(ico_sizes, {size for size in sizes if size <= 256})
        config = json.loads((ROOT / 'app/native/mygo.json').read_text(encoding='utf-8'))
        self.assertIn('../static/logos/gitowl/icons', config['resources'])


if __name__ == '__main__':
    unittest.main()
