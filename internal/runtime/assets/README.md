# 运行时资源

这些文件会编译进桌面应用和 `cagectl`，用来在专用虚拟机里构建镜像。

- `gatekeeper/Dockerfile`：门卫镜像。`gatekeeper/gatekeeper-linux-arm64` 由 `make gatekeeper-linux` 生成，不提交到仓库。
- `agent/`：Agent 镜像模板、启动时写入 CA 与配置的脚本、命令包装器和 `.bashrc`。
