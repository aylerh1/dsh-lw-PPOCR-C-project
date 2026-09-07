/**
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
