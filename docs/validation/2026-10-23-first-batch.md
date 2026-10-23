# 第一批验证结果（2026-10-23）

结论：验证包可运行，本地门卫回归与本机容器反向检查已取得部分证据；**G1、G2、G4 尚未整体通过，设计仍为草案**。没有真实模型任务或 MCP 成功证据，不勾选产品功能。

## 环境与代码

| 项目 | 实际值 |
|---|---|
| 时间 | 2026-10-23 19:38（上海时间） |
| 容器实验代码 | `be7cc63970ca331213a69df5024862ab7b56e7b0`，运行时工作区干净 |
| 执行方式 | Codex 在本机执行脚本；尚未经另一位队友独立复现 |
| 宿主 | macOS 26.2，Apple Silicon arm64 |
| Go | go1.26.4 darwin/arm64 |
| Colima / Lima | 0.10.3 / 2.1.2，官方 arm64 发行包，校验和固定在运行器 |
| VM | `aiofcage`，aarch64，vz，virtiofs，4 核 / 6 GiB / 40 GiB 数据盘 |
| 共享 | 实际 Lima 配置与 `/proc/mounts` 均仅有 `~/CageWorkspace:w` |
| Docker | CLI 29.3.1；专用 VM Engine 29.5.2 |
| 原默认 context | `desktop-linux`，实验后仍为该值 |
| goproxy | v1.9.2，独立 Go module 固定依赖 |
| Agent 产物 | Linux/arm64，`codex-cli 0.164.0`；版本命令成功，不代表模型认证已通过 |
| 模型配置 | 七牛 `https://xapi.qiniu.io/v1`，候选 `gpt-6.1-sol`，本次未调用模型 |
| 凭据 | 随机合成上游秘密和管理 token；真实模型任务按用户指示保留为阻塞 |

镜像内容摘要：

- 门卫：`sha256:6c00f22ed30dde9a2b05ca88f89cb3e0d88745a1505c3f617e62771d987706b4`
- Agent：`sha256:4dc31f62e015df2751802d38e4272f3a0b1f8b8627e5be28925854a397e99d86`

基础镜像和 apt 软件包尚未全部固定到不可变快照；跨日期构建需重新记录镜像摘要并复验。

## 本地门卫与运行器回归

```bash
cd experiments/validation
go test -race ./...
go vet ./...
cd ../..
python3 -m unittest discover -s experiments/validation -p 'test_*.py'
```

- Go 的 9 个顶层测试（含拒绝矩阵子用例）通过，race detector 和 `go vet` 无错误。
- 7 个 Python 测试通过，包括专用 context、错误不回显秘密、清理范围、跨分块秘密匹配、gzip 镜像层扫描、拒绝额外共享。
- Go 使用受控 HTTPS 上游验证真正的 CONNECT/TLS、授权后注入、请求体不改写、认证拒绝、目标不一致、非法证书、重定向、SSE 流式返回和并发上限。上游映射仅在测试代码存在。
- 实验发现 goproxy 在上游 TLS 失败时默认直接断连；验证包在 RoundTripper 层转换为脱敏 502，并验证没有 HTTP 请求到达不受信任的上游。

这些是本机自动测试证据，不是完整威胁模型或生产就绪证明。

## 专用容器实测

复现入口：

```bash
python3 experiments/validation/lab.py tools
python3 experiments/validation/lab.py setup
python3 experiments/validation/lab.py build
python3 experiments/validation/lab.py run
```

本次 ID 为 `run-20261023-193800-0da490`。脱敏的逐项证据见 [container.json](2026-10-23-container.json)，**33 项 pass、0 项 fail、5 项 blocked**。存在 blocked，因此命令退出码为 1。

| 范围 | 已观察到的结果 |
|---|---|
| VM 与容器 | 专用共享、固定资源、非 root、capabilities 为零、no-new-privileges、只读根文件系统、不挂载 Docker socket |
| 项目边界 | 项目外哨兵及指向哨兵的符号链接都不可读 |
| IPv4 | 同 VM 出网网段的 TCP 对照成功；Agent 自建 socket 与清空代理后的直连失败 |
| 路由与内部发现 | Agent 无 IPv4/IPv6 默认路由；内部 `gatekeeper` 别名可解析 |
| 管理面 | 宿主回环鉴权成功，模型占位符被拒；macOS 实际监听只在回环；Agent 经门卫内网/出网地址、VM 网关都不可建立管理连接 |
| HTTP 代理 | 容器信任实验 CA；未知目标、元数据地址、管理地址、未授权方法/路径、外来认证均得到明确 403 |
| DNS 解析 | Docker 内部 DNS 和直连 DNS 均未返回外部查询结果；这一结果不等同于没有报文外传 |
| 秘密交付 | 两个容器的 inspect 元数据不含本次上游秘密或管理 token；Agent 环境、可读进程参数、home/tmp/项目及两个镜像的全部导出层扫描为零命中 |
| 事件 | 6 条固定字段的脱敏拒绝事件，不含秘密、占位符原文或任意请求正文 |
| 清理 | 自动清理成功；再次查询实验标签，遗留容器和网络均为零 |

秘密扫描的匹配值只在宿主，未作为参数、环境变量或文件交给 Agent。零命中仅针对本次合成秘密的原始字节，不能证明所有秘密或变换编码都不存在。真实 Keychain—门卫—Codex 链路仍未验证。

## 阻塞项与后续实验

| 项目 | 为什么未通过 | 下一步 |
|---|---|---|
| IPv6 出网对照与 Agent 直连（2 项） | VM 出网网段也无法连接公共 IPv6，缺少有效正向对照 | 在具备 IPv6 外网的受控环境复验；不能把“没有 IPv6 网络”说成“IPv6 隔离已验证” |
| Codex 真实任务 | 本机不能读取文档指定的实验 Keychain 项；用户明确本次先交付验证包 | 保存独立测试凭据后运行 `run --live`；验证修改文件、实际跑测试、宿主独立断言和真实凭据零命中 |
| DNS 报文外传 | 本次只观察解析结果，没有 VM 外部抓包/受控权威 DNS 证据 | 增加可观察出口的 DNS 对照与泄漏测试 |
| LAN 管理访问 | 已检查本机回环监听，未从另一台局域网主机发起访问 | 在第二台机器上对本次随机管理端口进行反向检查 |

下一批还需验证一个 HTTP MCP 的初始化、只读工具、资源限制与撤销，再推进其余 Agent、OAuth 和密码连接器。上述缺口不能以增加 mock 或修改状态表代替。

## 本机保留内容

仅保留专用 VM、两张实验镜像、`~/CageWorkspace/run-20261023-193800-0da490` 的无秘密练习项目，以及被 Git 忽略的工具/构建输出/报告。没有删除用户项目、改变默认 context、修改 SSH 配置或注册登录启动服务。

实测结束后停止专用 VM，释放运行资源；实例磁盘、镜像与报告仍保留。再次运行前按验证包说明启动专用实例并复核配置。

本机原 Homebrew 位于 `/usr/local`，安装 Colima 时获得 x86_64 产物，不满足本次原生验证条件；随后改用经过校验的 arm64 工具，放在 `.validation-runs/tools`，未替换 Homebrew 二进制。后续复现可直接使用 `tools` 命令，无需该 Homebrew 安装。

另观察到：仅用 arm64 Colima 的路径启动命令、却让它从全局 PATH 调用 x86_64 Lima，会错误报告 VM 未运行。运行器会把整套 arm64 工具目录加入 PATH；手动停止/重启命令也必须使用这一 PATH。
