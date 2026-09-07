/**
 * lw.PPOCR.C Pure C/WASM Lightweight OCR Engine
 * Zero heavy AI dependencies, offline high-performance PP-OCRv6 inference runtime
 */

const fs = require('fs');
const path = require('path');
const { PNG } = require('pngjs');
const jpeg = require('jpeg-js');

class LwPpocrError extends Error {
  constructor(message, code, stage) {
    super(message);
    this.name = 'LwPpocrError';
    this.code = code || 'LW_UNKNOWN_ERROR';
    this.stage = stage || 'general';
  }
}

/**
 * Decode image buffer into raw BGR pixels
 * @param {Buffer} buffer
 * @returns {{width: number, height: number, bgr: Buffer}}
 */
function decodeImageToBgr(buffer) {
  let width, height, rgba;

  // 1. PNG format magic: 89 50 4E 47
  if (buffer.length >= 8 && buffer[0] === 0x89 && buffer[1] === 0x50 && buffer[2] === 0x4E && buffer[3] === 0x47) {
    const png = PNG.sync.read(buffer);
    width = png.width;
    height = png.height;
    rgba = png.data;
  }
  // 2. JPEG format magic: FF D8
  else if (buffer.length >= 2 && buffer[0] === 0xFF && buffer[1] === 0xD8) {
    const decoded = jpeg.decode(buffer, { useTArray: true });
    width = decoded.width;
    height = decoded.height;
    rgba = decoded.data;
  }
  // 3. BMP format magic: 42 4D
  else if (buffer.length >= 54 && buffer[0] === 0x42 && buffer[1] === 0x4D) {
    const dataOffset = buffer.readUInt32LE(10);
    width = buffer.readInt32LE(18);
    height = Math.abs(buffer.readInt32LE(22));
    const bpp = buffer.readUInt16LE(28);
    const isTopDown = buffer.readInt32LE(22) < 0;
    const bgr = Buffer.allocUnsafe(width * height * 3);
    const rowSize = Math.floor((bpp * width + 31) / 32) * 4;

    for (let y = 0; y < height; y++) {
      const srcY = isTopDown ? y : (height - 1 - y);
      const srcRow = dataOffset + srcY * rowSize;
      const dstRow = y * width * 3;
      for (let x = 0; x < width; x++) {
        if (bpp === 24) {
          bgr[dstRow + x * 3] = buffer[srcRow + x * 3];         // B
          bgr[dstRow + x * 3 + 1] = buffer[srcRow + x * 3 + 1]; // G
          bgr[dstRow + x * 3 + 2] = buffer[srcRow + x * 3 + 2]; // R
        } else if (bpp === 32) {
          bgr[dstRow + x * 3] = buffer[srcRow + x * 4];         // B
          bgr[dstRow + x * 3 + 1] = buffer[srcRow + x * 4 + 1]; // G
          bgr[dstRow + x * 3 + 2] = buffer[srcRow + x * 4 + 2]; // R
        }
      }
    }
    return { width, height, bgr };
  } else {
    throw new LwPpocrError('不支持的图像格式，仅支持 PNG、JPEG 或 BMP 格式', 'LW_UNSUPPORTED_FORMAT', 'preprocessing');
  }

  // Convert RGBA to BGR buffer
  const bgr = Buffer.allocUnsafe(width * height * 3);
  for (let i = 0, j = 0; i < rgba.length; i += 4, j += 3) {
    bgr[j] = rgba[i + 2];     // B
    bgr[j + 1] = rgba[i + 1]; // G
    bgr[j + 2] = rgba[i];     // R
  }

  return { width, height, bgr };
}

class LwPpocrEngine {
  constructor(options = {}) {
    this.options = Object.assign({
      det: true,
      cls: true,
      rec: true,
      readingOrder: 'horizontal-ltr',
      confidenceThreshold: 0.3,
      modelType: 'ppocrv6-tiny'
    }, options);

    this.initialized = false;
    this.runtime = null;
    this.info = null;
    this.modelDir = this._resolveModelDir(options.modelDir);
  }

  _resolveModelDir(customDir) {
    const candidates = [
      customDir,
      path.resolve(__dirname, '../../vendor/lw-ppocr-wasm'),
      path.resolve(__dirname, '../vendor/lw-ppocr-wasm'),
      path.resolve(process.cwd(), 'vendor/lw-ppocr-wasm')
    ].filter(Boolean);

    for (const dir of candidates) {
      if (fs.existsSync(path.join(dir, 'runtime.cjs')) && fs.existsSync(path.join(dir, 'rec.lwm'))) {
        return dir;
      }
    }

    return path.resolve(__dirname, '../../vendor/lw-ppocr-wasm');
  }

