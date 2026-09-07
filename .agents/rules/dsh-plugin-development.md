# DSH 插件开发与调试规则

在此工作区开发、修改或调试 DeepSeek Harness (DSH) 插件时，必须严格遵守以下规则：

## 1. 依赖注入与沙箱规范
- 若插件访问 `ctx.tools`、`ctx.fs` 等核心服务，必须在模块导出中显式声明 `inject = ['tools']`，严禁未声明直接访问。
- 必须通过 `ctx.on('dispose', () => { ... })` 安全释放持久资源（WASM 实例、定时器、文件句柄）。

## 2. DSH Bundle 与热挂载规范
- `package.json` 必须包含 `dsh.bundle.patch: "./cordis.patch.yml"`，确保安装后自动激活为 Profile Layer。
- `cordis.patch.yml` 必须写为嵌套的纯 `- insert:` 列表格式，严禁顶层直接写属性，保障热挂载无需重启。

## 3. Agent Tool 定义规范
- `parameters` 必须是标准的 JSON Schema Object（外层包含 `type: 'object'`, `properties: {...}`, `required: [...]`），严禁传扁平字典以防止 Gemini 等模型 400 报错。
- `execute(args)` 内部必须增加自适应参数键名容错（如 `args.image || args.file_path || args.input || args.path`）。

## 4. 依赖隔离与单例保护
- 严禁在插件自身的 dependencies 中硬依赖 `@deepseek-ai/dsh-tools`，防止在 Profile 的 node_modules 造成 Symbol 单例冲突并引发 `Cannot read properties of undefined (reading 'prepare')`。
- 插件自身的运行库（如 `pngjs`, `jpeg-js`）应安装在私有目录或轻量化自包含。

## 5. 纯轻量离线化与分发
- 保持整包轻量化（如 WASM + LWM 架构），在 `package.json` 的 `files` 中声明模型资产目录，确保通过 GitHub 仓库安装即可离线开箱即用。
