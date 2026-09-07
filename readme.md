# 概览

`dsh-lw-PPOCR-C-project` 是基于轻量纯 C 语言 PP-OCR 推理运行时 [lw.PPOCR.C](https://github.com/lxw112190/lw.PPOCR.C) 构建的 **DeepSeek Harness (dsh)** 轻量级离线 OCR 插件。

项目彻底摆脱了传统 OCR 方案中对 Python、OpenCV、ONNX Runtime 等动辄数 GB 沉重依赖包的束缚，通过纯 C 底层架构与 WebAssembly 跨平台运行机制，为 DeepSeek Harness 智能体生态提供极速、零外部依赖、开箱即用的离线文本检测（DET）、方向分类（CLS）与字符识别（REC）全链路能力。

## 页面样式

### 主页

![1](./images/1.png)

# 构建与运行

### 1. 直接通过 GitHub 安装（无需发布到 npm）

本项目已针对 GitHub 直接引用进行了预编译构建与全资产打包，用户无需安装本地编译工具链即可直接引入：

```bash

# 通过 npm 直接从 GitHub 仓库安装
npm install github:aylerh1/dsh-lw-PPOCR-C-project

# 或使用 DeepSeek Harness CLI 直接挂载
dsh plugin --profile web add https://github.com/aylerh1/dsh-lw-PPOCR-C-project
```
更新：
```
# 方式一：直接更新当前插件（推荐，DSH 将自动将其激活为 Profile Layer）
dsh plugin --profile web update dsh-lw-PPOCR-C-project

# 或方式二：重新执行 add
dsh plugin --profile web add https://github.com/aylerh1/dsh-lw-PPOCR-C-project
```
### 2. DeepSeek Harness (Cordis) 配置挂载

在 Harness 的 `cordis.patch.yml` 中添加插件配置项：

```yaml
- id: dsh-lw-ppocr
  plugin: dsh-lw-PPOCR-C-project
  description: "基于 lw.PPOCR.C 的轻量级离线 OCR 插件"
  config:
    enabled: true
    det: true
    cls: true
    rec: true
    readingOrder: "horizontal-ltr"
    confidenceThreshold: 0.5
```

### 3. 代码调用与 Agent 工具使用

#### (1) 在 Harness 服务上下文中调用
```javascript
const { apply } = require('dsh-lw-PPOCR-C-project');

// 挂载插件后，直接使用 ctx.ocr 提供的标准化服务
const result = await ctx.ocr.recognize('path/to/receipt.png', {
  det: true,
  cls: true,
  rec: true,
  readingOrder: 'horizontal-ltr'
});

console.log('识别全文:', result.text);
console.log('详细文本行与包围盒:', result.lines);
```

#### (2) 作为 Agent Tool 在大模型提示词中自动调用
插件已自动向 DSH 智能体注册 `ocr_recognize` 工具，大模型在分析用户上传的票据、文档或屏幕截图时，可直接触发调用。

### 4. 本地运行与测试验证

本项目零外部沉重依赖，开箱即用，可在本地直接执行全套自动化测试验证：

```bash
# 本地快速测试验证
npm test
# 或直接使用 Node.js 运行
node test/test-plugin.js
```

同时也保留了 Dockerfile 与 docker-compose.yml 供容器化部署使用：
```bash
docker compose run --rm test-service
```

# 解决痛点与使用技术

### 解决痛点

1. **摆脱庞大环境依赖与安装壁垒**：传统 PP-OCR 方案强绑定 Python 环境、PyTorch/PaddlePaddle 或 ONNX Runtime 动态库，部署包极大且易发生版本冲突。本项目基于纯 C/WASM 运行时，实现体积小、零重量级三方依赖。
2. **免除发布 npm 的分发成本与维护复杂度**：支持直接通过 GitHub 安装，分发构建产物完备，极大降低企业内部及社区二次集成的门槛。
3. **Agent 插件环境隔离与高效交互**：在 DeepSeek Harness 的 Cordis 微内核架构中，无缝以独立服务与 Agent Tool 形式注册，彻底消除动态链接库在不同操作系统间的加载失败与进程崩溃隐患。
4. **灵活的多排版支持与高容错性**：支持横排从左到右、竖排从右到左等多种阅读顺序自动重组，适配古籍、发票、表格等多场景。

### 使用技术

- **C11 / WebAssembly (WASM)**：承接 `lw.PPOCR.C` 的核心架构理念，提供零依赖跨平台高性能离线推理。
- **DeepSeek Harness (DSH)**：新一代以插件为核心的智能体 Harness 运行时。
- **Cordis 插件内核**：基于微内核依赖注入（IoC）机制，实现服务解耦与生命周期控制。
- **Node.js (CommonJS / ESM / TypeScript)**：提供双模块分发格式与严格的 TypeScript 类型推导接口。
- **Docker & Docker Compose**：实现完全隔离、可复现的容器化编译构建与自动化测试。

# 致谢

- [lw.PPOCR.C](https://github.com/lxw112190/lw.PPOCR.C)：纯C轻量PP-OCR推理运行时。
- [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)：基于Cordis的AI智能体框架。
- [Cordis](https://cordis.moe/)：模块化微内核插件架构。