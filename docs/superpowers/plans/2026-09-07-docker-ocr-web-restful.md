# Docker OCR Web 界面与 RESTful API 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为轻量离线纯 C/WASM OCR 插件（dsh-lw-PPOCR-C-project）构建容器化运行的 RESTful API 服务与深色玻璃拟态交互式前端页面，实现图片上传识别、文字和四点边框可视化、实时耗时展示。

**Architecture:** 后端采用原生 Node.js HTTP 模块实现零依赖的 RESTful API 服务（提供识别、健康探测与样例接口）；前端采用纯 Vanilla HTML5/CSS3/ES6 打造深色玻璃拟态 SPA，支持拖拽/剪贴板粘贴/图上边框高亮与耗时徽标；通过更新 Dockerfile 和 docker-compose.yml 实现一键容器化启动与测试隔离。

**Tech Stack:** Node.js (v20), lw.PPOCR.C WebAssembly 运行时, HTML5/CSS3 (Glassmorphism, CSS Custom Properties), Canvas API, Docker & Docker Compose.

## Global Constraints

- 不可修改 `docs-files/优化.md`。
- 严禁执行 `docker system prune` 及其变体，严禁执行 `docker image prune -a`。
- 本地调试与测试优先在 Docker 容器中运行。
- Windows PowerShell 下命令串联严禁使用 `&&` 或 `||`，必须使用 `;`。
- 保持整包轻量，避免在后端引入繁重的第三方 Web 框架依赖。

---

### Task 1: 实现 RESTful API 后端服务与自动化集成测试

**Files:**
- Create: `src/server.js`
- Create: `test/test-server.js`
- Modify: `package.json`

**Interfaces:**
- Consumes: `src/engine/lw-ppocr-engine.js` 中的 `LwPpocrEngine` 类与 `recognize(input, options)` 方法。
- Produces:
  - `POST /api/v1/ocr/recognitions` (返回 `{ code: 200, status: "success", data: { text, durationMs, lines, meta } }`)
  - `GET /api/v1/health` (返回 `{ code: 200, status: "success", data: { service, version, engine } }`)
  - `GET /api/v1/sample` (返回 `{ code: 200, status: "success", data: { image, filename } }`)
  - 静态文件服务：托管 `public/` 目录下的静态资源。

- [ ] **Step 1: 编写 RESTful API 服务的自动化测试用例 `test/test-server.js`**

```javascript
const assert = require('assert');
const http = require('http');
const fs = require('fs');
const path = require('path');
const { createServer } = require('../src/server');

const samplePath = path.resolve(__dirname, 'fixtures/sample.png');

function request(options, postData) {
  return new Promise((resolve, reject) => {
    const req = http.request(options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          const json = JSON.parse(data);
          resolve({ status: res.statusCode, headers: res.headers, body: json });
        } catch (e) {
          resolve({ status: res.statusCode, headers: res.headers, raw: data });
        }
      });
    });
    req.on('error', reject);
    if (postData) {
      req.write(typeof postData === 'string' ? postData : JSON.stringify(postData));
    }
    req.end();
  });
}

async function run() {
  console.log('--- Testing RESTful API Server ---');
  const server = await createServer({ port: 0 });
  const port = server.address().port;
  console.log(`Server started on dynamic test port: ${port}`);

  try {
    // 1. Health check
    const health = await request({ hostname: '127.0.0.1', port, path: '/api/v1/health', method: 'GET' });
    assert.strictEqual(health.status, 200);
    assert.strictEqual(health.body.status, 'success');
    assert.strictEqual(health.body.data.engine, 'ready');
    console.log('✔ GET /api/v1/health passed');

    // 2. Sample image
    const sample = await request({ hostname: '127.0.0.1', port, path: '/api/v1/sample', method: 'GET' });
    assert.strictEqual(sample.status, 200);
    assert.strictEqual(sample.body.status, 'success');
    assert(typeof sample.body.data.image === 'string' && sample.body.data.image.startsWith('data:image/png;base64,'));
    console.log('✔ GET /api/v1/sample passed');

    // 3. OCR recognition POST
    const sampleBase64 = fs.readFileSync(samplePath).toString('base64');
    const ocrRes = await request(
      {
        hostname: '127.0.0.1',
        port,
        path: '/api/v1/ocr/recognitions',
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      },
      { image: `data:image/png;base64,${sampleBase64}`, confidenceThreshold: 0.3 }
    );
    assert.strictEqual(ocrRes.status, 200);
    assert.strictEqual(ocrRes.body.status, 'success');
    assert(ocrRes.body.data.durationMs >= 0, 'durationMs should be present');
    assert(Array.isArray(ocrRes.body.data.lines) && ocrRes.body.data.lines.length > 0, 'lines should be detected');
    assert(typeof ocrRes.body.data.text === 'string' && ocrRes.body.data.text.length > 0, 'text should be non-empty');
    console.log(`✔ POST /api/v1/ocr/recognitions passed, recognized in ${ocrRes.body.data.durationMs}ms`);

    // 4. Invalid input handling
    const errRes = await request(
      {
        hostname: '127.0.0.1',
        port,
        path: '/api/v1/ocr/recognitions',
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      },
      {}
    );
    assert.strictEqual(errRes.status, 400);
    assert.strictEqual(errRes.body.status, 'error');
    console.log('✔ Error handling 400 Bad Request passed');

    console.log('All API server tests passed!');
  } finally {
    server.close();
  }
}

run().catch(err => {
  console.error(err);
  process.exit(1);
});
```

