/**
 * Test Suite for RESTful API Server
 * Validates /api/v1/health, /api/v1/sample, and /api/v1/ocr/recognitions endpoints
 */

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
    console.log('1. Testing GET /api/v1/health...');
    const health = await request({ hostname: '127.0.0.1', port, path: '/api/v1/health', method: 'GET' });
    assert.strictEqual(health.status, 200, 'Health check should return 200');
    assert.strictEqual(health.body.status, 'success');
    assert.strictEqual(health.body.data.engine, 'ready');
    console.log('✔ GET /api/v1/health passed');

    // 2. Sample image
    console.log('2. Testing GET /api/v1/sample...');
    const sample = await request({ hostname: '127.0.0.1', port, path: '/api/v1/sample', method: 'GET' });
    assert.strictEqual(sample.status, 200, 'Sample endpoint should return 200');
    assert.strictEqual(sample.body.status, 'success');
    assert(typeof sample.body.data.image === 'string' && sample.body.data.image.startsWith('data:image/png;base64,'), 'Sample image should be a base64 data URI');
    console.log('✔ GET /api/v1/sample passed');

    // 3. OCR recognition POST
    console.log('3. Testing POST /api/v1/ocr/recognitions with sample image...');
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
    assert.strictEqual(ocrRes.status, 200, 'OCR recognize should return 200');
    assert.strictEqual(ocrRes.body.status, 'success');
    assert(typeof ocrRes.body.data.durationMs === 'number' && ocrRes.body.data.durationMs >= 0, 'durationMs should be a valid number');
    assert(Array.isArray(ocrRes.body.data.lines) && ocrRes.body.data.lines.length > 0, 'lines should be detected');
    assert(typeof ocrRes.body.data.text === 'string' && ocrRes.body.data.text.length > 0, 'text should be non-empty');
    assert(ocrRes.body.data.lines.some(l => l.text.includes('Container') || l.text.includes('Name') || l.text.includes('zip')), 'Text should be correctly recognized');
    console.log(`✔ POST /api/v1/ocr/recognitions passed (duration: ${ocrRes.body.data.durationMs}ms, lines: ${ocrRes.body.data.lines.length})`);

    // 4. Invalid input handling
    console.log('4. Testing error handling with empty body...');
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
    assert.strictEqual(errRes.status, 400, 'Empty input should return 400');
    assert.strictEqual(errRes.body.status, 'error');
    console.log('✔ Error handling 400 Bad Request passed');

    console.log('🎉 All API server tests passed successfully! 🎉');
  } finally {
    server.close();
  }
}

run().catch(err => {
  console.error('❌ Server test failed:', err);
  process.exit(1);
});
