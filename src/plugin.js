/**
 * DeepSeek Harness (DSH) Plugin Entry
 * Cordis-compatible plugin implementation
 */

const { LwPpocrEngine } = require('./engine/lw-ppocr-engine');

const pluginName = 'dsh-lw-ppocr';

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
    ctx.provide('ocr', engine);
  } else {
    ctx.ocr = engine;
  }

  // 2. Register DSH Agent Tool if tool registry is present
  const toolDefinition = {
    name: 'ocr_recognize',
    description: '使用基于 lw.PPOCR.C 的轻量级纯 C/WASM 离线 OCR 引擎识别图片中的文字，返回提取文本、单行边界框与置信度。',
    parameters: {
      type: 'object',
      properties: {
        image: {
          type: 'string',
          description: '图片文件路径、Base64 编码字符串或 Data URI (data:image/...;base64,...)'
        },
        det: {
          type: 'boolean',
          description: '是否启用文字区域检测 (DET)，默认为 true'
        },
        cls: {
          type: 'boolean',
          description: '是否启用文字方向分类矫正 (CLS)，默认为 true'
        },
        rec: {
          type: 'boolean',
          description: '是否启用文字识别 (REC)，默认为 true'
        },
        readingOrder: {
          type: 'string',
          enum: ['horizontal-ltr', 'vertical-rtl', 'vertical-ltr'],
          description: '文字读取顺序，默认为 horizontal-ltr'
        }
      },
      required: ['image']
    },
    async execute(args) {
      return await engine.recognize(args.image, args);
    }
  };

  if (ctx.tools && typeof ctx.tools.register === 'function') {
    ctx.tools.register(toolDefinition.name, toolDefinition);
  } else if (ctx.tools && Array.isArray(ctx.tools)) {
    ctx.tools.push(toolDefinition);
  } else if (ctx.agent && typeof ctx.agent.registerTool === 'function') {
    ctx.agent.registerTool(toolDefinition);
  }

  // 3. Lifecycle disposal handler
  if (typeof ctx.on === 'function') {
    ctx.on('dispose', () => {
      // Clean up engine resources
      engine.initialized = false;
    });
  }

  return engine;
}

module.exports = {
  name: pluginName,
  apply,
  defaultConfig,
  LwPpocrEngine
};
