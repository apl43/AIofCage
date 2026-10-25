"""验证 CLI 的结构化投影与消息交付；不访问真实模型。"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

source = Path(sys.argv[1]).resolve()
sys.argv = sys.argv[:1]
spec = importlib.util.spec_from_file_location("agent_turn", source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class TurnTests(unittest.TestCase):
    def project(self, agent, events):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            projection = module.Projection(agent)
            for event in events:
                projection.consume(event)
        return [json.loads(line) for line in output.getvalue().splitlines()]

    def test_dsh_final_is_lossless_and_tools_do_not_expose_arguments(self):
        final = "回答" * 10000
        events = self.project("dsh", [
            {"type": "session", "sessionId": "session-demo"},
            {"type": "text", "text": "回答被截断"},
            {"type": "tool_call", "tool": "query", "input": {"private": "not-for-display"}},
            {"type": "final", "text": final}])
        self.assertEqual(events[-1], {"type": "final", "text": final})
        self.assertNotIn("private", json.dumps(events))
        self.assertEqual(events[0]["text"], "session-demo")

    def test_claude_deltas_are_not_duplicated(self):
        events = self.project("claude-code", [
            {"type": "stream_event", "event": {"delta": {"type": "text_delta", "text": "你好"}}},
            {"type": "assistant", "message": {"content": [{"type": "text", "text": "你好"}]}},
            {"type": "result", "session_id": "test-session", "is_error": False}])
        self.assertEqual([event["text"] for event in events if event["type"] in ("text", "delta")], ["你好"])

    def test_codex_projection_and_errors(self):
        events = self.project("codex", [
            {"type": "thread.started", "thread_id": "thread-demo"},
            {"type": "item.completed", "item": {"type": "agent_message", "text": "完成"}},
            {"type": "turn.failed", "error": {"message": "模型不可用"}}])
        self.assertEqual([item["type"] for item in events], ["session", "text", "error"])
        self.assertEqual(events[-1]["text"], "模型不可用")

    def test_command_preserves_text_and_models_as_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            body = "第一行\n第二行 $(echo should-not-run) `text`"
            base = {"text": body, "images": None, "model": 'model/"quoted"', "effort": "high", "resume_id": "saved"}
            args, task = module.command({**base, "agent": "codex"}, directory)
            self.assertEqual(task, body)
            self.assertEqual(args[:3], ["codex", "exec", "resume"])
            self.assertIn('model="model/\\"quoted\\""', args)
            self.assertEqual(args[-2:], ["saved", "-"])
            args, task = module.command({**base, "agent": "dsh", "dsh_patch": "reasoningEffort: high\n"}, directory)
            self.assertLess(args.index("--patch"), args.index("--json"))
            self.assertEqual(Path(args[args.index("--patch") + 1]).stat().st_mode & 0o777, 0o600)
            self.assertEqual(task, body)

    def test_real_subprocess_pipeline_has_no_terminal_sequences(self):
        with tempfile.TemporaryDirectory() as directory:
            cli = Path(directory) / "codex"
            cli.write_text("#!/usr/bin/env python3\nimport json,sys\nbody=sys.stdin.read()\nprint(json.dumps({'type':'thread.started','thread_id':'demo'}))\nprint(json.dumps({'type':'item.completed','item':{'type':'agent_message','text':body}}))\n")
            cli.chmod(0o755)
            text = "多行\n任务"
            payload = {"agent": "codex", "model": "test-model", "text": text, "images": []}
            result = subprocess.run([sys.executable, "-B", str(source)], input=json.dumps(payload),
                                    text=True, capture_output=True,
                                    env={**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]})
            self.assertEqual(result.returncode, 0, result.stderr)
            events = [json.loads(line) for line in result.stdout.splitlines()]
            self.assertEqual(events[-1]["text"], text)
            self.assertNotIn("\x1b", result.stdout)


unittest.main(verbosity=2)
