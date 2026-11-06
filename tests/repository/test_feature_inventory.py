"""Exercise the restricted parser and the generated contract against real sources."""

import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
GENERATOR = ROOT / "tools" / "generate_feature_inventory.py"
SPEC = importlib.util.spec_from_file_location("feature_inventory", GENERATOR)
inventory = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(inventory)


class ParserTests(unittest.TestCase):
    def test_enum_values_and_comments(self):
        source = inventory.uncomment("""export enum Example {
  First = 3,
  // Fake = 99,
  Second,
  Third = 'https://example.test', /* Fourth, */
}
""")
        self.assertEqual(inventory.enum_members(source, "Example"),
                         [("First", 3), ("Second", 4), ("Third", "https://example.test")])

    def test_union_uses_only_its_declaration(self):
        source = inventory.uncomment("""export type Example =
  | 'one'
  /* | 'fake' */
  | Other
export type Unrelated = 'two'
""")
        self.assertEqual(inventory.union(source, "Example"), ["'one'", "Other"])

    def test_ipc_extracts_direct_keys_and_balanced_signatures(self):
        source = """export type Channels = {
  'first': (
    value: Array<{ nested: string; other: number }>
  ) => Promise<string | null>
  second: (value: string[]) => void
}
"""
        self.assertEqual(inventory.ipc_members(source, "Channels"), [
            ["first", "( value: Array<{ nested: string; other: number }> ) => Promise<string | null>"],
            ["second", "(value: string[]) => void"],
        ])

    def test_unsupported_shapes_fail_instead_of_dropping_entries(self):
        cases = [
            (inventory.enum_members, "export enum X {\n  One = call(),\n}\n"),
            (inventory.enum_members, "export enum X {\n  One,\n  One,\n}\n"),
            (inventory.enum_members, "export enum X {\n  One,\n"),
            (inventory.union, "export type X =\n  | 'one' | 'two'\n"),
            (inventory.union, "export type X =\n  | 'one' & Other\n"),
            (inventory.ipc_members, "export type X = {\n  [key: string]: () => void\n}\n"),
            (inventory.ipc_members, "export type X = {\n  one?: () => void\n}\n"),
            (inventory.ipc_members, "export type X = {\n  one: (value: Array<string) => void\n}\n"),
            (inventory.ipc_members, "export type X = {\n  one: () => void\n  one: () => void\n}\n"),
            (inventory.ipc_members, "export type X = {\n  one: () => void\n} & Extra\n"),
        ]
        for parse, source in cases:
            with self.subTest(source=source), self.assertRaises(inventory.ParseError):
                parse(source, "X")


