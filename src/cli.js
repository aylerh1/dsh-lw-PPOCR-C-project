/**
 * Lightweight Standalone CLI Worker for lw-PPOCR
 * Reads JSON request payload from stdin, executes WASM OCR inference,
 * writes JSON result to stdout, and exits immediately.
 */

const { LwPpocrEngine } = require('./engine/lw-ppocr-engine');

async function main() {
  let raw = '';
  process.stdin.setEncoding('utf8');

  for await (const chunk of process.stdin) {
    raw += chunk;
  }

  if (!raw.trim()) {
    console.log(JSON.stringify({
      code: 400,
      status: 'error',
      message: '缺少请求数据 (Empty input payload)',
      timestamp: Date.now()
    }));
    process.exit(1);
  }

  let body;
  try {
    body = JSON.parse(raw);
  } catch (e) {
    console.log(JSON.stringify({
      code: 400,
      status: 'error',
      message: `Invalid JSON: ${e.message}`,
      timestamp: Date.now()
    }));
    process.exit(1);
  }

  const inputImage = body.image || body.file_path || body.input;
  if (!inputImage || typeof inputImage !== 'string' || !inputImage.trim()) {
    console.log(JSON.stringify({
      code: 400,
      status: 'error',
      message: '缺少必需参数: image (支持 Base64、Data URI 或图片绝对路径)',
      timestamp: Date.now()
    }));
    process.exit(1);
  }

  const options = {
    det: body.det !== undefined ? !!body.det : true,
    cls: body.cls !== undefined ? !!body.cls : true,
    rec: body.rec !== undefined ? !!body.rec : true,
    readingOrder: body.readingOrder || 'horizontal-ltr',
    confidenceThreshold: typeof body.confidenceThreshold === 'number' ? body.confidenceThreshold : 0.3
  };

  const engine = new LwPpocrEngine(options);

  try {
    const result = await engine.recognize(inputImage, options);
    console.log(JSON.stringify({
      code: 200,
      status: 'success',
      data: {
        text: result.text,
        durationMs: result.durationMs,
        lines: result.lines,
        meta: result.meta
      },
      timestamp: Date.now()
    }));
    process.exit(0);
  } catch (err) {
    console.log(JSON.stringify({
      code: 500,
      status: 'error',
      message: err.message || 'OCR 推理执行异常',
      timestamp: Date.now()
    }));
    process.exit(1);
  }
}

main().catch(err => {
  console.error('Fatal CLI Error:', err);
  process.exit(1);
});