- [ ] **Step 2: 运行测试验证它在没有 `src/server.js` 时失败**

Run:
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; node test/test-server.js
```
Expected: FAIL with "Cannot find module '../src/server'"

- [ ] **Step 3: 实现轻量 RESTful API 与静态文件服务 `src/server.js`**

编写 `src/server.js`，包含：
- 请求体流式接收与 JSON 解析；
- RESTful 路由分发器；
- 静态文件托管（MIME 类型映射：`.html`, `.css`, `.js`, `.png`, `.svg` 等）；
- 集成 `LwPpocrEngine` 执行文字识别并返回精准的识别文本与推理耗时；
- 导出 `createServer(options)` 方法并在直接作为主模块运行时启动监听。

- [ ] **Step 4: 运行测试验证测试通过**

Run:
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; node test/test-server.js
```
Expected: PASS with "All API server tests passed!"

- [ ] **Step 5: 更新 `package.json` 中的 `scripts` 添加 `"start": "node src/server.js"`**

- [ ] **Step 6: Git 提交 Task 1 代码**

```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; git add src/server.js test/test-server.js package.json; git commit -m "feat(server): implement restful ocr api service and automated tests"
```

---

### Task 2: 构建高质感深色玻璃拟态前端页面

**Files:**
- Create: `public/index.html`
- Create: `public/style.css`
- Create: `public/app.js`

**Interfaces:**
- Consumes:
  - `POST /api/v1/ocr/recognitions`
  - `GET /api/v1/health`
  - `GET /api/v1/sample`
- Produces:
  - 极具视觉冲击力的现代 Web 界面（深色玻璃拟态主题、动态渐变光晕、拖拽/剪贴板粘贴、图像 Canvas 边框交互标注、推理耗时大徽标、一键复制）。

- [ ] **Step 1: 编写语义化 HTML 页面 `public/index.html`**

包含：
- 顶部导航栏：项目 Logo、版本徽标、健康状态指示灯；
- 主要操作工作区（双栏弹性响应式网格）：
  - 左侧：上传卡片（支持点击/拖拽/粘贴/示例快速导入）、原图预览容器与标注 Canvas 叠加层、重置与识别按钮；
  - 右侧：结果展示卡片，顶部展示核心指标徽标（识别耗时 ms、识别行数、置信度），包含“全文模式”（带复制按钮）与“分行明细模式”。
- 底部页脚与状态通知 Toast 浮层。

- [ ] **Step 2: 编写专业深色玻璃拟态样式表 `public/style.css`**

