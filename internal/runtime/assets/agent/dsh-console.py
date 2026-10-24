#!/usr/bin/env python3
# 通过固定版本的 headless profile 提供可连续对话的终端入口。
import json
import subprocess

print("DeepSeek Harness · 输入任务开始，输入 /new 新建会话，/exit 退出。", flush=True)
session_id = None
while True:
    try:
        prompt = input("\n› ").strip()
    except (EOFError, KeyboardInterrupt):
        break
    if prompt == "/exit":
        break
    if prompt == "/new":
        session_id = None
        print("已新建会话。", flush=True)
        continue
    if not prompt:
        continue
    args = ["dsh", "headless", "--json"]
    if session_id:
        args.extend(["--session-id", session_id])
    args.append(prompt)
    child = subprocess.Popen(args, stdout=subprocess.PIPE, text=True)
    try:
        for line in child.stdout:
            try:
                event = json.loads(line)
            except ValueError:
                print(line, end="", flush=True)
                continue
            if event.get("type") == "session":
                session_id = event.get("sessionId")
            elif event.get("type") == "text":
                print(event.get("text", ""), flush=True)
            elif event.get("type") == "tool_call":
                print("[工具] " + str(event.get("tool", "")), flush=True)
        code = child.wait()
        if code:
            print("任务未完成（退出码 %s），可重试或输入 /new。" % code, flush=True)
    except KeyboardInterrupt:
        child.terminate()
        child.wait()
        print("任务已中断。", flush=True)
