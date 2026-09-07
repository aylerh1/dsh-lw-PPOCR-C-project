/**
 * DeepSeek Harness (DSH) Plugin Entry
 * Cordis-compatible plugin implementation
 */

const { LwPpocrEngine } = require('./engine/lw-ppocr-engine');

const pluginName = 'dsh-lw-ppocr';
const inject = ['tools'];

const defaultConfig = {
  enabled: true,
  det: true,
  cls: true,
  rec: true,
  readingOrder: 'horizontal-ltr',
  confidenceThreshold: 0.5,
  modelType: 'ppocrv6-tiny'
};

/**
 * Build standard JSON Schema compliant tool definition
 * Compatible with Gemini, OpenAI, Anthropic, and DSH Native Agent Tools
 * @param {LwPpocrEngine} engine
 */
function createOcrToolDefinition(engine) {
  return {
    name: 'ocr_recognize',
    description: '轻量级离线 OCR 文字识别引擎：从本地图片文件路径、Base64 或 Data URI 中提取所有文字内容与排版（当用户需要查看图片内容、识别图片文字、阅读图片或提取图像中文本时调用）',
    parameters: {
      type: 'object',
      properties: {
        image: {
          type: 'string',
          description: '图片文件绝对路径、Base64 编码字符串或 Data URI (data:image/...;base64,...)',
        },
        det: {
          type: 'boolean',
          description: '是否启用文字区域检测 (DET)，默认为 true',
        },
        cls: {
          type: 'boolean',
          description: '是否启用文字方向分类矫正 (CLS)，默认为 true',
        },
        rec: {
          type: 'boolean',
          description: '是否启用文字识别 (REC)，默认为 true',
        },
        readingOrder: {
          type: 'string',
          description: '文字读取顺序: horizontal-ltr, vertical-rtl, vertical-ltr',
        }
      },
      required: ['image']
    },
    output: {
      schema: { type: 'string' },
      render: (_args, value) => [{ type: 'text', text: value }],
    },
    isConcurrencySafe: () => true,
    async execute(args) {
      const input = (args && typeof args === 'object')
        ? (args.image || args.file_path || args.input || args.path || args.file)
        : args;
      const res = await engine.recognize(input, args);
      return res.text ? `OCR 识别结果:\n${res.text}` : '（未在图片中检测到可识别的文字内容）';
    }
  };
}

/**
 * Cordis apply function
 * @param {Object} ctx Cordis context
 * @param {Object} config Plugin configuration
 */
function apply(ctx, config = {}) {
  const mergedConfig = Object.assign({}, defaultConfig, config);
  if (mergedConfig.enabled === false) {
    return;
  }

  const engine = new LwPpocrEngine(mergedConfig);

  // 1. Provide OCR service on context
  if (typeof ctx.provide === 'function') {
    try {
      ctx.provide('ocr', engine);
    } catch (e) {
      // ignore
    }
  }

  // 2. Register DSH Agent Tool if tools service is available
  try {
    const toolDefinition = createOcrToolDefinition(engine);

    if (ctx.tools && typeof ctx.tools.register === 'function') {
      ctx.tools.register(toolDefinition);
    } else if (ctx.tools && Array.isArray(ctx.tools)) {
      ctx.tools.push(toolDefinition);
    }
  } catch (err) {
    console.warn(`[${pluginName}] failed to register ocr_recognize tool:`, err.message);
  }

  // 3. Lifecycle disposal handler
  if (typeof ctx.on === 'function') {
    ctx.on('dispose', () => {
      engine.initialized = false;
    });
  }

  return engine;
}

module.exports = {
  name: pluginName,
  inject,
  apply,
  defaultConfig,
  LwPpocrEngine,
  createOcrToolDefinition
};
