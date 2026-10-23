import subprocess
import unittest
from unittest.mock import patch

import lab


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


if __name__ == "__main__":
    unittest.main()
