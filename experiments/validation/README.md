# 第一批可行性验证包

这是独立、可丢弃的实验，不是产品入口，也不代表 G0—G5 已通过。正式模块仍须等设计冻结后实现。

本批只验证专用 Colima 环境、模型 MITM 门卫、一个 Codex 真实任务，以及网络/管理面/秘密的反向检查。MCP、Claude Code、dsh、OAuth、数据库连接器和桌面流程仍待后续实验。

## 分阶段产出

1. 环境检查和证据格式：`python3 experiments/validation/lab.py doctor`。
2. 固定依赖的最小门卫与本地安全回归测试。
3. 专用环境编排、隔离探针、Codex 任务与清理。
4. 脱敏实测记录、限制和下一步。

每阶段独立 commit。实验代码集中在本目录，Go 使用独立 module，不给根模块引入产品依赖。

## 约束

- 只使用 `aiofcage` profile 和 `colima-aiofcage` context；每次 Docker 调用显式传 context，不切换默认实例。
- VM 只共享 `~/CageWorkspace`，实验创建自己的子目录，不导入当前真实项目。
- 上游密钥只从宿主 Keychain 读取，在内存中通过标准输入/管理接口传递；不进入命令参数、普通环境变量、配置文件或报告。
- 运行输出写入被 Git 忽略的 `.validation-runs/`。只把经过检查的摘要提交到 `docs/validation/`。
- `pass`、`fail`、`blocked` 是单个实验检查的结果；不自动改变需求文档中的“待验证”状态。缺少工具、凭据或测试条件应记为 `blocked`，不能算通过。
- 网络请求有超时；清理只按本次实验标签操作，不执行全局 prune，不删除用户项目或已有 profile。

## 环境检查

```bash
python3 experiments/validation/lab.py doctor
python3 -m unittest discover -s experiments/validation -p 'test_*.py'
```

需要 macOS Apple Silicon、Python 3.12+、Go 1.26.4+、Docker CLI、Colima（含 Lima）。`doctor` 只读检查，不安装工具、不启动 VM。它始终把尚未运行的隔离验收列为阻塞，不把工具存在当作安全验证通过。

## 门卫本地实验

```bash
cd experiments/validation
go test -race ./...
go vet ./...
```

固定 `goproxy v1.9.2`。测试在宿主启动临时 HTTPS 上游和代理，通过真实 CONNECT/TLS 检查占位符注入、请求体不改写、未知方法/路径、外来/重复认证、CONNECT 与 Host 不一致、私网地址、重定向、非法上游证书、SSE 首段及时返回、并发请求数上限、管理鉴权与事件脱敏。只用合成凭据，不连接真实模型。

可执行程序必须传入两个不同的具体接口地址；从 stdin 一次性接收 bootstrap，未获得完整配置不会监听。CA 每次运行新建，私钥仅在内存，管理接口只返回公有证书。代理仅接受固定目标的 HTTPS `POST` 和精确路径，目标连接先校验解析 IP，再直接拨已校验地址；不读取上游代理环境变量。

实验限制：最多 100 次请求，默认由运行器进一步收紧；只接受已知长度且不超过 2 MiB 的请求体；全部重定向拒绝；错误响应不透传上游正文。成功响应中的业务数据仍信任模型服务。管理事件是最多 256 条的内存 JSON 快照，带序号，**不是**产品设计中的 SSE 补发/持久化审计。没有 MCP、动态授权、OAuth 或生产会话恢复。

本地测试中的上游地址映射和临时信任根只存在于 `_test.go`，可执行程序不提供关闭 TLS 校验或放行私网的开关。本地通过不能代替 Colima 内的隔离和真实 Agent 验收。

## 在专用实例中运行

从仓库根目录依次执行：

```bash
# 可选：缺少原生 arm64 工具时，下载校验过的固定版本到忽略目录。
python3 experiments/validation/lab.py tools

# 仅首次创建：aiofcage，vz/virtiofs，4 核、6 GiB、40 GiB 数据盘。
python3 experiments/validation/lab.py setup

# 构建 Linux/arm64 镜像，固定 Codex 0.164.0；此阶段不读取凭据。
python3 experiments/validation/lab.py build

# 无真实凭据的隔离实验；使用随机合成秘密，不调用模型。
python3 experiments/validation/lab.py run
```

出网正向对照默认连接 `1.1.1.1:443` 和 `2606:4700:4700::1111`。所在网络访问不到默认地址时，用 `--control-ipv4` / `--control-ipv6` 换成当地可达的公共地址（例如 `--control-ipv4 223.5.5.5`），否则直连检查只能记为阻塞。对照地址同时用作 Agent 的直连 DNS 探测目标，只做 TCP 建连和 DNS 查询，不发送应用数据。