  /**
   * Initialize WASM runtime and mount offline model files
   */
  async init() {
    if (this.initialized) {
      return;
    }

    const runtimeModulePath = path.join(this.modelDir, 'runtime.cjs');
    if (!fs.existsSync(runtimeModulePath)) {
      throw new LwPpocrError(
        `未找到 lw.PPOCR.C WASM 运行时: ${runtimeModulePath}`,
        'LW_RUNTIME_NOT_FOUND',
        'initialization'
      );
    }

    const LwPpocrModule = require(runtimeModulePath);
    this.runtime = await LwPpocrModule({});

    // Mount models into WASM virtual filesystem
    try {
      this.runtime.FS.mkdir('/models');
    } catch (e) {
      // directory may already exist
    }

    const requiredModels = ['det.lwm', 'cls.lwm', 'rec.lwm', 'ppocr_keys.txt'];
    for (const name of requiredModels) {
      const modelPath = path.join(this.modelDir, name);
      if (!fs.existsSync(modelPath)) {
        throw new LwPpocrError(
          `未找到模型资产文件: ${modelPath}`,
          'LW_MODEL_NOT_FOUND',
          'initialization'
        );
      }
      this.runtime.FS.writeFile(`/models/${name}`, fs.readFileSync(modelPath));
    }

    // Initialize C runtime with CLS direction classification enabled
    const useCls = this.options.cls !== false ? 1 : 0;
    const status = this.runtime._lw_web_init(useCls);
    if (status !== 0) {
      throw new LwPpocrError(`lw_web_init 初始化失败，代码: ${status}`, 'LW_INIT_FAILED', 'initialization');
    }

    // Query Host ABI info
    const infoPointer = this.runtime._lw_web_malloc(20);
    if (!infoPointer || this.runtime._lw_web_get_info(infoPointer) !== 0) {
      throw new LwPpocrError('无法查询 WASM Host ABI 信息', 'LW_ABI_ERROR', 'initialization');
    }

    const u32 = p => this.runtime.HEAPU32[p >> 2] >>> 0;
    this.info = {
      abi: u32(infoPointer),
      maxLines: u32(infoPointer + 4),
      maxText: u32(infoPointer + 8),
      lineSize: u32(infoPointer + 12),
      resultSize: u32(infoPointer + 16)
    };
    this.runtime._lw_web_free(infoPointer);

    this.initialized = true;
  }

  /**
   * Run real OCR inference on input image
   * @param {string|Buffer|Uint8Array} input Image path, Buffer, base64 string, or data URI
   * @param {Object} [overrideOptions]
   * @returns {Promise<{text: string, lines: Array, durationMs: number, meta: Object}>}
   */
  async recognize(input, overrideOptions = {}) {
    if (!this.initialized) {
      await this.init();
    }

    const opts = Object.assign({}, this.options, overrideOptions);
    const startTime = Date.now();

    // 1. Resolve input into buffer and decode to BGR
    const rawBuffer = this._resolveImageInput(input);
    const decoded = decodeImageToBgr(rawBuffer);

    // 2. Prepare WASM memory allocations
    const runtime = this.runtime;
    const info = this.info;

    const source = runtime._lw_web_malloc(decoded.bgr.length);
    const linesPtr = runtime._lw_web_malloc(info.maxLines * info.lineSize);
    const textPtr = runtime._lw_web_malloc(info.maxText);
    const resultPtr = runtime._lw_web_malloc(info.resultSize);

    if (!source || !linesPtr || !textPtr || !resultPtr) {
      throw new LwPpocrError('WASM 运行内存分配失败', 'LW_OOM', 'runtime');
    }

    const lines = [];

    try {
      runtime.HEAPU8.set(decoded.bgr, source);
      const runStatus = runtime._lw_web_run(
        source, decoded.bgr.length, decoded.width, decoded.height, decoded.width * 3,
        linesPtr, info.maxLines, textPtr, info.maxText, resultPtr
      );

      if (runStatus !== 0) {
        throw new LwPpocrError(`lw_web_run 推理执行失败，错误码: ${runStatus}`, 'LW_INFERENCE_FAILED', 'runtime');
      }

      const lineCount = runtime.HEAPU32[resultPtr >> 2] >>> 0;
      const decoder = new TextDecoder('utf-8');

      for (let idx = 0; idx < lineCount; idx++) {
        const ptr = linesPtr + idx * info.lineSize;
        const offset = runtime.HEAPU32[(ptr + 52) >> 2] >>> 0;
        const len = runtime.HEAPU32[(ptr + 56) >> 2] >>> 0;

        const text = decoder.decode(runtime.HEAPU8.subarray(textPtr + offset, textPtr + offset + len));
        const detScore = runtime.HEAPF32[(ptr + 32) >> 2];
        const recScore = runtime.HEAPF32[(ptr + 36) >> 2];
        const clsScore = runtime.HEAPF32[(ptr + 40) >> 2];
        const angle = runtime.HEAPU32[(ptr + 48) >> 2];

        // 4 corners quadrilaterals: [[x0, y0], [x1, y1], [x2, y2], [x3, y3]]
        const box = [
          [Math.round(runtime.HEAPF32[(ptr + 0) >> 2]), Math.round(runtime.HEAPF32[(ptr + 4) >> 2])],
          [Math.round(runtime.HEAPF32[(ptr + 8) >> 2]), Math.round(runtime.HEAPF32[(ptr + 12) >> 2])],
          [Math.round(runtime.HEAPF32[(ptr + 16) >> 2]), Math.round(runtime.HEAPF32[(ptr + 20) >> 2])],
          [Math.round(runtime.HEAPF32[(ptr + 24) >> 2]), Math.round(runtime.HEAPF32[(ptr + 28) >> 2])]
        ];

        const confidence = Math.round(((detScore + recScore) / 2) * 1000) / 1000;

        if (recScore >= (opts.confidenceThreshold || 0.3) && text.trim().length > 0) {
          lines.push({
            index: idx,
            text: text.trim(),
            confidence: confidence,
            detScore: Math.round(detScore * 1000) / 1000,
            recScore: Math.round(recScore * 1000) / 1000,
            box: box,
            angle: angle
          });
        }
      }
    } finally {
      for (const p of [source, linesPtr, textPtr, resultPtr]) {
        if (p) runtime._lw_web_free(p);
      }
    }

    // 3. Sort lines according to reading order
    this._sortLines(lines, opts.readingOrder || 'horizontal-ltr');

    const fullText = lines.map(l => l.text).filter(Boolean).join('\n');
    const durationMs = Date.now() - startTime;

    return {
      text: fullText,
      lines: lines,
      durationMs: durationMs,
      meta: {
        width: decoded.width,
        height: decoded.height,
        det: opts.det !== false,
        cls: opts.cls !== false,
        rec: opts.rec !== false,
        readingOrder: opts.readingOrder || 'horizontal-ltr',
        engine: 'lw.PPOCR.C-wasm'
      }
    };
  }

