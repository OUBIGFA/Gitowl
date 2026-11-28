"""Native validation must cover its exact source/assets, never a filtered run."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools'))
import prepare_go_runtime as runtime
import test_go_kernel as runner


class NativeValidationTests(unittest.TestCase):
    def test_inherited_filter_cannot_produce_full_validation(self):
        # Do not clear/repopulate Windows' native environment: empty inherited
        # Git config values must remain present for subsequent child processes.
        with mock.patch.object(runner.os, 'environ', dict(os.environ, GOFLAGS='-run=OnlyOneTest')), \
                mock.patch.object(sys, 'argv', ['test_go_kernel.py']), \
                mock.patch.object(runner.subprocess, 'run') as run:
            with self.assertRaisesRegex(RuntimeError, 'GOFLAGS'):
                runner.main()
            run.assert_not_called()

    def test_only_full_selection_can_write_build_receipt(self):
        self.assertTrue(runner.test_selection([])[2])
        self.assertTrue(runner.test_selection(['--package', './...'])[2])
        for args in [['--package', './internal/gitcli'], ['--run', 'TestNative'],
                     ['--bench', 'BenchmarkCache'], ['--run', ''],
                     ['--package', './internal/backend', '--package', './internal/gitcli']]:
            with self.subTest(args=args):
                self.assertFalse(runner.test_selection(args)[2])

    def test_invalid_selection_fails_before_preparing_resources(self):
        for package in ['../other', './internal/../../other', 'example.invalid/module']:
            with self.subTest(package=package), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    runner.test_selection(['--package', package])

    def test_validation_hash_tracks_assets_and_patches_but_ignores_scratch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '--quiet', str(root)], check=True)

            def write(path, text):
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(text, encoding='utf-8')

            write('.gitignore', '**/_temp/\n')
            write('app/native/main.go', 'package main\n')
            write('app/native/assets/icon.svg', '<svg/>')
            write('third_party/mygo.json', json.dumps({'patches': [{'path': 'third_party/patches/mygo.patch'}]}))
            write('third_party/patches/mygo.patch', 'first patch')
            for path in ['tools/test_go_kernel.py', 'tools/prepare_go_runtime.py',
                         'tools/prepare_mygo.py', 'references/native-legacy-fixture.cjs',
                         'output/native-runtime/runtime-receipt.json']:
                write(path, 'fixture')
            with mock.patch.object(runtime, 'ROOT', root):
                baseline = runtime.validation_hash()
                write('app/native/assets/icon.svg', '<svg><path/></svg>')
                assets = runtime.validation_hash()
                self.assertNotEqual(baseline, assets, 'asset changes reused an unrelated validation receipt')
                write('third_party/patches/mygo.patch', 'different patch')
                patched = runtime.validation_hash()
                self.assertNotEqual(assets, patched, 'MyGo behavior changes reused an unrelated validation receipt')
                write('app/native/_temp/probe.go', 'ignored scratch')
                write('references/unrelated.md', 'unrelated documentation')
                self.assertEqual(patched, runtime.validation_hash())


if __name__ == '__main__':
    unittest.main()