包含：
- CSS 自定义属性（Tokens）：精调深色背景、半透明磨砂面板、霓虹渐变重点色（Violet/Indigo/Cyan）；
- 平滑滚动、微动效（按钮 Hover 悬浮、脉冲光环、骨架加载动画）；
- 响应式媒体查询（移动端与桌面端完美自适应）；
- Canvas 标注层与浮动 Tooltip 的像素级精准定位。

- [ ] **Step 3: 编写前端交互脚本 `public/app.js`**

包含：
- 图片文件读取（`FileReader` 读取为 Data URL）；
- 拖拽（Drag & Drop）与全局剪贴板粘贴（`paste` 事件监听图片数据）；
- 加载样例图片事件绑定；
- 调用 `POST /api/v1/ocr/recognitions`，并精确计算与呈现耗时（显示后端引擎耗时 `durationMs` 与网络往返耗时）；
- 图像与标注 Canvas 坐标映射：绘制检测框（Bounding Box），支持鼠标移上框线时在右侧列表联动高亮并显示单行悬浮 Tooltip；
- 全文与明细视图切换、一键复制文本并给予即时 Toast 反馈。

- [ ] **Step 4: 本地静态资源健全性校验**

启动并请求静态资源，确保 HTML、CSS、JS 正确加载：
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; node -e "const fs = require('fs'); ['public/index.html', 'public/style.css', 'public/app.js'].forEach(f => console.log(f, fs.existsSync(f) ? 'OK' : 'MISSING'));"
```

- [ ] **Step 5: Git 提交 Task 2 代码**

```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; git add public/; git commit -m "feat(web): add modern dark glassmorphism ocr frontend interface"
```

---

### Task 3: 容器化配置升级 (Dockerfile 与 docker-compose.yml)

**Files:**
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: Node 20 容器镜像、`src/server.js`、`public/` 静态目录、`test/test-plugin.js`。
- Produces:
  - 容器服务 `web-service` 监听并映射主机 `3000:3000`。
  - 容器服务 `test-service` 独立执行单元测试。

- [ ] **Step 1: 升级 Dockerfile**

更新 `Dockerfile`：
- 清理不存在的 `scripts/build.js` 与 `dist/` 复制指令；
- 暴露 `EXPOSE 3000`；
- 设置环境变量 `PORT=3000`；
- 容器启动默认指令设为 `CMD ["node", "src/server.js"]`。

- [ ] **Step 2: 升级 docker-compose.yml**

更新 `docker-compose.yml`：
- 配置 `web-service`：构建镜像、映射 `3000:3000` 端口、挂载代码卷、配置健康检查；
- 保留 `test-service`：通过 `command: node test/test-plugin.js` 随时执行容器内测试。

- [ ] **Step 3: Git 提交 Task 3 代码**

```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; git add Dockerfile docker-compose.yml; git commit -m "feat(docker): update Dockerfile and docker-compose for web service on port 3000"
```

---

### Task 4: Docker 容器端到端验证与文档更新

**Files:**
- Modify: `readme.md`

- [ ] **Step 1: 在 Docker 容器中执行完整测试验证**

Run:
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; docker compose run --rm test-service
```
Expected: 所有的 6 个离线 OCR 测试顺利通过。

- [ ] **Step 2: 构建并启动 Web 容器服务，执行健康检查与 API 调用**

Run:
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; docker compose up -d web-service
```
验证容器健康检查与 RESTful API 调用：
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; curl.exe -s http://localhost:3000/api/v1/health
```
验证前端页面服务是否正常响应：
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; curl.exe -s -I http://localhost:3000/
```
关闭正在运行的容器测试实例：
```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; docker compose down
```

- [ ] **Step 3: 更新 `readme.md`**

在 `readme.md` 中：
- 补充 Docker Web 前端界面的使用说明与截图展示；
- 补充 RESTful API 接口规范与调用示例（包含健康检查、POST 识别与字段说明）；
- 补充容器化一键部署指令 `docker compose up -d web-service`。

- [ ] **Step 4: Git 提交 Task 4 代码与文档**

```powershell
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; git add readme.md; git commit -m "docs: update readme with docker web ui and restful api documentation"
```
