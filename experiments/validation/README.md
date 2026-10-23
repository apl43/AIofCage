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