  /**
   * Parse input into decoded raw image buffer
   * @private
   */
  _resolveImageInput(input) {
    if (typeof input === 'string') {
      if (input.startsWith('data:image/')) {
        const match = input.match(/^data:image\/[a-zA-Z0-9.+_-]+;base64,(.+)$/);
        if (!match) {
          throw new LwPpocrError('Invalid Data URI format', 'LW_INPUT_INVALID', 'preprocessing');
        }
        return Buffer.from(match[1], 'base64');
      } else if (input.match(/^[A-Za-z0-9+/=]+$/) && input.length > 100 && !fs.existsSync(input)) {
        return Buffer.from(input, 'base64');
      } else {
        const resolved = path.resolve(input);
        if (!fs.existsSync(resolved)) {
          throw new LwPpocrError(`File not found: ${input}`, 'LW_FILE_NOT_FOUND', 'preprocessing');
        }
        return fs.readFileSync(resolved);
      }
    } else if (Buffer.isBuffer(input)) {
      return input;
    } else if (input instanceof Uint8Array) {
      return Buffer.from(input);
    } else {
      throw new LwPpocrError('Unsupported image input type', 'LW_INPUT_TYPE_ERROR', 'preprocessing');
    }
  }

  /**
   * Sort text lines according to reading order
   * @private
   */
  _sortLines(lines, readingOrder) {
    if (readingOrder === 'vertical-rtl') {
      lines.sort((a, b) => {
        const ax = (a.box[0][0] + a.box[1][0]) / 2;
        const bx = (b.box[0][0] + b.box[1][0]) / 2;
        return bx - ax; // right to left
      });
    } else if (readingOrder === 'vertical-ltr') {
      lines.sort((a, b) => {
        const ax = (a.box[0][0] + a.box[1][0]) / 2;
        const bx = (b.box[0][0] + b.box[1][0]) / 2;
        return ax - bx; // left to right
      });
    } else {
      // horizontal-ltr
      lines.sort((a, b) => {
        const ay = (a.box[0][1] + a.box[3][1]) / 2;
        const by = (b.box[0][1] + b.box[3][1]) / 2;
        if (Math.abs(ay - by) > 12) {
          return ay - by; // top to bottom
        }
        const ax = a.box[0][0];
        const bx = b.box[0][0];
        return ax - bx; // left to right within line
      });
    }
  }

  /**
   * Release WASM runtime resources
   */
  dispose() {
    if (this.initialized && this.runtime) {
      try {
        this.runtime._lw_web_shutdown();
      } catch (e) {
        // ignore
      }
      this.initialized = false;
      this.runtime = null;
      this.info = null;
    }
  }
}

module.exports = {
  LwPpocrEngine,
  LwPpocrError,
  decodeImageToBgr
};
