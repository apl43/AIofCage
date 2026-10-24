#!/usr/bin/env python3
"""初始化公有 CA 与 Agent 配置；所有写入锚定目录描述符，不跟随符号链接。"""
import json
import os
from pathlib import Path
import secrets
import ssl
import stat
import sys

MAX_BYTES = 4 * 1024 * 1024
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def path_parts(value):
    if not isinstance(value, str) or not value or "\0" in value:
        raise ValueError("invalid path")
    path = Path(value)
    if path.is_absolute() or ".." in path.parts or not path.parts:
        raise ValueError("invalid path")
    return path.parts


def parent_directory(root, parts):
    """逐级打开并持有目录，路径被替换后也不会跳到链接指向的位置。"""
    current = os.dup(root)
    try:
        for part in parts:
            try:
                os.mkdir(part, 0o700, dir_fd=current)
            except FileExistsError:
                pass
            child = os.open(part, DIRECTORY_FLAGS, dir_fd=current)
            os.close(current)
            current = child
        return current
    except BaseException:
        os.close(current)
        raise


def check_target(directory, name):
    try:
        info = os.stat(name, dir_fd=directory, follow_symlinks=False)
    except FileNotFoundError:
        return
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("refusing non-regular configuration target")


def atomic_write(directory, name, content, mode):
    check_target(directory, name)
    temporary = ".aiofcage-" + secrets.token_hex(16) + ".tmp"
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                 0o600, dir_fd=directory)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content.encode("utf-8"))
            stream.flush()
            os.fchmod(stream.fileno(), mode)
        # replace 不跟随最后一段链接；两端均使用已打开的父目录。
        os.replace(temporary, name, src_dir_fd=directory, dst_dir_fd=directory)
    finally:
        try:
            os.unlink(temporary, dir_fd=directory)
        except FileNotFoundError:
            pass


def check_depth(value, depth=0):
    if depth > 32:
        raise ValueError("configuration nesting too deep")
    if isinstance(value, dict):
        for child in value.values():
            check_depth(child, depth + 1)
    elif isinstance(value, list):
        for child in value:
            check_depth(child, depth + 1)


def merge(base, update):
    for key, value in update.items():
        if isinstance(value, dict) and isinstance(base.get(key), dict):
            merge(base[key], value)
        else:
            base[key] = value
    return base


def existing_json(directory, name):
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                     dir_fd=directory)
    except FileNotFoundError:
        return {}
    with os.fdopen(fd, "rb") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError("refusing non-regular configuration target")
        raw = stream.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        raise ValueError("existing configuration too large")
    try:
        result = json.loads(raw)
    except (ValueError, UnicodeError):
        return {}
    check_depth(result)
    return result if isinstance(result, dict) else {}


def validate(data):
    if not isinstance(data, dict) or not isinstance(data.get("ca"), str):
        raise ValueError("invalid initialization payload")
    ca = data["ca"]
    if "PRIVATE KEY" in ca or not ca.startswith("-----BEGIN CERTIFICATE-----"):
        raise ValueError("invalid certificate")
    # 交给 TLS 库实际解析，避免仅凭 PEM 开头接受任意文本。
    ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT).load_verify_locations(cadata=ca)
    files = data.get("files", [])
    if not isinstance(files, list) or len(files) > 128:
        raise ValueError("invalid file list")
    for item in files:
        if not isinstance(item, dict):
            raise ValueError("invalid file configuration")
        path_parts(item.get("path"))
        if not isinstance(item.get("content"), str) or len(item["content"].encode("utf-8")) > MAX_BYTES:
            raise ValueError("invalid configuration content")
        mode = item.get("mode", 0o600)
        if type(mode) is not int or mode not in (0o600, 0o644):
            raise ValueError("invalid configuration mode")
        if type(item.get("merge_json", False)) is not bool:
            raise ValueError("invalid merge flag")
        keys = item.get("replace_keys", [])
        if not isinstance(keys, list) or any(not isinstance(key, str) for key in keys):
            raise ValueError("invalid replacement keys")
        if item.get("merge_json"):
            update = json.loads(item["content"])
            if not isinstance(update, dict):
                raise ValueError("JSON update must be an object")
            check_depth(update)
    return [{"path": ".aiofcage/gatekeeper-ca.crt", "mode": 0o644, "content": ca}] + files


def initialize(data, home=Path("/home/agent"), roots=Path("/opt/aiofcage/system-ca.crt"),
               bundle=Path("/etc/ssl/certs/ca-certificates.crt")):
    files = validate(data)
    root = os.open(home, DIRECTORY_FLAGS)
    try:
        # 证书输出也采用安全写入，避免可写 tmpfs 中的链接预置。
        certificate_directory = os.open(bundle.parent, DIRECTORY_FLAGS)
        try:
            atomic_write(certificate_directory, bundle.name,
                         roots.read_text().rstrip("\n") + "\n" + data["ca"], 0o644)
        finally:
            os.close(certificate_directory)
        for item in files:
            parts = path_parts(item["path"])
            directory = parent_directory(root, parts[:-1])
            try:
                content = item["content"]
                if item.get("merge_json"):
                    existing = existing_json(directory, parts[-1])
                    for key in item.get("replace_keys", []):
                        existing.pop(key, None)
                    content = json.dumps(merge(existing, json.loads(content)), ensure_ascii=False, indent=2)
                if len(content.encode("utf-8")) > MAX_BYTES:
                    raise ValueError("merged configuration too large")
                atomic_write(directory, parts[-1], content, item.get("mode", 0o600))
            finally:
                os.close(directory)
    finally:
        os.close(root)


def main():
    try:
        raw = sys.stdin.buffer.read(MAX_BYTES + 1)
        if len(raw) > MAX_BYTES:
            raise ValueError("initialization payload too large")
        initialize(json.loads(raw))
    except (OSError, ValueError, TypeError, RecursionError):
        # 不回显内容、路径或配置中的凭据。
        raise SystemExit("invalid or unsafe initialization configuration") from None
    print("ok")


if __name__ == "__main__":
    main()
