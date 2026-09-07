/**
 * dsh-lw-PPOCR-C-project
 * Entry point for DeepSeek Harness OCR Plugin
 */

const { name, inject, apply, defaultConfig, LwPpocrEngine } = require('./plugin');
const { LwPpocrError } = require('./engine/lw-ppocr-engine');

module.exports = {
  name,
  inject,
  apply,
  defaultConfig,
  LwPpocrEngine,
  LwPpocrError
};

// Default export is the plugin definition for Cordis loader
module.exports.default = module.exports;
