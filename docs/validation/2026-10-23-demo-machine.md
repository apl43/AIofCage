# 演示机复验结果（2026-10-23）

结论：在演示用的 Mac 上复跑第一批验证包，**33 项通过、0 项失败、5 项阻塞**。与首批相比，IPv4 直连隔离从“阻塞”变为“通过”（换用当地可达的对照地址后，正向对照成立）。真实 Codex 任务仍阻塞：本机当前网络连不上 `xapi.qiniu.io`。设计状态不变。

## 环境

| 项目 | 实际值 |
|---|---|
| 时间 | 2026-10-23 20:34（上海时间） |
| 代码 | `677dd19` + 本分支对验证包的对照地址参数改动（报告中 `working_tree_dirty=true` 即此改动） |
| 宿主 | macOS 26.6.1，Apple Silicon arm64 |
| Go | go1.26.4 darwin/arm64 |
| Colima / Lima | 0.10.3 / 2.2.1，Homebrew arm64 版本（`/opt/homebrew`），未使用 `.validation-runs/tools` |
| VM | `aiofcage`，aarch64，vz，virtiofs，4 核 / 6 GiB / 40 GiB；`setup` 只读校验通过 |
| Docker | CLI 29.8.1；专用 VM Engine 29.5.2；默认 context 仍为 `desktop-linux` |
| 对照地址 | IPv4 `223.5.5.5`（本机网络不可达 `1.1.1.1`）；IPv6 保持默认 |
| Agent 产物 | `codex-cli 0.164.0`，Linux/arm64 |
| 凭据 | 随机合成秘密和管理 token；未读取真实 key |

逐项结果见 [demo-machine-container.json](2026-10-23-demo-machine-container.json)。

## 与首批结果的差异

| 检查 | 首批（队友机器） | 本次（演示机） | 原因 |
|---|---|---|---|
| `public-ipv4-control` | 通过 | 通过（对照地址改为 `223.5.5.5`） | 本机网络到 `1.1.1.1:443` 超时；用默认地址时该项及下面两项都会变成阻塞 |
| `direct-ipv4`、`unset-proxy-direct` | 通过 | 通过 | 对照成立后才能评估直连隔离 |
| `direct-dns` | 通过 | 通过，目标改为 `223.5.5.5` | 对照地址同时用作直连 DNS 服务器，这是一个真实可用的公共解析器 |
| 其余 30 项 | 通过/阻塞 | 相同 | — |

## 仍然阻塞的 5 项

| 项目 | 原因 | 下一步 |
|---|---|---|
| `public-ipv6-control`、`direct-ipv6` | VM 出网网段没有可用的 IPv6 外网，缺少正向对照 | 宿主有 IPv6 地址，但 Colima VM 未配置 IPv6 出网；需要有 IPv6 出网的环境再验 |
| `codex-real-task` | 本机当前网络到 `xapi.qiniu.io`（38.127.133.103:443）直连和经本机代理都超时；同时 `api.qnaigc.com`、GitHub 等均正常 | 换到能访问 xapi 的网络后运行 `run --live --control-ipv4 223.5.5.5` |
| `dns-packet-egress` | 只验证了解析失败，没有 VM 外抓包证据 | 同首批 |
| `lan-management` | 没有从另一台局域网机器发起访问 | 同首批 |

## 本机复现注意事项

1. **Docker Hub 在本机网络不可用。** `registry-1.docker.io` 被解析到错误地址并拒绝连接，Docker Desktop 同样拉不下来。验证包的 Dockerfile 用的是 Docker Hub 官方镜像名，所以先从 AWS 公共镜像库（Docker 官方镜像的镜像站）拉取，再打上原名标签：

   ```bash
   D() { docker --context colima-aiofcage "$@"; }
   D pull public.ecr.aws/docker/library/debian:bookworm-slim
   D pull public.ecr.aws/docker/library/node:22.16.0-bookworm-slim
   D tag public.ecr.aws/docker/library/debian:bookworm-slim debian:bookworm-slim
   D tag public.ecr.aws/docker/library/node:22.16.0-bookworm-slim node:22.16.0-bookworm-slim
   ```

   构建过程中的 `apt-get` 和 `npm install` 在 VM 内直连可用，不需要代理。产品镜像应直接使用 `public.ecr.aws/docker/library/...` 并固定摘要，不依赖 Docker Hub。

2. **门卫交叉编译需要 Go 模块镜像。** 本机访问 `proxy.golang.org` 返回 502，构建时临时使用 `GOPROXY=https://goproxy.cn,direct`。

3. **公共对照地址可配置。** 验证包新增 `--control-ipv4` / `--control-ipv6`（默认值不变），只用于 TCP 建连和直连 DNS 探测，不发送应用数据。

## 本机保留内容

专用 VM 保持运行；验证镜像、两次实验的无秘密练习项目（`~/CageWorkspace/run-*`）和被忽略的报告目录保留。两次实验的容器和网络均已按标签清理。
