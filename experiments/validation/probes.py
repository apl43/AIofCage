"""在 Agent 中运行的反向检查。只输出状态与固定描述，不输出请求或文件正文。"""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys

cfg = json.load(sys.stdin)
checks = []


def record(name, passed, detail):
    checks.append({"name": name, "status": "pass" if passed else "fail", "detail": detail})


def execute(args, timeout=8, env=None):
    try:
        return subprocess.run(args, capture_output=True, timeout=timeout, env=env)
    except subprocess.TimeoutExpired:
        return subprocess.CompletedProcess(args, 124, b"", b"")


clean_env = {k: v for k, v in os.environ.items() if "proxy" not in k.lower()}
record("non-root", os.getuid() == 1000, "Agent UID 必须为 1000")
status = Path("/proc/self/status").read_text()
record("capabilities", "CapEff:\t0000000000000000" in status and "NoNewPrivs:\t1" in status,
       "有效 capabilities 为零，no-new-privileges 已生效")
record("docker-socket", not Path("/var/run/docker.sock").exists(), "Agent 不挂载 Docker socket")
for name, filename in (("host-sentinel", cfg["sentinel"]), ("symlink-escape", "escape-link")):
    try:
        Path(filename).read_bytes()
        readable = True
    except OSError:
        readable = False
    record(name, not readable, "项目外哨兵不可读取")
try:
    Path("/rootfs-write-probe").write_text("probe")
    writable = True
except OSError:
    writable = False
record("read-only-root", not writable, "根文件系统不可写")
for family in ("-4", "-6"):
    route = execute(["ip", family, "route", "show", "default"])
    record("no-default-route" + family, route.returncode == 0 and not route.stdout.strip(),
           "检查实际路由表，不以代理环境变量代替隔离")

# 自建 socket，不依赖 curl 是否支持代理或 IPv6。固定公共地址不发送应用数据。
for name, family, address in (("direct-ipv4", socket.AF_INET, "1.1.1.1"),
                              ("direct-ipv6", socket.AF_INET6, "2606:4700:4700::1111")):
    sock = socket.socket(family, socket.SOCK_STREAM)
    sock.settimeout(2)
    try:
        sock.connect((address, 443))
        connected = True
    except OSError:
        connected = False
    finally:
        sock.close()
    record(name, not connected, "自建 TCP socket 无法直连公共 IP")
direct = execute(["curl", "--noproxy", "*", "--connect-timeout", "2", "--max-time", "3",
                  "--silent", "https://1.1.1.1"], env=clean_env)
record("unset-proxy-direct", direct.returncode != 0, "清空全部大小写代理变量后仍无法直连")

for name, host, port in (("admin-via-agent-interface", "gatekeeper", 8899),
                         ("admin-via-egress-interface", cfg["admin_ip"], 8899),
                         ("vm-gateway-admin", cfg["gateway"], 8899)):
    try:
        with socket.create_connection((host, port), timeout=2):
            connected = True
    except OSError:
        connected = False
    record(name, not connected, "Agent 不可建立到管理面的 TCP 连接")

# Docker 内部服务发现必须成功；外部 DNS 禁用。该探针只证明解析结果，不证明任意 UDP 均未离机。
try:
    socket.getaddrinfo("gatekeeper", 8888)
    resolved = True
except OSError:
    resolved = False
record("internal-service-discovery", resolved, "内部别名可解析，排除所有探针因网络完全故障而失败")
for name, server in (("docker-dns-external", "127.0.0.11"), ("direct-dns", "1.1.1.1")):
    result = execute(["dig", "@" + server, "example.com", "A", "+time=1", "+tries=1", "+short"])
    # 没有答案不能证明没有 DNS 报文离开 VM；完整泄漏证据由宿主单独记录。
    answered = result.returncode == 0 and any(line and not line.startswith(b";") for line in result.stdout.splitlines())
    record(name, not answered, "禁止外部 DNS 解析；报文外传仍需独立观察")

for name, target in (("proxy-admin-denied", cfg["admin_ip"] + ":8899"),
                      ("proxy-metadata-denied", "169.254.169.254:443"),
                      ("proxy-unknown-host-denied", "example.com:443")):
    with socket.create_connection(("gatekeeper", 8888), timeout=2) as sock:
        sock.settimeout(3)
        sock.sendall(f"CONNECT {target} HTTP/1.1\r\nHost: {target}\r\n\r\n".encode())
        response = sock.recv(256)
    record(name, response.startswith(b"HTTP/1.1 403"), "代理明确返回 403，不能转发到未授权目标")

placeholder = os.environ["CAGE_MODEL_PLACEHOLDER"]
for name, method, path, auth in (("proxy-method-denied", "GET", "/v1/responses", placeholder),
                               ("proxy-path-denied", "POST", "/v1/files", placeholder),
                               ("proxy-foreign-auth-denied", "POST", "/v1/responses", "synthetic-foreign-account")):
    result = execute(["curl", "--silent", "--output", "/dev/null", "--write-out", "%{http_code}",
                      "--max-time", "5", "--request", method, "--data", "{}",
                      "--header", "Authorization: Bearer " + auth, "https://xapi.qiniu.io" + path])
    record(name, result.returncode == 0 and result.stdout == b"403", "经容器 CA 完成 MITM 后被策略明确拒绝")

print(json.dumps(checks))
