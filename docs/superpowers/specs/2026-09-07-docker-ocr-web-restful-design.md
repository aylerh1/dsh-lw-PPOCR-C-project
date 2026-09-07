# Docker OCR Web 界面与 RESTful API 架构设计规范

- **创建日期**：2026-09-07
- **项目名称**：dsh-lw-PPOCR-C-project
- **状态**：已评审 (Approved)

---

## 1. 目标与范围 (Goal & Scope)

为轻量级 C/WASM 离线 OCR 插件（`dsh-lw-PPOCR-C-project`）提供开箱即用的 Docker 容器化 Web 前端界面与符合 RESTful 标准的 API 服务，满足：
1. **用户直观体验**：在浏览器中直接通过拖拽、点击、剪贴板粘贴图片进行 OCR 文字提取。
2. **文字与耗时展示**：在界面直观、醒目地显示推理耗时（ms）、识别全文、四点框线高亮与单行明细。
3. **标准 RESTful API**：提供符合 RESTful 规范的接口供外部系统与前端调用。
4. **轻量与容器化**：基于原生 Node.js HTTP 与微服务架构，保持 0 沉重外部依赖的优势；提供一键启动的 Docker Compose 支持。

---

## 2. 系统架构与文件布局

```
dsh-lw-PPOCR-C-project/
├── src/
│   ├── engine/             # 纯 WASM 离线 OCR 推理引擎 (保持不变)
│   ├── server.js           # [NEW] 轻量 RESTful API 与静态文件服务
│   ├── plugin.js           # 现有的 DSH/Cordis 插件导出 (保持不变)
│   └── index.js            # 入口文件 (保持不变)
├── public/                 # [NEW] 前端静态资源
│   ├── index.html          # 单页面现代化 HTML
│   ├── style.css           # 深色玻璃拟态 (Dark Glassmorphism) 样式
│   └── app.js              # 前端交互与 Canvas 框线绘制逻辑
├── Dockerfile              # [UPDATE] 容器镜像定义，配置 Node 20 运行时与端口暴露
├── docker-compose.yml      # [UPDATE] 配置 web-service (3000) 与 test-service
└── package.json            # [UPDATE] 添加 "start": "node src/server.js" 指令
```

---

## 3. RESTful API 规范

### 3.1 接口列表

#### 1. 创建并执行识别任务
- **方法与路径**：`POST /api/v1/ocr/recognitions`
- **请求头**：`Content-Type: application/json`
- **请求体**：
  ```json
  {
    "image": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA...",
    "confidenceThreshold": 0.3,
    "readingOrder": "horizontal-ltr"
  }
  ```
- **成功响应 (200 OK / 201 Created)**：
  ```json
  {
    "code": 200,
    "status": "success",
    "data": {
      "text": "提取的识别全文\n第二行内容",
      "durationMs": 115,
      "lines": [
        {
          "text": "提取的识别全文",
          "score": 0.982,
          "box": [[10, 12], [180, 12], [180, 42], [10, 42]]
        }
      ],
      "meta": {
        "width": 600,
        "height": 400,
        "readingOrder": "horizontal-ltr"
      }
    },
    "timestamp": 1757235200000
  }
  ```
- **错误响应 (400 Bad Request / 500 Internal Server Error)**：
  ```json
  {
    "code": 400,
    "status": "error",
    "message": "请提供有效的图片数据 (Base64 或 Data URI)",
    "timestamp": 1757235200000
  }
  ```

#### 2. 健康检查探针
- **方法与路径**：`GET /api/v1/health`
- **成功响应 (200 OK)**：
  ```json
  {
    "code": 200,
    "status": "success",
    "data": {
      "service": "dsh-lw-ppocr-web",
      "version": "1.0.0",
      "engine": "ready",
      "uptime": 128.4
    },
    "timestamp": 1757235200000
  }
  ```

#### 3. 获取内置样例图片
- **方法与路径**：`GET /api/v1/sample`
- **成功响应 (200 OK)**：
  ```json
  {
    "code": 200,
    "status": "success",
    "data": {
      "image": "data:image/png;base64,...",
      "filename": "sample.png"
    },
    "timestamp": 1757235200000
  }
  ```

---

## 4. 前端界面设计 (UI/UX)

- **视觉风格**：
  - 核心基调：科技深色主题（`#0b0f19` 背景）+ 蓝紫渐变霓虹强调色（`#6366f1` / `#8b5cf6`）。
  - 组件质感：磨砂玻璃（`backdrop-filter: blur(16px)`）、半透明边框与柔和阴影。
  - 动效与交互：平滑过渡动画（Hover 升起、渐入加载动画、脉冲呼吸光晕）。
- **交互功能**：
  - **上传区域**：拖拽上传、点击本地选择、剪贴板全局 `Ctrl+V` 截屏直接粘贴；支持一键载入样例图片。
  - **图片预览与标注层**：使用自适应缩放的 Canvas 或 SVG 遮罩层在原图上绘制检测到的文字边框；鼠标悬浮在边框时高亮显示对应行文本及置信度 Tooltip。
  - **指标统计看板**：顶部大字展示 **推理耗时 (ms)** 徽标、识别行数、置信度等关键指标。
  - **结果查看器**：
    - 「全文模式」：排版文本、字数统计、带复制反馈的一键复制按钮。
    - 「明细列表」：单行卡片，包含行号、识别文本、置信度进度条及四点坐标详情。

---

## 5. Docker 容器集成

- **Dockerfile 调整**：
  - 基础镜像：`node:20-alpine`。
  - 清理不存在的 `scripts/build.js` 与 `dist/`，复制必须代码文件并安装依赖。
  - 暴露端口 `3000`。
  - 默认启动指令 `CMD ["node", "src/server.js"]`。
- **docker-compose.yml**：
  - `web-service`：映射端口 `3000:3000`，挂载当前目录便于热重载调试。
  - `test-service`：执行单元测试 `node test/test-plugin.js`。
