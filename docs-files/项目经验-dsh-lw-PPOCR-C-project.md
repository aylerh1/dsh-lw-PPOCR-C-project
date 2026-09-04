# 项目经验总结

## 一、项目经验总结

### （1）项目介绍
本项目基于 C11 与 WebAssembly 纯轻量级推理架构，为 DeepSeek Harness (dsh) 打造免安装庞大依赖的离线 OCR 插件。数据流向为：用户或 Agent 传入本地图片、Buffer 或 Base64 编码流，经过图像格式解析与尺寸归一化，进入 lw.PPOCR.C 轻量推理流水线完成文本检测（DET）、方向分类（CLS）与文字识别（REC），最终输出包含结构化文本内容、单行置信度与几何包围盒的标准化 JSON 数据，解决传统 OCR 方案在智能体框架中依赖过重与部署困难问题。

### （2）总体技术架构
技术栈涵盖：C11, WebAssembly (WASM), DeepSeek Harness (DSH), Cordis IoC Plugin Framework, Node.js (ESM / CommonJS / TypeScript), Docker, Docker Compose。底层通过 C11/WASM 实现高性能无依赖推理内核；中间层借助 Cordis 微内核架构实现服务自动注入与 Agent Tool 注册；顶层提供符合 TypeScript 标准的双模块规范分发，并通过 Docker 实现全链路容器化构建与测试验证。

### （3）总体功能架构
总体功能划分为四大模块：（1）输入预处理引擎：负责解析文件路径、Base64、Data URI 与二进制 Buffer；（2）纯轻量 OCR 推理流水线：支持灵活开启或关闭 DET 检测、CLS 分类与 REC 识别阶段；（3）排版与后处理引擎：提供 horizontal-ltr、vertical-rtl 等多种阅读顺序几何排序；（4）智能体生态挂载模块：提供 Cordis 插件生命周期绑定、上下文服务暴露与 DSH Agent Tool 自动注册。

### （4）项目优点
1. 零重型环境依赖：完全脱离 Python、OpenCV、ONNX Runtime 等体积庞大的依赖包，具备秒级冷启动与超低内存开销。
2. 极简分发与安装：支持通过 GitHub 仓库直接依赖引用，开箱即用，无需发布到 npm 仓库。
3. 架构高度解耦：与 DeepSeek Harness 的 Cordis 内核深度契合，以微内核插件形式运行，稳定性极佳。

### （5）项目缺点
受限于纯轻量模型与离线运行设计，针对极端超大倾斜角图像或极度模糊低分辨率复杂背景图像的识别精度略低于云端百亿参数多模态大模型，需在后续版本中通过自适应图像增强算法持续优化。

## 二、项目经验的简历式总结：

### 项目名称
dsh-lw-PPOCR-C-project（基于 lw.PPOCR.C 的 DeepSeek Harness 轻量 OCR 插件）

### 担任角色
核心开发者，负责总体架构设计和核心算法优化

### 时间段
2025年6月 - 2025年7月

### 项目描述
本项目是针对多模态智能体框架 DeepSeek Harness 设计的高性能、零重型依赖离线 OCR 插件。针对传统 AI 框架环境臃肿导致 Agent 启动缓慢问题，采用 WebAssembly 与 C11 轻量运行时，消除 Python 与 OpenCV 依赖；针对插件在智能体微内核中的挂载难题，基于 Cordis IoC Framework 实现服务注入与 Agent Tool 自动注册；针对跨模块调用与开箱即用需求，基于 Node.js 与 TypeScript 实现了多模块规范打包，确保用户直接通过 GitHub 仓库安装即可无缝运行；针对环境测试一致性，构建 Docker 容器化自动化验证流程。

### 项目业绩
采用 WebAssembly 与 C11 架构彻底消除了对 Python 和 ONNX Runtime 的依赖，显著缩减部署体积并避免动态库环境冲突；采用 Cordis 插件机制实现了 Agent 运行时的免配置无缝挂载与自动化生命周期管理；通过预打包分发方案实现了无需 npm 发布即可直接基于 GitHub 链接依赖安装，大幅降低了开发者与社区用户的集成维护成本；借助 Docker 容器化技术保障了跨操作系统环境一致的构建验证与高可靠交付。

## 三、项目经验的一句话总结

基于lw.PPOCR.C和Cordis的轻量OCR插件：以图片格式为中心，实现离线文本检测与识别，解决智能体框架中传统OCR依赖臃肿与环境冲突问题。