class FeatureInventoryTests(unittest.TestCase):
    def setUp(self):
        scratch = Path(os.environ.get("GITOWL_TEST_TEMP", ROOT / "_temp" / "feature-inventory-tests"))
        scratch.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="case-", dir=scratch)
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for path in [*inventory.SOURCES.values(), inventory.OUTPUT]:
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / path, target)

    def run_generator(self, *args):
        return subprocess.run(
            [sys.executable, "-X", "utf8", str(GENERATOR), "--root", str(self.root), *args],
            capture_output=True, text=True, encoding="utf-8",
        )

    def test_real_fixture_matches_complete_extraction_and_exact_bytes(self):
        fixture = (ROOT / inventory.OUTPUT).read_bytes()
        self.assertEqual(inventory.render(ROOT), fixture)
        parsed = json.loads(fixture)
        self.assertEqual(parsed["upstream_revision"], "a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7")
        self.assertEqual(parsed["migration_status"], "not-assessed")
        self.assertEqual(parsed["source_baseline"]["kind"], "local-git-baseline")
        self.assertFalse(parsed["source_baseline"]["remote_content_verified"])
        features = {entry["id"]: entry for entry in parsed["features"]}
        self.assertEqual(len(features), len(parsed["features"]))
        self.assertEqual(features["PreferencesTab:Accounts"], {
            "id": "PreferencesTab:Accounts", "source_file": "app/src/models/preferences.ts",
            "symbol": "PreferencesTab", "member": "Accounts", "category": "preferences-tab", "value": 0,
        })
        self.assertEqual(features["RequestResponseChannels:show-contextual-menu"]["signature"],
                         "( items: ReadonlyArray<ISerializableMenuItem>, addSpellCheckMenu: boolean ) => Promise<ReadonlyArray<number> | null>")
        self.assertEqual(features["RetryActionType:DiscardChanges"]["value"], 12)
        self.assertEqual(features["IUnknownAction:unknown"]["union"], "URLActionType")
        self.assertTrue(features["MenuEvent:test-cli-action"]["test_only"])
        result = self.run_generator("--check")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_add_remove_and_rename_in_every_source_invalidate_fixture(self):
        # Mutate actual source declarations; fixture bytes must remain untouched.
        cases = [
            ("menu", "  | 'push'", "  | 'inventory-probe'"),
            ("menu", "  'boomtown',", "  'inventory-probe',"),
            ("popup", "  RenameBranch = 'RenameBranch',", "  InventoryProbe = 'InventoryProbe',"),
            ("preferences", "  Accounts,", "  InventoryProbe,"),
            ("retry", "  Pull,", "  InventoryProbe,"),
            ("banner", "  SuccessfulMerge = 'SuccessfulMerge',", "  InventoryProbe = 'InventoryProbe',"),
            ("state", "  AddMenu,", "  InventoryProbe,"),
            ("state", "  Changes,", "  InventoryProbe,"),
            ("ipc", "  focus: () => void", "  inventoryProbe: () => void"),
            ("ipc", "  'get-app-path': () => Promise<string>", "  inventoryProbe: () => Promise<string>"),
            ("url", "  | IOAuthAction", "  | IInventoryProbeAction"),
        ]
        original_fixture = (self.root / inventory.OUTPUT).read_bytes()
        for source, old, new in cases:
            path = self.root / inventory.SOURCES[source]
            original = path.read_text(encoding="utf-8")
            self.assertEqual(original.count(old), 1)
            for operation, replacement in (("add", old + "\n" + new), ("remove", ""), ("rename", new)):
                with self.subTest(source=source, entry=old, operation=operation):
                    changed = original.replace(old, replacement)
                    if source == "url" and operation != "remove":
                        changed += "\nexport interface IInventoryProbeAction {\n  readonly name: 'inventory-probe'\n}\n"
                    path.write_text(changed, encoding="utf-8")
                    # A valid changed declaration must parse and produce a different inventory.
                    self.assertNotEqual(inventory.render(self.root), original_fixture)
                    result = self.run_generator("--check")
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertEqual((self.root / inventory.OUTPUT).read_bytes(), original_fixture)
                    path.write_text(original, encoding="utf-8")

    def test_value_signature_and_discriminant_changes_are_detected(self):
        for source, old, new in (
            ("popup", "RenameBranch = 'RenameBranch'", "RenameBranch = 'OtherValue'"),
            ("ipc", "focus: () => void", "focus: (reason: string) => void"),
            ("url", "readonly name: 'oauth'", "readonly name: 'other-oauth'"),
            ("url", "readonly state: string", "readonly state: string | null"),
            ("url", "readonly branch: string | null", "readonly branch?: string"),
        ):
            with self.subTest(source=source):
                path = self.root / inventory.SOURCES[source]
                original = path.read_text(encoding="utf-8")
                path.write_text(original.replace(old, new), encoding="utf-8")
                self.assertEqual(self.run_generator("--check").returncode, 1)
                path.write_text(original, encoding="utf-8")

    def test_check_is_byte_exact_and_generation_is_repeatable(self):
        target = self.root / inventory.OUTPUT
        expected = target.read_bytes()
        target.write_bytes(expected.replace(b"\n", b"\r\n"))
        self.assertEqual(self.run_generator("--check").returncode, 1)
        for _ in range(2):
            result = self.run_generator()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(target.read_bytes(), expected)
        target.unlink()
        self.assertEqual(self.run_generator("--check").returncode, 1)
        self.assertFalse(target.exists())

    def test_invalid_or_missing_source_returns_failure_without_writing(self):
        target = self.root / inventory.OUTPUT
        expected = target.read_bytes()
        path = self.root / inventory.SOURCES["preferences"]
        path.write_text("export enum PreferencesTab {\n  Computed = compute(),\n}\n", encoding="utf-8")
        self.assertEqual(self.run_generator().returncode, 1)
        self.assertEqual(target.read_bytes(), expected)
        path.unlink()
        self.assertEqual(self.run_generator("--check").returncode, 1)
        self.assertEqual(target.read_bytes(), expected)


if __name__ == "__main__":
    unittest.main()