`tools` 固定 Colima 0.10.3、Lima 2.1.2，校验官方发布的 SHA-256。若 `.validation-runs/tools/bin` 存在，后续脚本优先使用其中的原生工具，避免 `/usr/local` Homebrew 在 Apple Silicon 上安装 x86_64 版本。不会替换全局二进制或注册登录启动服务。

`setup` 禁用用户 Colima 模板、默认 context 切换和 SSH agent 转发。已有配置只读校验，不覆写、不自动重启；已有实例停止时，先检查其共享配置，再由操作者启动原配置。运行前从 Lima JSON、VM `/proc/mounts` 和 Docker context socket 复核实际边界；不接受同名但指向其他引擎的 context。

`run` 为每次实验创建独立项目、内部 IPv4/IPv6 网络、出网网络、门卫与非 root Agent。项目中只有加法练习和测试；为允许 UID 1000 操作，只有这个无秘密的实验副本目录开放写权限，不改变真实项目所有者。公有 CA、Agent home 和临时文件进入 tmpfs，Agent 根文件系统只读。Agent DNS 只允许 Docker 内部服务发现，外部解析显式禁用。

门卫的秘密和管理 token 经 `docker start --attach --interactive` 的 stdin 交付，只存在宿主/门卫内存；不通过 `docker -e`。管理监听只绑定出网接口并发布宿主随机回环端口；代理只绑定 Agent 内网。实测会检查 macOS 的真实监听地址。

报告包含各检查的 `pass/fail/blocked` 和实际镜像 ID。IPv4/IPv6 使用同 VM 出网网段的正向对照；对照也不通时不会把 Agent 的直连失败写为通过。外部 DNS 解析失败不等于已证明 DNS 报文未外传，VM 外部抓包/权威 DNS 观察和另一台 LAN 主机的管理访问测试目前明确为阻塞。

## 真实 Codex 任务

先确认无凭据实验中没有 `fail`，再用独立测试账号的七牛 xapi key。凭据在宿主 Keychain 中预先保存，默认 service 为 `sbx-builderday-key`、account 为当前 macOS 用户；不要把 key 放进参数、环境变量、仓库或聊天记录。

```bash
python3 experiments/validation/lab.py run --live --model gpt-6.1-sol
# 使用不同的已保存 Keychain 项时，只传名称，不传 key：
python3 experiments/validation/lab.py run --live --keychain-service YOUR_TEST_SERVICE
```

最多 12 次模型请求，任务限时 180 秒，不允许自动重试或联网安装依赖。容器内 Codex 的 `danger-full-access` 仅用于本实验的外部容器隔离，不能在宿主套用。要求 Agent 实现 `add` 并实际执行 unittest；宿主检查结构化命令结果、测试文件未变，并另外注入独立断言验证实现。输出只在宿主内存扫描，报告不保存终端正文。

真实 key 缺失时标记阻塞。模型/客户端/证书链兼容性只有真实运行通过才能确认；本地受控上游测试和 `codex --version` 均不能替代它。

Codex 配置依据：[自定义模型提供方](https://developers.openai.com/codex/config-advanced/)与[非交互运行](https://developers.openai.com/codex/noninteractive/)，2026-10-23 查阅。版本记录指本实验固定版本，不表示通用最低版本。

## 报告、退出与清理

无论成功或失败，正常退出都会删除本次标签下的容器和网络；实验项目和报告保留。门卫/Agent 镜像与专用 VM 保留供下一轮使用。脚本退出码 `0` 表示该报告全部检查通过，`1` 表示存在失败或阻塞；无凭据运行中的真实任务、缺少 IPv6 对照等会使退出码为 `1`，请查看具体项目。

```bash
# 进程被强制杀死时，用启动时打印的 ID 清理，不影响其他实验。
python3 experiments/validation/lab.py cleanup --run-id RUN_ID

# 确认不再需要时，可手动停止专用实例；不删除实例、镜像或项目。
PATH="$PWD/.validation-runs/tools/bin:$PATH" colima --profile aiofcage stop
```

重启已验证且未改动的专用配置时使用 `PATH="$PWD/.validation-runs/tools/bin:$PATH" colima --profile aiofcage start --activate=false`，随后重新运行 `setup` 校验实际配置。Colima 和 Lima 必须来自同一套 arm64 工具；只指定 Colima 的绝对路径而仍调用全局 x86_64 Lima，可能错误报告实例未运行。

秘密扫描在宿主执行：读取 Agent 的环境、可读进程参数、home/tmp/项目，以及两个镜像的导出元数据和所有层。匹配值不作为扫描参数传进 Agent。扫描命令失败、超时或不支持的压缩格式均不记为零命中。合成秘密的零命中只证明本次实验的交付路径；真实凭据链路仍需 `--live` 复验。

当前基础镜像使用版本标签，构建时系统软件包可能变化；报告记录实际镜像内容 ID，不承诺跨日期逐字节重建。该实验还未实现 MCP、跨会话攻击矩阵、生产审计和撤销。完整通过条件仍以需求文档为准。
