"""将 Agent 可读状态流向宿主扫描器；不把待匹配的上游秘密交给 Agent。"""
import os
from pathlib import Path
import sys

output = sys.stdout.buffer
output.write(b"\0".join(k.encode() + b"=" + v.encode() for k, v in os.environ.items()))
for root in ("/home/agent", "/tmp", os.getcwd()):
    for directory, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = [d for d in dirs if not Path(directory, d).is_symlink()]
        for name in files:
            p = Path(directory, name)
            if p.is_symlink() or not p.is_file():
                continue
            with p.open("rb") as stream:
                while chunk := stream.read(65536):
                    output.write(chunk)
for p in Path("/proc").glob("[0-9]*"):
    for name in ("environ", "cmdline"):
        try:
            output.write((p / name).read_bytes())
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            pass
