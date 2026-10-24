# 图片只保存在沙箱 home 的独立目录，所有路径组件都拒绝符号链接。
import base64
import json
import os
import re
import sys

operation, batch = sys.argv[1:]
if not re.fullmatch(r"[A-Za-z0-9]{20,40}", batch):
    raise ValueError("invalid batch")

flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
root = os.open("/home/agent", flags)
try:
    for name in (".aiofcage", "attachments"):
        try:
            os.mkdir(name, 0o700, dir_fd=root)
        except FileExistsError:
            pass
        child = os.open(name, flags, dir_fd=root)
        os.close(root)
        root = child
    if operation == "write":
        files = json.load(sys.stdin)
        if not 1 <= len(files) <= 4:
            raise ValueError("invalid count")
        os.mkdir(batch, 0o700, dir_fd=root)
        directory = os.open(batch, flags, dir_fd=root)
        try:
            for index, file in enumerate(files, 1):
                ext = file["extension"]
                if ext not in ("png", "jpg", "gif", "webp"):
                    raise ValueError("invalid format")
                data = base64.b64decode(file["data"], validate=True)
                if not 0 < len(data) <= 10 * 1024 * 1024:
                    raise ValueError("invalid size")
                fd = os.open(str(index) + "." + ext, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
                with os.fdopen(fd, "wb") as target:
                    target.write(data)
        finally:
            os.close(directory)
    elif operation == "remove":
        try:
            directory = os.open(batch, flags, dir_fd=root)
        except FileNotFoundError:
            sys.exit(0)
        try:
            for name in os.listdir(directory):
                if re.fullmatch(r"[1-4]\.(png|jpg|gif|webp)", name):
                    os.unlink(name, dir_fd=directory)
        finally:
            os.close(directory)
        os.rmdir(batch, dir_fd=root)
    else:
        raise ValueError("invalid operation")
finally:
    os.close(root)
