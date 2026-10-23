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

需要 macOS Apple Silicon、Python 3.11+、Go 1.26.4+、Docker CLI、Colima（含 Lima）。`doctor` 只读检查，不安装工具、不启动 VM。未启动专用实例时会返回非零状态并给出缺项。

后续操作和实测证据随对应阶段补齐；此时不能把目录骨架当作已可运行的容器实验。

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
