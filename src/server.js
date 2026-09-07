/**
 * Lightweight RESTful API & Static Web Server for dsh-lw-PPOCR-C-project
 * Zero external web framework dependencies - pure Node.js runtime
 */

const http = require('http');
const fs = require('fs');
const path = require('path');
const { LwPpocrEngine, LwPpocrError } = require('./engine/lw-ppocr-engine');

const MIME_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.wasm': 'application/wasm'
};

const PUBLIC_DIR = path.resolve(__dirname, '../public');
const SAMPLE_PATH = path.resolve(__dirname, '../test/fixtures/sample.png');

/**
 * Send JSON response helper
 */
function sendJson(res, statusCode, data) {
  const payload = JSON.stringify(data);
  res.writeHead(statusCode, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type, Authorization'
  });
  res.end(payload);
}

/**
 * Parse incoming JSON request body
 */
function parseJsonBody(req, maxBytes = 50 * 1024 * 1024) {
  return new Promise((resolve, reject) => {
    let raw = '';
    let bytesReceived = 0;

    req.on('data', chunk => {
      bytesReceived += chunk.length;
      if (bytesReceived > maxBytes) {
        reject(new Error('Payload Too Large: 请求体超过最大限制 (50MB)'));
        req.destroy();
        return;
      }
      raw += chunk;
    });

    req.on('end', () => {
      if (!raw.trim()) {
        resolve({});
        return;
      }
      try {
        const parsed = JSON.parse(raw);
        resolve(parsed);
      } catch (err) {
        reject(new Error(`Invalid JSON: 请求体不是合法的 JSON 格式 (${err.message})`));
      }
    });

    req.on('error', reject);
  });
}

/**
 * Serve static files from public/ directory
 */
function serveStatic(req, res, pathname) {
  let relativePath = pathname === '/' ? '/index.html' : pathname;
  // Normalize and prevent path traversal
  const safePath = path.normalize(relativePath).replace(/^(\.\.[\/\\])+/, '');
  const filePath = path.join(PUBLIC_DIR, safePath);

  // Check if file is inside public directory
  if (!filePath.startsWith(PUBLIC_DIR)) {
    sendJson(res, 403, { code: 403, status: 'error', message: 'Forbidden' });
    return;
  }

  fs.stat(filePath, (err, stats) => {
    if (err || !stats.isFile()) {
      sendJson(res, 404, { code: 404, status: 'error', message: 'Not Found' });
      return;
    }

    const ext = path.extname(filePath).toLowerCase();
    const contentType = MIME_TYPES[ext] || 'application/octet-stream';

    res.writeHead(200, {
      'Content-Type': contentType,
      'Content-Length': stats.size,
      'Cache-Control': ext === '.html' ? 'no-cache' : 'public, max-age=86400'
    });

    fs.createReadStream(filePath).pipe(res);
  });
}

/**
 * Create and start the OCR HTTP Server
 * @param {Object} options
 * @param {number} [options.port]
 * @param {string} [options.host]
 * @param {LwPpocrEngine} [options.engine]
 * @returns {Promise<http.Server>}
 */
