# 附件（图片与普通文件）只保存在沙箱 home 的独立目录：每一级目录都拒绝符号链接，批次目录每次新建，
# 文件只以独占方式创建并在写入后核对。宿主以 Agent 用户直接运行本脚本（python3 -I，不经过 aiofcage-run），
# 内容只经标准输入传递；出错时不回显内容、名称或路径。
import base64
import json
import os
import re
import stat
import sys

HOME = "/home/agent"
BATCH = re.compile(r"[A-Za-z0-9]{20,40}")
# 图片为 <序号>.<扩展名>，其他文件为 <序号>-<名称>；名称已在宿主端清理，这里只做文件系统层面的复核。
NAME = re.compile(r"([1-4])(?:\.(?:png|jpg|gif|webp)|-[^/\x00-\x1f\x7f-\x9f]+)")
MAX_FILES = 4
MAX_FILE_BYTES = 10 * 1024 * 1024
MAX_TOTAL_BYTES = 20 * 1024 * 1024
MAX_NAME_BYTES = 255
MAX_INPUT_BYTES = 32 * 1024 * 1024
DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
CREATE = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC


def parse(raw):
    """在写入任何内容之前校验全部附件，返回 (文件名字节, 内容) 列表。"""
    items = json.loads(raw)
    if not isinstance(items, list) or not 1 <= len(items) <= MAX_FILES:
        raise ValueError("invalid count")
    result, total = [], 0
    for index, item in enumerate(items, 1):
        if not isinstance(item, dict) or not isinstance(item.get("name"), str) or not isinstance(item.get("data"), str):
            raise ValueError("invalid attachment")
        name = item["name"]
        match = NAME.fullmatch(name)
        encoded = name.encode("utf-8")
        if not match or match.group(1) != str(index) or len(encoded) > MAX_NAME_BYTES:
            raise ValueError("invalid name")
        data = base64.b64decode(item["data"], validate=True)
        total += len(data)
        if not 0 < len(data) <= MAX_FILE_BYTES or total > MAX_TOTAL_BYTES:
            raise ValueError("invalid size")
        result.append((encoded, data))
    return result


def attachments(home, create):
    """逐级打开 home/.aiofcage/attachments 并持有目录描述符；任何一级是符号链接都会失败。"""
    current = os.open(home, DIRECTORY)
    try:
        for name in (".aiofcage", "attachments"):
            if create:
                try:
                    os.mkdir(name, 0o700, dir_fd=current)
                except FileExistsError:
                    pass
            child = os.open(name, DIRECTORY, dir_fd=current)
            os.close(current)
            current = child
        return current
    except BaseException:
        os.close(current)
        raise


def open_new_batch(root, batch):
    """打开刚新建的批次目录，并确认它属于当前用户且为空。"""
    directory = os.open(batch, DIRECTORY, dir_fd=root)
    try:
        os.fchmod(directory, 0o700)
        info = os.fstat(directory)
        if info.st_uid != os.geteuid() or os.listdir(directory):
            raise OSError("attachment directory is not new")
    except BaseException:
        os.close(directory)
        raise
    return directory


def write_file(directory, name, data):
    """独占创建并写入一个附件，再核对它确实是本次写入的普通文件。"""
    fd = os.open(name, CREATE, 0o600, dir_fd=directory)
    try:
        os.fchmod(fd, 0o600)
        view = memoryview(data)
        while view:
            view = view[os.write(fd, view):]
        info = os.fstat(fd)
    finally:
        os.close(fd)
    if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size != len(data)
            or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o600):
        raise OSError("attachment verification failed")


def write(home, batch, items):
    root = attachments(home, True)
    made, directory, written = False, None, []
    try:
        # 同名目录、文件或符号链接已存在时 mkdir 直接失败，不会复用别人准备的目录。
        os.mkdir(batch, 0o700, dir_fd=root)
        made = True
        directory = open_new_batch(root, batch)
        for name, data in items:
            written.append(name)
            write_file(directory, name, data)
        # 目录里只能有本次写入的文件。
        if sorted(os.fsencode(entry) for entry in os.listdir(directory)) != sorted(written):
            raise OSError("unexpected attachment directory contents")
    except BaseException:
        # 写到一半失败时删除本次创建的内容；宿主随后还会再清理一次。
        if directory is not None:
            for name in written:
                try:
                    os.unlink(name, dir_fd=directory)
                except OSError:
                    pass
        if made:
            try:
                os.rmdir(batch, dir_fd=root)
            except OSError:
                pass
        raise
    finally:
        if directory is not None:
            os.close(directory)
        os.close(root)


def remove(home, batch):
    """只删除本脚本命名格式的目录项，再删除空的批次目录；不跟随任何符号链接。"""
    try:
        root = attachments(home, False)
    except FileNotFoundError:
        return
    try:
        try:
            directory = os.open(batch, DIRECTORY, dir_fd=root)
        except FileNotFoundError:
            return
        try:
            for entry in os.listdir(directory):
                if NAME.fullmatch(entry):
                    try:
                        # unlink 只删除目录项本身；即使它是符号链接，也不会影响链接指向的文件。
                        os.unlink(entry, dir_fd=directory)
                    except OSError:
                        pass
        finally:
            os.close(directory)
        os.rmdir(batch, dir_fd=root)
    finally:
        os.close(root)


def main(argv):
    if len(argv) != 2 or argv[0] not in ("write", "remove") or not BATCH.fullmatch(argv[1]):
        raise SystemExit("invalid attachment arguments")
    operation, batch = argv
    try:
        if operation == "write":
            raw = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
            if len(raw) > MAX_INPUT_BYTES:
                raise ValueError("attachment payload too large")
            write(HOME, batch, parse(raw))
        else:
            remove(HOME, batch)
    except (OSError, ValueError, TypeError, RecursionError):
        raise SystemExit("attachment operation failed") from None


if __name__ == "__main__":
    main(sys.argv[1:])
