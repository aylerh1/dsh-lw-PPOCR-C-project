/**
 * Build script to generate dist/index.js, dist/index.mjs, dist/index.d.ts
 */

const fs = require('fs');
const path = require('path');

const distDir = path.resolve(__dirname, '../dist');
if (!fs.existsSync(distDir)) {
  fs.mkdirSync(distDir, { recursive: true });
}

// 1. Bundle CommonJS distribution
const engineSrc = fs.readFileSync(path.resolve(__dirname, '../src/engine/lw-ppocr-engine.js'), 'utf8');
const pluginSrc = fs.readFileSync(path.resolve(__dirname, '../src/plugin.js'), 'utf8');

// Inline engine into single bundle
const distCjs = `/**
 * dsh-lw-PPOCR-C-project v1.0.0
 * Lightweight pure C/WASM PP-OCR plugin for DeepSeek Harness
 * Zero Python/OpenCV/ONNX Runtime dependencies
 */

${engineSrc.replace("module.exports = {", "const _engineExports = {")}

${pluginSrc
  .replace("const { LwPpocrEngine } = require('./engine/lw-ppocr-engine');", "const { LwPpocrEngine } = _engineExports;")
  .replace("module.exports = {", "const _pluginExports = {")
}

module.exports = {
  name: _pluginExports.name,
  apply: _pluginExports.apply,
  defaultConfig: _pluginExports.defaultConfig,
  LwPpocrEngine: _engineExports.LwPpocrEngine,
  LwPpocrError: _engineExports.LwPpocrError
};
module.exports.default = module.exports;
`;

fs.writeFileSync(path.join(distDir, 'index.js'), distCjs, 'utf8');

// 2. ESM wrapper
const distEsm = `/**
 * dsh-lw-PPOCR-C-project v1.0.0 (ESM)
 */
import cjs from './index.js';

export const name = cjs.name;
export const apply = cjs.apply;
export const defaultConfig = cjs.defaultConfig;
export const LwPpocrEngine = cjs.LwPpocrEngine;
export const LwPpocrError = cjs.LwPpocrError;

export default cjs;
`;

fs.writeFileSync(path.join(distDir, 'index.mjs'), distEsm, 'utf8');

// 3. TypeScript definitions
const distDts = `/**
 * TypeScript Definitions for dsh-lw-PPOCR-C-project
 */

export interface LwPpocrOptions {
  det?: boolean;
  cls?: boolean;
  rec?: boolean;
  readingOrder?: 'horizontal-ltr' | 'vertical-rtl' | 'vertical-ltr';
  confidenceThreshold?: number;
  modelType?: string;
}

export interface OcrLineResult {
  index: number;
  text: string;
  confidence: number;
  box: number[][];
  angle: number;
}

export interface OcrResult {
  text: string;
  lines: OcrLineResult[];
  durationMs: number;
  meta: {
    width: number;
    height: number;
    det: boolean;
    cls: boolean;
    rec: boolean;
    readingOrder: string;
    engine: string;
  };
}

export declare class LwPpocrError extends Error {
  code: string;
  stage: string;
  constructor(message: string, code?: string, stage?: string);
}

export declare class LwPpocrEngine {
  options: LwPpocrOptions;
  initialized: boolean;
  constructor(options?: LwPpocrOptions);
  init(): Promise<void>;
  recognize(input: string | Buffer | Uint8Array, overrideOptions?: Partial<LwPpocrOptions>): Promise<OcrResult>;
}

export interface CordisContext {
  provide?(name: string, value: any): void;
  ocr?: LwPpocrEngine;
  tools?: any;
  agent?: any;
  on?(event: string, handler: Function): void;
  [key: string]: any;
}

export declare const name: string;
export declare const defaultConfig: LwPpocrOptions;
export declare function apply(ctx: CordisContext, config?: Partial<LwPpocrOptions>): LwPpocrEngine | void;

declare const _default: {
  name: string;
  apply: typeof apply;
  defaultConfig: LwPpocrOptions;
  LwPpocrEngine: typeof LwPpocrEngine;
  LwPpocrError: typeof LwPpocrError;
};

export default _default;
`;

fs.writeFileSync(path.join(distDir, 'index.d.ts'), distDts, 'utf8');
console.log('Build completed: dist/index.js, dist/index.mjs, dist/index.d.ts generated.');