async function createServer(options = {}) {
  const port = options.port !== undefined ? options.port : (parseInt(process.env.PORT, 10) || 3000);
  const host = options.host || process.env.HOST || '0.0.0.0';

  const engine = options.engine || new LwPpocrEngine({
    confidenceThreshold: 0.3
  });

  if (!engine.initialized) {
    await engine.init();
  }

  const server = http.createServer(async (req, res) => {
    // 1. Handle CORS Preflight
    if (req.method === 'OPTIONS') {
      res.writeHead(204, {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
        'Access-Control-Allow-Headers': 'Content-Type, Authorization',
        'Access-Control-Max-Age': '86400'
      });
      res.end();
      return;
    }

    const urlObj = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
    const pathname = urlObj.pathname;

    try {
      // 2. Health check endpoint
      if (req.method === 'GET' && pathname === '/api/v1/health') {
        sendJson(res, 200, {
          code: 200,
          status: 'success',
          data: {
            service: 'dsh-lw-ppocr-web',
            version: '1.0.0',
            engine: engine.initialized ? 'ready' : 'not_ready',
            uptime: process.uptime()
          },
          timestamp: Date.now()
        });
        return;
      }

      // 3. Built-in sample image endpoint
      if (req.method === 'GET' && pathname === '/api/v1/sample') {
        if (!fs.existsSync(SAMPLE_PATH)) {
          sendJson(res, 404, {
            code: 404,
            status: 'error',
            message: '样例测试图片未找到'
          });
          return;
        }
        const sampleBuf = fs.readFileSync(SAMPLE_PATH);
        sendJson(res, 200, {
          code: 200,
          status: 'success',
          data: {
            filename: 'sample.png',
            mimeType: 'image/png',
            image: `data:image/png;base64,${sampleBuf.toString('base64')}`
          },
          timestamp: Date.now()
        });
        return;
      }

      // 4. OCR Recognition resource creation endpoint (POST /api/v1/ocr/recognitions)
      if (req.method === 'POST' && (pathname === '/api/v1/ocr/recognitions' || pathname === '/api/v1/ocr')) {
        let body;
        try {
          body = await parseJsonBody(req);
        } catch (err) {
          sendJson(res, 400, {
            code: 400,
            status: 'error',
            message: err.message,
            timestamp: Date.now()
          });
          return;
        }

        const inputImage = body.image || body.file_path || body.input;
        if (!inputImage || typeof inputImage !== 'string' || !inputImage.trim()) {
          sendJson(res, 400, {
            code: 400,
            status: 'error',
            message: '缺少必需参数: image (支持 Base64、Data URI 或图片绝对路径)',
            timestamp: Date.now()
          });
          return;
        }

        const overrideOptions = {
          det: body.det !== undefined ? !!body.det : true,
          cls: body.cls !== undefined ? !!body.cls : true,
          rec: body.rec !== undefined ? !!body.rec : true,
          readingOrder: body.readingOrder || 'horizontal-ltr',
          confidenceThreshold: typeof body.confidenceThreshold === 'number' ? body.confidenceThreshold : 0.3
        };

        const result = await engine.recognize(inputImage, overrideOptions);

        sendJson(res, 200, {
          code: 200,
          status: 'success',
          data: {
            text: result.text,
            durationMs: result.durationMs,
            lines: result.lines,
            meta: result.meta
          },
          timestamp: Date.now()
        });
        return;
      }

      // 5. Static Web Frontend fallback
      if (req.method === 'GET' || req.method === 'HEAD') {
        serveStatic(req, res, pathname);
        return;
      }

      // 6. Unknown API route
      sendJson(res, 404, {
        code: 404,
        status: 'error',
        message: `API 接口未找到: ${req.method} ${pathname}`,
        timestamp: Date.now()
      });

    } catch (err) {
      console.error(`[Server Error] ${req.method} ${pathname}:`, err);
      const isUserError = err instanceof LwPpocrError && err.code.startsWith('LW_FILE');
      const statusCode = isUserError ? 400 : 500;
      sendJson(res, statusCode, {
        code: statusCode,
        status: 'error',
        message: err.message || '内部处理异常',
        codeKey: err.code || 'INTERNAL_ERROR',
        timestamp: Date.now()
      });
    }
  });

  return new Promise((resolve) => {
    server.listen(port, host, () => {
      const addr = server.address();
      console.log(`[dsh-lw-PPOCR-C-project] Server running at http://${host}:${addr.port}`);
      resolve(server);
    });
  });
}

// Auto start if run directly
if (require.main === module) {
  createServer().catch(err => {
    console.error('Failed to start server:', err);
    process.exit(1);
  });
}

module.exports = {
  createServer,
  sendJson,
  parseJsonBody
};
