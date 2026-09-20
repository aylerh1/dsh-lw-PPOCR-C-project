# 基于原生 C/AVX2 编译加速与 Go CGO 架构的高性能 OCR 提速设计规范

- **创建日期**：2026-09-20
- **项目名称**：dsh-lw-PPOCR-C-project
- **状态**：已评审 (Approved)

---

## 1. 目标与背景 (Goal & Background)

### 1.1 现状与瓶颈分析
在前期版本（g1.4）中，为实现容器常驻内存控制在 10MB 左右的目标，系统采用了由 Go 原生 HTTP 服务在每次请求时通过 `exec.Command` 动态拉起独立 Node CLI 进程执行 WASM 推理的方案。虽然空闲内存降低了，但引入了严重的性能损耗：
1. **进程冷启动与重复加载开销**：每次请求都需启动 Node.js V8 虚拟机、从磁盘读取 7.2MB 的 LWM 模型文件并进行 WASM 内存映射和 C 运行时初始化（单次耗时约 250ms ~ 400ms）。
2. **纯 JavaScript 软解码开销**：使用纯 JS 实现的 `pngjs` / `jpeg-js` 解码全幅页面图像，单核计算缓慢（单次耗时约 200ms ~ 500ms）。
3. **单线程模拟与串行识别**：WASM 环境下单线程串行处理 DET 文本检测与每一行 REC 字符识别，在 20~40 行的常规文档页面上，总耗时高达 **1.x 秒每页**。

### 1.2 优化目标
1. **极致提速（提速 10 倍以上）**：整页端到端推理耗时由 1.x 秒降至 **50ms ~ 120ms** 级别，吞吐量提升至 50+ 页/秒。
2. **超低内存（保持 < 30MB）**：利用原生 C 编译内存紧凑特性，常驻内存维持在 **15MB ~ 30MB**，远低于传统 AI 框架的数百兆开销。
3. **容器瘦身（降低 80% 体积）**：彻底剔除 Docker 容器中的 Node.js 运行时与 npm 依赖，镜像体积从 **155MB 降至约 25MB**。
4. **无缝兼容**：保持现有 RESTful API 格式（`/api/v1/ocr/recognitions`）、Web 深色拟态前端界面与 DSH 插件规范 100% 兼容。

---

## 2. 总体架构与数据流 (Architecture & Data Flow)

```
+-----------------------------------------------------------------------------------+
|                        Docker 容器 (轻量 Alpine/Linux, ~25MB)                      |
|                                                                                   |
|   1. 外部请求 / 浏览器 Web UI                                                       |
|         │                                                                         |
|         ▼ (POST /api/v1/ocr/recognitions, 包含 Base64 / Data URI / 路径)            |
|   2. Go Native HTTP 服务 (cmd/server)                                             |
|         │                                                                         |
|         ├─► [Go 原生高速图像解码器] (image/png, image/jpeg)                        |
|         │   原生编译代码 5~15ms 快速完成解压并转换为连续 Interleaved BGR8 像素切片  |
|         │                                                                         |
|         ▼ (Zero-Copy 内存指针直接传递 unsafe.Pointer(&bgr[0]))                    |
|   3. liblw_ppocr_c 原生引擎 (C11, -O3, AVX2, FMA, POSIX Threads)                  |
|         ├─ DET: DBNet 文本检测 (AVX2 向量加速卷积, ~5ms)                           |
|         ├─ CLS: 文字方向纠偏分类 (可选开关, ~2ms)                                   |
|         └─ REC: 多 Worker 线程池并行字符识别 (AVX2+多核, ~15ms)                     |
|         │                                                                         |
|         ▼ (结构化提取文本、置信度、四点几何坐标)                                     |
|   4. 返回标准化 RESTful JSON 响应并触发前端 Canvas 框线联动                         |
+-----------------------------------------------------------------------------------+
```

---

## 3. 详细设计与关键模块

