"""只接收公有证书、模型名；上游秘密和管理凭据绝不能进入此进程。"""
import json
from pathlib import Path
import sys

data = json.load(sys.stdin)
cert = data["ca"]
if "PRIVATE KEY" in cert or not cert.startswith("-----BEGIN CERTIFICATE-----"):
    raise SystemExit("invalid public certificate")
Path("/tmp/validation-ca.crt").write_text(cert)
Path("/etc/ssl/certs/ca-certificates.crt").write_text(
    Path("/opt/validation/system-ca.crt").read_text() + "\n" + cert)
config = Path("/home/agent/.codex")
config.mkdir(mode=0o700, parents=True, exist_ok=True)
# JSON 字符串也是合法 TOML 基本字符串，避免模型名被解释成配置语法。
(config / "config.toml").write_text(
    f'model = {json.dumps(data["model"])}\n'
    'model_provider = "validation"\n'
    'approval_policy = "never"\n'
    'sandbox_mode = "danger-full-access"\n'
    '[model_providers.validation]\n'
    'name = "AIofCage isolated validation"\n'
    'base_url = "https://xapi.qiniu.io/v1"\n'
    'env_key = "CAGE_MODEL_PLACEHOLDER"\n'
    'wire_api = "responses"\n'
    'request_max_retries = 0\n'
    'stream_max_retries = 0\n'
    'stream_idle_timeout_ms = 60000\n'
)
