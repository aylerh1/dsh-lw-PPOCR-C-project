# 项目经验总结

## 一、项目经验总结

### （1）项目介绍
本项目基于 C11 与 WebAssembly 纯轻量级推理架构，为 DeepSeek Harness (dsh) 打造免安装庞大依赖的离线 OCR 插件。数据流向为：用户或 Agent 传入本地图片路径、Buffer 或 Base64 编码流，经过纯 JS 图像解码器（pngjs / jpeg-js）转换为 BGR8 像素流并完成尺寸校验，直接输入 lw.PPOCR.C 官方纯 WASM 推理运行时完成文本区域检测（DET）、文字方向分类（CLS）与字符识别（REC），最终输出包含结构化文本内容、单行置信度与四点几何包围盒的标准化数据，彻底解决传统 OCR 方案在智能体框架中依赖过重、环境冲突与无法真正离线运行的问题。

### （2）总体技术架构
技术栈涵盖：C11, WebAssembly (WASM), DeepSeek Harness (DSH), Cordis IoC Plugin Framework, Node.js (CommonJS / TypeScript / JSON Schema), Docker, Docker Compose。底层通过官方编译的纯 C/WASM 模块与轻量 LWM 模型资产实现零环境依赖的高性能离线推理内核；中间层借助 Cordis 微内核架构实现服务自动注入与标准化 OpenAPI / JSON Schema Agent Tool 注册；顶层提供具备 TypeScript 严格类型定义的双模块分发接口，并通过全套自动化测试套件与 Docker 容器化方案保障跨平台运行的一致性与高可靠性。

### （3）总体功能架构
总体功能划分为四大模块：（1）图像预处理引擎：支持本地文件绝对路径、Base64、Data URI 与原始 Buffer 输入，内置纯 JS 的 PNG/JPEG/BMP 解码器与 BGR 像素转换器；（2）全流程离线 OCR 推理流水线：集成官方 PP-OCRv6 tiny 资产（det.lwm, cls.lwm, rec.lwm, ppocr_keys.txt），实现真实的端到端毫秒级文本检测、方向校正与识别；（3）排版与几何后处理引擎：提供 horizontal-ltr、vertical-rtl 等多种阅读顺序几何排序与四点多边形坐标转换；（4）智能体生态挂载模块：遵循 Cordis IoC 插件生命周期，向 Context 挂载 ctx.ocr 核心服务，并注册符合 Gemini/OpenAI 规范的 ocr_recognize 智能体工具。

### （4）项目优点
1. 真实离线与零重型环境依赖：内置约 7MB 纯 WASM 运行时与精简 LWM 模型，完全摆脱 Python、OpenCV、ONNX Runtime 等沉重环境，具备百毫秒级冷启动与超低内存开销。
2. 极简分发与开箱即用：完整集成模型与运行时资产，用户无需安装本地 C/C++ 编译工具链，直接通过 GitHub 仓库链接即可一键依赖安装并投入生产。
3. 智能体深度兼容与高容错：Agent Tool 严格遵循标准 JSON Schema Object 规范，杜绝 Gemini/Claude 等模型调用时的 400 校验异常，并内置多候选参数自适应提取逻辑。

### （5）项目缺点
受限于端侧纯轻量化模型与离线运行设计，针对极端超大倾斜角度图像或极度模糊低分辨率复杂背景图像的识别召回率略低于云端百亿参数多模态视觉大模型，需在后续版本中通过自适应对比度增强与图像超分辨率算法持续优化。

## 二、项目经验的简历式总结：

### 项目名称
dsh-lw-PPOCR-C-project（基于 lw.PPOCR.C 的 DeepSeek Harness 轻量离线 OCR 插件）

### 担任角色
核心开发者，负责总体架构设计、WASM 运行时集成与智能体工具链适配

### 时间段
2025年6月 - 2025年7月

### 项目描述
本项目是针对多模态智能体框架 DeepSeek Harness 设计的高性能、零重型依赖离线 OCR 插件。针对传统 OCR 框架强依赖 Python、OpenCV 与 ONNX Runtime 导致 Agent 启动缓慢与环境冲突问题，引入 lw.PPOCR.C 纯 C/WASM 离线推理架构与纯 JS 图像解码机制；针对插件在智能体微内核中的挂载与大模型 Function Calling 报错问题，基于 Cordis IoC 框架实现标准 JSON Schema Object 工具注册与多候选参数容错解析；针对跨环境免编译分发需求，实现模型资产内置打包，支持通过 GitHub 仓库直接依赖引用；针对测试稳定性，构建了完整的离线测试用例与容器化验证流程。

### 项目业绩
采用 WebAssembly 与纯 C 架构彻底消除了对 Python 和 ONNX Runtime 的重型依赖，插件整体体积仅约 7MB 且实现 100~150ms 级真实端到端推理；通过规范化 OpenAPI / JSON Schema 工具定义彻底解决了 Gemini 模型 400 参数校验异常与 DSH 调度器单例冲突问题；采用全资产内嵌的分发方案实现了无需 npm 发布即可直接基于 GitHub 链接开箱即用安装，大幅降低了用户的集成与维护成本；编写了覆盖真实图片识别、多阅读顺序与异常处理的全套测试套件，并通过 Docker 容器化技术保障了跨操作系统的一致性交付。

## 三、项目经验的一句话总结

基于lw.PPOCR.C与Cordis微内核的轻量离线OCR插件：以纯WASM推理与标准JSON Schema工具为核心，实现零Python依赖的百毫秒级离线文本识别，解决智能体框架中传统OCR环境臃肿与大模型调用冲突问题。