### 3.1 C 源码集成与编译配置 (`vendor/lw-ppocr-c`)
集成开源纯 C 运行时 `lw.PPOCR.C` 的核心源码目录：
- `include/lw_infer.h`：原生 C ABI 接口头文件；
- `src/kernels/`：纯 C 算子（卷积、矩阵乘、激活函数、Softmax）；
- `src/simd/`：AVX2 / SSE2 / FMA 高性能向量加速核；
- `src/ppocr/`：检测、分类、识别、排版及后处理逻辑；
- `src/runtime/`：会话管理、执行图计划及多线程并行调度池；

**构建配置**：
- 采用 C11 标准编译；
- 开启编译优化参数：`-O3 -mavx2 -mfma -pthread`；
- 打包为静态归档库 `liblw_ppocr_c_static.a`，供 Go 编译期静态链接。

### 3.2 Go 原生高速图像解码 (`cmd/server/image.go`)
- 废弃 Node 纯 JS 软解码，改用 Go 标准库 `image/png`、`image/jpeg` 以及轻量 BMP 解码；
- 将 RGBA/NRGBA/YCbCr 快速转换为连续内存块 `interleaved BGR8`（符合 `lw_infer.h` 的输入契约）；
- 解码耗时仅需 5ms ~ 15ms，极大缩短前处理延迟。

### 3.3 Go CGO 绑定与引擎管理 (`cmd/server/ocr_engine.go`)
- **生命周期**：在 Go 服务启动阶段调用 `lw_ocr_create` 一次性完成模型加载与初始化，避免单次请求重复读盘与模型解析；
- **配置项自适应**：
  - `rec_max_width`：默认 960，支持动态自适应缩放；
  - `use_classifier`：可配置开关（默认启用，对于正向扫描件可关闭以进一步提速）；
  - `ocr_workers`：底层多线程 Worker 数量（默认自适应 CPU 核心数）；
- **并发与安全**：
  - 利用 `sync.Mutex` 或多句柄池保障多请求安全执行；
  - 直接在 Go 切片与 C 指针之间进行数据传递，零额外序列化开销。

### 3.4 接口兼容与服务入口 (`cmd/server/main.go`)
- 保持 RESTful 路由：
  - `POST /api/v1/ocr/recognitions`：支持 Base64 / Data URI / 文件路径输入，返回行文本、四点框坐标、置信度与 `durationMs`；
  - `GET /api/v1/health`：健康检查接口，返回 native-avx2 引擎状态与运行指标；
  - `GET /api/v1/sample`：样例图片返回；
  - `GET /*`：静态资源映射（托管 `public/` 目录）。

### 3.5 多阶段 Dockerfile 优化 (`Dockerfile`)
- **Stage 1 (Builder)**：
  - 基础镜像：`golang:1.24-alpine` + `build-base cmake git`；
  - 编译 `liblw_ppocr_c_static.a`；
  - 静态编译 Go HTTP 服务二进制 `/build/server`，嵌入 CGO 静态库。
- **Stage 2 (Runtime)**：
  - 基础镜像：`alpine:3.21`（极简运行环境，仅约 7MB）；
  - 拷贝 `/build/server`、模型文件 `models/` 及静态网页 `public/`；
  - 容器镜像大小从 155MB 暴降至 **~25MB**。

---

## 4. 容错与异常处理

1. **输入格式校验**：若传入非合法图片格式或格式不支持，Go 解码层直接返回 `400 Bad Request`，无需下发至 C 引擎。
2. **模型加载校验**：启动时检查 `det.lwm`、`cls.lwm`、`rec.lwm` 及字典文件完整性；若文件缺失或校验失败，输出明确日志并安全退出。
3. **CGO 内存管理**：推理输出缓冲区生命周期由 Go 控制并复用，防止内存泄漏。

---

## 5. 验证与基准测试计划

1. **功能验证**：
   - 运行针对 `sample.png` 的识别测试，校验文本行、四点几何包围盒与置信度完全匹配；
   - 验证 Web 界面画框标注、鼠标悬停联动与文字复制功能正常。
2. **性能压测**：
   - 在 Docker 容器中执行 10 轮压测，对比端到端耗时与单模块耗时；
   - 验证端到端耗时控制在 **100ms 左右**（较当前 1.x 秒提速 10 倍以上）；
   - 验证容器在压测期间内存占用控制在 **30MB 左右**。
