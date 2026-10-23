import subprocess
import io
import tarfile
import gzip
import json
import unittest
from unittest.mock import patch

import lab
import runtime


class BoundaryTests(unittest.TestCase):
    def test_docker_always_selects_dedicated_context(self):
        with patch("lab.command") as command:
            lab.docker("info")
        self.assertEqual(command.call_args.args[0],
                         ["docker", "--context", "colima-aiofcage", "info"])

    def test_command_failure_does_not_echo_credentials(self):
        output = subprocess.CompletedProcess([], 1, b"sensitive-value", b"sensitive-value")
        with patch("subprocess.run", return_value=output):
            with self.assertRaises(lab.LabError) as raised:
                lab.command(["docker", "exec"])
        self.assertNotIn("sensitive-value", str(raised.exception))

    def test_run_id_cannot_escape_state_directory_or_select_all_resources(self):
        for value in ("../default", "", "*", "a/b", "UPPER", "x" * 49):
            with self.subTest(value=value), self.assertRaises(lab.LabError):
                lab.run_id(value)
        self.assertEqual(lab.run_id("run-20261023"), "run-20261023")

    def test_stream_scanner_finds_secret_across_chunk_boundary(self):
        value = b"synthetic-private-value"
        stream = io.BytesIO(b"x" * 65531 + value + b"y" * 100)
        self.assertTrue(runtime.contains_secret(stream, [value]))
        self.assertFalse(runtime.contains_secret(io.BytesIO(b"public-data"), [value]))

    def test_image_scanner_reads_compressed_layers(self):
        payload = gzip.compress(b"prefix synthetic-private-value suffix")
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w") as archive:
            info = tarfile.TarInfo("blobs/sha256/test")
            info.size = len(payload)
            archive.addfile(info, io.BytesIO(payload))
        buffer.seek(0)
        with patch("subprocess.Popen") as start:
            process = start.return_value
            process.stdout = buffer
            process.wait.return_value = 0
            process.poll.return_value = 0
            self.assertTrue(runtime.scan_command(["image", "save", "test"],
                                                 [b"synthetic-private-value"], image=True))

    def test_cleanup_selects_only_one_run_and_never_prunes(self):
        def response(*args, **kwargs):
            if args[0:2] == ("container", "ls"):
                return subprocess.CompletedProcess([], 0, b"abc\n", b"")
            if args[0:2] == ("network", "ls"):
                return subprocess.CompletedProcess([], 0, b"def\n", b"")
            return subprocess.CompletedProcess([], 0, b"", b"")
        with patch("runtime.docker", side_effect=response) as docker:
            self.assertTrue(runtime.cleanup("run-example"))
        self.assertEqual(docker.call_args_list[0].args,
                         ("container", "ls", "--quiet", "--filter", "label=io.aiofcage.validation=run-example", "--all"))
        self.assertEqual(docker.call_args_list[1].args, ("rm", "--force", "abc"))
        self.assertEqual(docker.call_args_list[3].args, ("network", "rm", "def"))

    def test_vm_rejects_extra_host_mount_before_using_docker(self):
        entry = {"name": "aiofcage", "status": "Running", "arch": "aarch64"}
        config = {"config": {"mounts": [
            {"location": str(runtime.WORKSPACE), "writable": True},
            {"location": "/Users", "writable": True}]}}
        with patch("runtime.WORKSPACE") as workspace, patch("runtime.colima") as colima, \
                patch("runtime.command") as command, patch("runtime.docker") as docker:
            workspace.is_symlink.return_value = False
            workspace.is_dir.return_value = True
            colima.return_value.stdout = json.dumps(entry).encode()
            command.return_value.stdout = json.dumps(config).encode()
            with self.assertRaises(lab.LabError):
                runtime.verify_vm()
            docker.assert_not_called()


if __name__ == "__main__":
    unittest.main()
