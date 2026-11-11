"""Exercise the native installer entry's early gates in isolated directories.

No test invokes the production builder, a user profile, or the fixed installer.
The detailed source/runtime/evidence graph tests use the existing TS runner.
"""
from pathlib import Path
import json
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SHELL = shutil.which('powershell.exe') or shutil.which('pwsh')


@unittest.skipUnless(SHELL, 'PowerShell is not available')
class NativeAcceptanceEntryTests(unittest.TestCase):
    def run_entry(self, root, *arguments):
        script = root / 'script/build-native.ps1'
        script.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / 'script/build-native.ps1', script)
        return subprocess.run(
            [SHELL, '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(script), *arguments],
            cwd=root, capture_output=True, timeout=30,
        )

    def test_missing_evidence_stops_before_any_build_output(self):
        with tempfile.TemporaryDirectory(prefix='gitowl-gate-entry-') as directory:
            root = Path(directory)
            result = self.run_entry(root)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'complete Desktop kernel presentation parity', result.stderr)
            self.assertFalse((root / 'output').exists())
            self.assertFalse((root / '_temp').exists())

    def test_preview_is_explicit_and_does_not_bypass_its_own_readiness_gate(self):
        with tempfile.TemporaryDirectory(prefix='gitowl-gate-preview-') as directory:
            root = Path(directory)
            registration = root / 'third_party/native-runtimes.json'
            registration.parent.mkdir()
            registration.write_text(json.dumps({
                'schemaVersion': 2,
                'packaging': {'previewReady': False, 'blockers': ['fixture pending review']},
            }), encoding='utf-8')
            result = self.run_entry(root, '-Preview')
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'original Desktop CLI/OAuth acceptance is pending', result.stderr)
            self.assertFalse((root / 'output').exists())
            self.assertFalse((root / '_temp').exists())


if __name__ == '__main__':
    unittest.main()
