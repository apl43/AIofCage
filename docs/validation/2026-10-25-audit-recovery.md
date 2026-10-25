# 审计恢复链路验证

本次围绕日志与用量可信度完善事件订阅契约。测试使用合成记录、临时目录和一次性空白沙箱；不需要供应商密钥或模型调用。

基于 `5ff5f78`（本次改动前 main），环境为 macOS / Apple Silicon、专用 `colima-aiofcage` Docker context。

## 修复前复现

`TestUsageQueryKeepsGapBesideRequest` 在修复前失败：

- 请求先写入、同 ID 的缺口后写入：缺口数为 0，应为 1。
- 缺口先写入、同 ID 的请求后写入：请求数为 0，应为 1。

代码复查另确认两处恢复问题：`record` 的写盘错误没有传回事件客户端，后续成功记录可以推进游标；`follow` 在门卫运行变化时清空旧游标，使服务端无法报告跨运行缺口。

## 自动化回归

```bash
go test -race ./internal/runtime ./internal/service \
  -run 'TestEventStream|TestAudit|TestUnreadableAudit|TestUsageQuery' -count=1
make test
make check
node --experimental-default-type=module --test frontend/tests/*.test.js frontend/tests/*.test.mjs
git diff --check
```

覆盖接收方拒绝确认后停止投递、无效事件不被跳过、SSE 类型检查、真实 HTTP 门卫的跨运行补发、磁盘读取与写入失败、残行保留、沙箱归属校验，以及缺口与请求 ID 冲突时的去重。

## 专用运行环境验证

```bash
AIOFCAGE_AUDIT_E2E=1 go test -race ./internal/service \
  -run '^TestAuditRecoveryEndToEnd$' -count=1 -v
```

该入口使用临时配置目录和 `review-audit-recovery-` 项目，创建一个没有供应商和 MCP 的空白沙箱。主动向保留域名发送明文 HTTP，由门卫本地返回 403，不请求模型或公网服务。

验证步骤：

1. 第一条拒绝事件保存成功。
2. 临时用目录占住此测试自己的日志文件路径，触发真实写盘失败；观察采集中断提示。
3. 故障期间产生两条拒绝事件，恢复文件路径后，确认从原游标补齐两条事件。
4. 只停止/启动此测试沙箱，确认出现 `cursor_from_other_run` 缺口，然后继续记录第 4 条拒绝事件。
5. 查询得到缺口数 1、模型请求数 0；清理此测试的容器、网络、home、项目与临时数据。

首次运行发现测试把被拒的明文 HTTP 事件误当成包含 host/path 的记录；实际事件在读取目标元数据前就以 `plain_http_denied` 拒绝。已按真实事件语义修改断言，不修改产品行为。

## 结果与边界

| 检查 | 实际结果 |
| --- | --- |
| 定向 Go 竞态回归 | 通过；包括真实 HTTP 门卫和临时日志故障。 |
| `make test` | 全部 internal 包的竞态回归通过；显式启用的外部环境测试默认跳过。 |
| `make check` | Go vet 与 production 构建通过；链接器仍有既有的 `-lobjc` 重复警告。 |
| 前端测试与 `main.js` 语法 | 通过；共 10 项前端测试。 |
| 专用环境 E2E | 通过，测试本体 6.09 秒；写盘恢复、跨运行缺口和第 4 条拒绝事件均实际观察到，模型请求数为 0。 |
| `git diff --check` | 通过。 |
| 原生界面的通知样式 | 跳过；本次不重启用户正在使用的桌面应用。后端通知级别、前端分流与语法已检查，不等同视觉验收。 |

测试证明上述可复现恢复路径，不构成完整操作审计、无限离线补发、防篡改或容器逃逸防护的证明。

运行中的用户桌面应用不需要为本次测试重启。现有沙箱、供应商、数据库与 MCP 不属于此测试的修改或清理范围。
