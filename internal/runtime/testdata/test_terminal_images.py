"""对实际附件脚本验证写入边界：符号链接、预置目录、恶意名称与中途失败；只使用自动清理的临时目录。"""
import base64
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

source = Path(sys.argv[1]).resolve()
sys.argv = sys.argv[:1]
spec = importlib.util.spec_from_file_location("terminal_images", source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

BATCH = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"


def payload(*items):
    return json.dumps([{"name": name, "data": base64.b64encode(data).decode()} for name, data in items]).encode()


class AttachmentTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="aiofcage-attachments-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.home = self.root / "home"
        self.home.mkdir()
        # project 代表 Agent 可写、但附件绝不能写入或删除的位置。
        self.project = self.root / "project"
        self.project.mkdir()
        self.marker = self.project / "1.png"
        self.marker.write_bytes(b"BEFORE")
        self.attachments = self.home / ".aiofcage" / "attachments"
        self.batch = self.attachments / BATCH

    def write(self, *items):
        module.write(str(self.home), BATCH, module.parse(payload(*items)))

    def remove(self):
        module.remove(str(self.home), BATCH)

    def plant(self, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.symlink_to(self.project, target_is_directory=True)

    def assert_project_untouched(self):
        self.assertEqual(sorted(path.name for path in self.project.iterdir()), ["1.png"])
        self.assertEqual(self.marker.read_bytes(), b"BEFORE")

    def test_images_and_files_are_written_exactly_and_removed(self):
        self.write(("1.png", b"\x89PNG image"), ("2-季度 报告(终稿).pdf", b"%PDF-1.7"), ("3-附件", b"plain"))
        self.assertEqual((self.batch / "1.png").read_bytes(), b"\x89PNG image")
        self.assertEqual((self.batch / "2-季度 报告(终稿).pdf").read_bytes(), b"%PDF-1.7")
        self.assertEqual((self.batch / "3-附件").read_bytes(), b"plain")
        self.assertEqual(self.batch.stat().st_mode & 0o777, 0o700)
        for path in self.batch.iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.remove()
        self.assertFalse(self.batch.exists())
        self.assertTrue(self.attachments.is_dir())
        self.assert_project_untouched()

    def test_symlinked_parent_directories_are_not_followed(self):
        for link in (self.home / ".aiofcage", self.attachments):
            with self.subTest(link=link.name):
                self.plant(link)
                with self.assertRaises(OSError):
                    self.write(("1.png", b"NEW"))
                with self.assertRaises(OSError):
                    self.remove()
                self.assert_project_untouched()
                link.unlink()

    def test_existing_batch_path_is_never_reused(self):
        self.plant(self.batch)
        with self.assertRaises(FileExistsError):
            self.write(("1.png", b"NEW"))
        self.assertTrue(self.batch.is_symlink())
        # 删除时也不能经由链接删掉项目里同名的文件。
        with self.assertRaises(OSError):
            self.remove()
        self.assert_project_untouched()
        self.batch.unlink()
        dangling = self.root / "created-through-link"
        self.batch.symlink_to(dangling, target_is_directory=True)
        with self.assertRaises(FileExistsError):
            self.write(("1.png", b"NEW"))
        self.assertFalse(dangling.exists())
        self.batch.unlink()
        # 预先建好的目录（里面放着指向项目的链接）同样不复用。
        self.batch.mkdir()
        (self.batch / "1.png").symlink_to(self.marker)
        with self.assertRaises(FileExistsError):
            self.write(("1.png", b"NEW"))
        self.assert_project_untouched()

    def test_entries_planted_in_the_new_batch_are_not_followed(self):
        original = module.open_new_batch
        secret = self.home / "secret.txt"
        secret.write_bytes(b"KEEP")

        def plant_after_check(kind):
            def opened(root, batch):
                directory = original(root, batch)
                if kind == "symlink":
                    os.symlink(self.marker, "1.png", dir_fd=directory)
                else:
                    os.link(secret, "1.png", dst_dir_fd=directory)
                return directory
            return opened

        for kind in ("symlink", "hardlink"):
            with self.subTest(kind=kind):
                with patch.object(module, "open_new_batch", side_effect=plant_after_check(kind)):
                    with self.assertRaises(FileExistsError):
                        self.write(("1.png", b"NEW"))
                self.assertFalse(self.batch.exists())
                self.assert_project_untouched()
                self.assertEqual(secret.read_bytes(), b"KEEP")

        def plant_before_check(root, batch):
            os.symlink(self.marker, BATCH + "/1.png", dir_fd=root)
            return original(root, batch)

        with patch.object(module, "open_new_batch", side_effect=plant_before_check):
            with self.assertRaises(OSError):
                self.write(("1.png", b"NEW"))
        self.assert_project_untouched()

    def test_failure_half_way_removes_what_was_written(self):
        original = module.write_file
        calls = []

        def flaky(directory, name, data):
            calls.append(name)
            if len(calls) == 2:
                fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600, dir_fd=directory)
                os.write(fd, data[:2])
                os.close(fd)
                raise OSError("disk full")
            return original(directory, name, data)

        with patch.object(module, "write_file", side_effect=flaky):
            with self.assertRaises(OSError):
                self.write(("1-a.txt", b"first"), ("2-b.txt", b"second"), ("3-c.txt", b"third"))
        self.assertEqual(len(calls), 2)
        self.assertFalse(self.batch.exists())
        self.assertEqual(list(self.attachments.iterdir()), [])

    def test_hostile_payloads_are_rejected_before_any_write(self):
        seven = b"x" * (7 * 1024 * 1024)
        cases = {
            "traversal": payload(("1-../../project/1.png", b"x")),
            "separator": payload(("1-a/b", b"x")),
            "absolute": payload(("/etc/passwd", b"x")),
            "no index": payload(("report.pdf", b"x")),
            "wrong index": payload(("2-report.pdf", b"x")),
            "duplicate index": payload(("1.png", b"x"), ("1-b.txt", b"x")),
            "not an image extension": payload(("1.exe", b"x")),
            "dot": payload((".", b"x")),
            "dot dot": payload(("..", b"x")),
            "newline": payload(("1-a\nb", b"x")),
            "nul": payload(("1-a\x00b", b"x")),
            "c1 control": payload(("1-a\x85b", b"x")),
            "name too long": payload(("1-" + "a" * 254, b"x")),
            "empty file": payload(("1-a.txt", b"")),
            "too many": payload(*[("%d-a" % i, b"x") for i in range(1, 6)]),
            "file too large": payload(("1-a.bin", b"x" * (module.MAX_FILE_BYTES + 1))),
            "total too large": payload(("1-a", seven), ("2-b", seven), ("3-c", seven)),
            "not a list": b'{"name": "1-a", "data": "eA=="}',
            "empty list": b"[]",
            "not json": b"not json",
            "name not text": b'[{"name": 1, "data": "eA=="}]',
            "missing data": b'[{"name": "1-a"}]',
            "invalid base64": b'[{"name": "1-a", "data": "!!"}]',
            "lone surrogate": b'[{"name": "1-\\ud800", "data": "eA=="}]',
        }
        for name, raw in cases.items():
            with self.subTest(case=name):
                with self.assertRaises(ValueError):
                    module.write(str(self.home), BATCH, module.parse(raw))
                self.assertFalse((self.home / ".aiofcage").exists())
                self.assert_project_untouched()

    def test_remove_unlinks_entries_without_following_them(self):
        self.write(("1.png", b"image"), ("2-a.txt", b"text"))
        (self.batch / "2-a.txt").unlink()
        (self.batch / "2-a.txt").symlink_to(self.marker)
        (self.batch / "notes.md").write_text("agent output")
        # Agent 自己的文件保留，目录因此删不掉；链接只删除自身。
        with self.assertRaises(OSError):
            self.remove()
        self.assertEqual(sorted(path.name for path in self.batch.iterdir()), ["notes.md"])
        self.assert_project_untouched()

    def test_remove_without_attachments_is_quiet(self):
        self.remove()
        self.attachments.mkdir(parents=True)
        self.remove()
        self.assertEqual(list(self.attachments.iterdir()), [])

    def test_command_line_rejects_bad_input_without_echo(self):
        script = [sys.executable, "-I", str(source)]
        for args in ([], ["write"], ["delete", BATCH], ["write", "../../tmp"], ["write", "short"]):
            with self.subTest(args=args):
                result = subprocess.run(script + args, input=b"", capture_output=True)
                self.assertNotEqual(result.returncode, 0)
        secret = payload(("1-secret-name/../x", b"secret-content"))
        result = subprocess.run(script + ["write", BATCH], input=secret, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(b"secret", result.stdout + result.stderr)


unittest.main(verbosity=2)
