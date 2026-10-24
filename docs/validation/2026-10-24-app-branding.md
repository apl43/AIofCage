# 应用品牌与图标检查

本轮将本机已有的品牌改动整合为独立功能分支，基于终端图片功能 PR #34。

- 界面、窗口与应用显示名称统一为 `AiOf Cage`；保留 `io.aiofcage.desktop` 应用标识和原有数据路径。
- SVG 为唯一设计源，侧栏、浏览器图标和 macOS 图标使用同一图形。`make icons` 可重新生成 PNG 与 ICNS，`make app` 将 ICNS 复制到应用并签名。
- `make icons`、`make app`、`make check`、8 项前端测试和 `codesign --verify --deep --strict` 均通过。
- 校验 1024 × 1024 PNG、ICNS 解包后的 10 个标准图标档位、打包图标与源图标字节一致、bundle 名称与标识；已目视检查 PNG。
- 本轮原工作树的未提交改动保留原状，验证与提交在独立工作树进行。后续视觉优化在此功能之上继续，使用同一品牌资产。

编译仅出现已有的重复 `-lobjc` 警告。
