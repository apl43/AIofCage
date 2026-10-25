#!/usr/bin/env python3
"""通过官方 CLI 执行一轮任务；使用 JSON 管道，不注入终端按键或粘贴序列。"""
import json
from pathlib import Path
import subprocess
import sys
import tempfile


def emit(kind, text=""):
    print(json.dumps({"type": kind, "text": text}, ensure_ascii=False), flush=True)


def command(data, directory):
    agent, model, effort = data["agent"], data["model"], data.get("effort", "")
    resume, text, images = data.get("resume_id", ""), data["text"], data.get("images") or []
    if agent == "codex":
        args = ["codex", "exec"] + (["resume"] if resume else [])
        args += ["--json", "--skip-git-repo-check", "-c", 'approval_policy="never"',
                 "-c", 'sandbox_mode="danger-full-access"', "-c", "model=" + json.dumps(model)]
        if effort:
            args += ["-c", "model_reasoning_effort=" + json.dumps(effort)]
        for image in images:
            args += ["--image", image]
        if resume:
            args.append(resume)
        return args + ["-"], text
    if images:
        text += "\n\n请用图片读取工具查看这些附件后回答：\n" + "\n".join(images)
    if agent == "claude-code":
        args = ["claude", "--print", "--output-format", "stream-json", "--verbose",
                "--include-partial-messages", "--dangerously-skip-permissions", "--model", model]
        if effort:
            args += ["--effort", effort]
        if resume:
            args += ["--resume", resume]
        return args, text
    if agent == "dsh":
        overlay = Path(directory) / "model.patch.yml"
        overlay.write_text(data["dsh_patch"])
        overlay.chmod(0o600)
        # --patch 属于启动器，必须在 headless 自己的参数之前。
        args = ["dsh", "--profile", "headless", "--patch", str(overlay), "--json"]
        if resume:
            args += ["--session-id", resume]
        return args + ["-"], text
    raise ValueError("unsupported agent")


class Projection:
    def __init__(self, agent):
        self.agent, self.streamed = agent, False

    def consume(self, event):
        kind = event.get("type")
        if self.agent == "dsh":
            if kind == "session":
                emit("session", event.get("sessionId", ""))
            elif kind in ("text", "thinking", "final"):
                # final 保留无截断的最后答案，修正 text 投影的 8 KiB 上限。
                emit(kind, event.get("text", ""))
            elif kind == "tool_call":
                emit("tool", str(event.get("tool", "工具")))
            elif kind == "error":
                emit("error", str(event.get("message", "任务失败")))
        elif self.agent == "codex":
            if kind == "thread.started":
                emit("session", event.get("thread_id", ""))
            elif kind == "item.completed":
                item = event.get("item", {})
                if item.get("type") == "agent_message":
                    emit("text", item.get("text", ""))
                elif item.get("type") == "reasoning":
                    emit("thinking", item.get("text", ""))
                elif item.get("type") in ("command_execution", "mcp_tool_call", "file_change", "web_search"):
                    emit("tool", item["type"])
            elif kind in ("error", "turn.failed"):
                emit("error", str(event.get("message") or event.get("error", {}).get("message") or "任务失败"))
        elif self.agent == "claude-code":
            if event.get("session_id"):
                emit("session", event["session_id"])
            if event.get("parent_tool_use_id"):
                return
            if kind == "stream_event":
                nested = event.get("event", {})
                delta = nested.get("delta", {})
                if nested.get("type") == "content_block_start":
                    block = nested.get("content_block", {})
                    if block.get("type") == "tool_use":
                        emit("tool", block.get("name", "工具"))
                elif delta.get("type") == "text_delta":
                    self.streamed = True
                    emit("delta", delta.get("text", ""))
                elif delta.get("type") == "thinking_delta":
                    emit("thinking", delta.get("thinking", ""))
            elif kind == "assistant":
                for block in event.get("message", {}).get("content", []):
                    if block.get("type") == "text" and not self.streamed:
                        emit("text", block.get("text", ""))
                self.streamed = False
            elif kind == "result" and event.get("is_error"):
                emit("error", str(event.get("result") or "任务未完成"))


def main():
    data = json.load(sys.stdin)
    with tempfile.TemporaryDirectory(prefix="aiofcage-turn-") as directory:
        args, text = command(data, directory)
        projection = Projection(data["agent"])
        with tempfile.TemporaryFile() as task:
            task.write(text.encode("utf-8"))
            task.seek(0)
            child = subprocess.Popen(args, stdin=task, stdout=subprocess.PIPE, text=True)
            for line in child.stdout:
                try:
                    event = json.loads(line)
                except ValueError:
                    continue
                if isinstance(event, dict):
                    projection.consume(event)
            code = child.wait()
        raise SystemExit(code)


if __name__ == "__main__":
    main()
