# 前端第三方库

固定版本的 ES 模块文件，应用不需要 npm 或打包步骤。通过 index.html 的 import map 引用。

| 文件 | 来源 | npm integrity |
|---|---|---|
| preact-10.29.8.module.js | preact@10.29.8 dist/preact.module.js | sha512-ej2aVZ+vZ8WO7tvlQWRM9N63A0KzF9q4mWJfDUHgYaIofWY9hu74QdnQrjoPMmZi2/nZ5gN0bJCQF49xQqx09Q== |
| preact-hooks-10.29.8.module.js | preact@10.29.8 hooks/dist/hooks.module.js | 同上 |
| htm-3.1.1.module.js | htm@3.1.1 dist/htm.module.js | sha512-983Vyg8NwUE7JkZ6NmOqpCZ+sh1bKv2iYTlUkzlWmA5JD2acKoxd4KVxbMmxX/85mtfdnDmTFoNKcg5DGAvxNQ== |
| xterm-6.0.0.module.js、xterm-6.0.0.css | @xterm/xterm@6.0.0 lib/xterm.mjs、css/xterm.css | sha512-TQwDdQGtwwDt+2cgKDLn0IRaSxYu1tSUjgKarSDkUM0ZNiSRXFpjxEsvc/Zgc5kq5omJ+V0a8/kIM2WD3sMOYg== |
| xterm-addon-fit-0.11.0.module.js | @xterm/addon-fit@0.11.0 lib/addon-fit.mjs | sha512-jYcgT6xtVYhnhgxh3QgYDnnNMYTcf8ElbxxFzX0IZo+vabQqSPAjC3c1wJrKB5E19VwQei89QCiZZP86DCPF7g== |

## 许可证

- preact：MIT，全文见 `preact-10.29.8.LICENSE.txt`。
- htm：Apache-2.0，全文见 `htm-3.1.1.LICENSE.txt`。
- xterm.js 与 addon-fit：MIT，全文见 `xterm-6.0.0.LICENSE.txt`、`xterm-addon-fit-0.11.0.LICENSE.txt`。用于内置终端。文件未作修改。
