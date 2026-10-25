"""对实际初始化模块验证文件边界；只使用自动清理的临时目录。"""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

source, certificate = sys.argv[1:3]
sys.argv = sys.argv[:1]
spec = importlib.util.spec_from_file_location("agent_init", source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
CA = Path(certificate).read_text()


class InitializationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="aiofcage-init-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.outside = self.root / "outside"
        self.outside.mkdir()
        self.marker = self.outside / "config.json"
        self.marker.write_text("BEFORE")
        self.roots = self.root / "system-ca.crt"
        self.roots.write_text(CA)
        self.bundle = self.root / "bundle.crt"
        self.item = {"path": "settings/config.json", "mode": 0o600, "content": "AFTER"}
        self.target = self.home / self.item["path"]
        self.target.parent.mkdir()

    def initialize(self, item=None, ca=CA):
        module.initialize({"ca": ca, "files": [item or self.item]}, self.home, self.roots, self.bundle)

    def test_regular_and_legacy_temporary_symlink(self):
        legacy = self.target.with_name(self.target.name + ".aiofcage-tmp")
        legacy.symlink_to(self.marker)
        self.initialize()
        self.assertEqual(self.target.read_text(), "AFTER")
        self.assertFalse(self.target.is_symlink())
        self.assertEqual(self.target.stat().st_mode & 0o7777, 0o600)
        self.assertEqual(self.marker.read_text(), "BEFORE")
        self.assertEqual(self.bundle.stat().st_mode & 0o7777, 0o644)
        self.assertFalse(list(self.target.parent.glob(".aiofcage-*.tmp")))

    def test_target_symlink_is_rejected(self):
        self.target.symlink_to(self.marker)
        with self.assertRaises((OSError, ValueError)):
            self.initialize()
        self.assertEqual(self.marker.read_text(), "BEFORE")

    def test_parent_symlink_is_rejected(self):
        self.target.parent.rmdir()
        self.target.parent.symlink_to(self.outside, target_is_directory=True)
        with self.assertRaises(OSError):
            self.initialize()
        self.assertEqual(self.marker.read_text(), "BEFORE")

    def test_parent_swap_during_replace_cannot_redirect_write(self):
        original = os.replace
        detached = self.home / "detached"

        def swap(source, destination, **kwargs):
            if destination == "config.json":
                self.target.parent.rename(detached)
                self.target.parent.symlink_to(self.outside, target_is_directory=True)
            return original(source, destination, **kwargs)

        with patch.object(module.os, "replace", side_effect=swap):
            self.initialize()
        self.assertEqual(self.marker.read_text(), "BEFORE")
        self.assertEqual((detached / "config.json").read_text(), "AFTER")

    def test_random_name_collision_is_not_followed(self):
        link = self.target.parent / (".aiofcage-" + "a" * 32 + ".tmp")
        link.symlink_to(self.marker)
        directory = os.open(self.target.parent, module.DIRECTORY_FLAGS)
        try:
            with patch.object(module.secrets, "token_hex", return_value="a" * 32):
                with self.assertRaises(FileExistsError):
                    module.atomic_write(directory, "config.json", "AFTER", 0o600)
        finally:
            os.close(directory)
        self.assertEqual(self.marker.read_text(), "BEFORE")

    def test_certificate_output_symlink_is_rejected(self):
        self.bundle.symlink_to(self.marker)
        with self.assertRaises(ValueError):
            self.initialize()
        self.assertEqual(self.marker.read_text(), "BEFORE")

    def test_bad_payloads_are_rejected_before_writes(self):
        cases = [{"path": "../outside/config.json"}, {"path": "/tmp/elsewhere"},
                 {"mode": 0o7777}, {"mode": True}, {"content": "[]", "merge_json": True},
                 {"content": 123}, {"replace_keys": "mcpServers"}]
        for change in cases:
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    self.initialize({**self.item, **change})
                self.assertFalse(self.bundle.exists())
                self.assertEqual(self.marker.read_text(), "BEFORE")

    def test_malformed_ca_is_rejected_before_writes(self):
        with self.assertRaises((OSError, ValueError)):
            self.initialize(ca="-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----\n")
        self.assertFalse(self.bundle.exists())

    def test_json_merge_preserves_user_state_and_replaces_owned_keys(self):
        self.target.write_text(json.dumps({"user": {"theme": "dark"}, "mcpServers": {"old": {}}}))
        self.initialize({**self.item, "content": '{"user":{"font":14},"mcpServers":{}}',
                         "merge_json": True, "replace_keys": ["mcpServers"]})
        self.assertEqual(json.loads(self.target.read_text()),
                         {"user": {"theme": "dark", "font": 14}, "mcpServers": {}})


unittest.main(verbosity=2)
