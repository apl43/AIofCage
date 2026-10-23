#!/usr/bin/env python3
"""AIofCage 的可丢弃验证工具；绝不使用默认 Docker context。"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
LAB = Path(__file__).resolve().parent
PROFILE = "aiofcage"
CONTEXT = "colima-aiofcage"
LABEL = "io.aiofcage.validation"
RUNS = ROOT / ".validation-runs"
TOOLS = RUNS / "tools" / "bin"
if TOOLS.is_dir():
    os.environ["PATH"] = str(TOOLS) + os.pathsep + os.environ.get("PATH", "")


class LabError(Exception):
    """错误消息只含固定上下文，不能包含秘密或未经脱敏的子进程输出。"""


def command(args: list[str], *, timeout: int = 30, data: bytes | None = None,
            check: bool = True, env: dict | None = None) -> subprocess.CompletedProcess:
    try:
        result = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout, env=env)
    except FileNotFoundError:
        raise LabError(f"缺少命令：{args[0]}") from None
    except subprocess.TimeoutExpired:
        raise LabError(f"命令超时：{args[0]}（{timeout} 秒）") from None
    if check and result.returncode:
        raise LabError(f"命令失败：{args[0]}，退出码 {result.returncode}；原始输出不写入报告")
    return result


def docker(*args: str, **kwargs) -> subprocess.CompletedProcess:
    # 即使 DOCKER_HOST 被用户设置，也以显式 context 选择专用实例。
    return command(["docker", "--context", CONTEXT, *args], **kwargs)


def run_id(value: str) -> str:
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,47}", value):
        raise LabError("实验 ID 只允许 1—48 位小写字母、数字和连字符")
    return value


def new_report(kind: str) -> dict:
    revision = command(["git", "rev-parse", "HEAD"], check=False).stdout.decode().strip()
    dirty = bool(command(["git", "status", "--porcelain"], check=False).stdout)
    return {"schema": 1, "experiment": kind,
            "time": dt.datetime.now(dt.timezone(dt.timedelta(hours=8))).isoformat(),
            "commit": revision, "working_tree_dirty": dirty,
            "host": {"os": platform.system(), "release": platform.mac_ver()[0],
                     "arch": platform.machine()}, "checks": []}


def record(report: dict, name: str, status: str, detail: str) -> None:
    if status not in {"pass", "fail", "blocked"}:
        raise ValueError(status)
    report["checks"].append({"name": name, "status": status, "detail": detail})
    print(f"[{status}] {name}: {detail}")


def save_report(report: dict, directory: Path | None = None) -> int:
    directory = directory or RUNS / dt.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = directory / f"{report['experiment']}.json"
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    path.chmod(0o600)
    print(f"报告：{path.relative_to(ROOT)}")
    return 0 if all(c["status"] == "pass" for c in report["checks"]) else 1


def doctor() -> int:
    report = new_report("doctor")
    supported = platform.system() == "Darwin" and platform.machine() == "arm64"
    record(report, "platform", "pass" if supported else "blocked", "首批基线：macOS arm64")
    for name, args in (("go", ["go", "version"]),
                       ("docker", ["docker", "--version"]),
                       ("colima", ["colima", "version"])):
        if not shutil.which(name):
            record(report, name, "blocked", "未安装，请先安装该依赖")
            continue
        try:
            result = command(args)
            # 这里只运行公开版本命令，不输出任意配置或环境变量。
            version = result.stdout.decode(errors="replace").strip().splitlines()[0]
            record(report, name, "pass", version)
        except (LabError, IndexError) as error:
            record(report, name, "blocked", str(error))
    if shutil.which("docker"):
        try:
            docker("context", "inspect", CONTEXT)
            version = docker("info", "--format", "{{.ServerVersion}}").stdout.decode().strip()
            record(report, "dedicated-context", "pass", f"{CONTEXT}，Docker Engine {version}")
        except LabError:
            record(report, "dedicated-context", "blocked", f"{CONTEXT} 不存在或未运行；不回退默认实例")
    record(report, "isolation-evidence", "blocked", "doctor 不验证隔离；需执行容器探针")
    return save_report(report)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["doctor", "tools", "setup", "build", "run", "cleanup"])
    parser.add_argument("--live", action="store_true", help="使用 Keychain 中的实验 key 运行 Codex 真实任务")
    parser.add_argument("--model", default="gpt-6.1-sol")
    parser.add_argument("--keychain-service", default="sbx-builderday-key")
    parser.add_argument("--run-id", help="清理被中断的本次实验资源；不删除工作区")
    args = parser.parse_args()
    try:
        if args.action == "doctor":
            return doctor()
        import runtime
        return runtime.dispatch(args)
    except LabError as error:
        print(f"实验停止：{error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    # runtime 导入同一个模块实例，避免 LabError 因 __main__/lab 双载入而不被捕获。
    sys.modules["lab"] = sys.modules[__name__]
    raise SystemExit(main())
