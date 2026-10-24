#!/usr/bin/env python3
"""沙箱启动时由 AIofCage 调用：写入门卫的公有 CA 和 Agent 配置。

只接收公有证书和非敏感设置（占位符、接口地址、模型名）；真实密钥永远不会进入这里。
"""
import json
import os
from pathlib import Path
import sys

HOME = Path("/home/agent")
data = json.load(sys.stdin)
ca = data["ca"]
if "PRIVATE KEY" in ca or not ca.startswith("-----BEGIN CERTIFICATE-----"):
    raise SystemExit("invalid certificate")

# 系统信任库 = 镜像自带的公共根证书 + 本次门卫的 CA。/etc/ssl/certs 是 tmpfs，每次启动重写。
system = Path("/opt/aiofcage/system-ca.crt").read_text()
Path("/etc/ssl/certs/ca-certificates.crt").write_text(system.rstrip("\n") + "\n" + ca)


def safe_target(relative: str) -> Path:
    rel = Path(relative)
    if rel.is_absolute() or ".." in rel.parts or not rel.parts:
        raise SystemExit("invalid path")
    current = HOME
    for part in rel.parts:
        current = current / part
        # 拒绝经符号链接写到 home 以外（例如被链接到挂载的项目目录）。
        if current.is_symlink():
            raise SystemExit("refusing to write through a symlink")
    return current


files = [{"path": ".aiofcage/gatekeeper-ca.crt", "mode": 0o644, "content": ca}] + data.get("files", [])
for item in files:
    target = safe_target(item["path"])
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    tmp = target.with_name(target.name + ".aiofcage-tmp")
    tmp.write_text(item["content"])
    os.chmod(tmp, item.get("mode", 0o600))
    os.replace(tmp, target)
print("ok")
