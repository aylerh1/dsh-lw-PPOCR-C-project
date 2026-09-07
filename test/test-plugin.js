/**
 * Test Suite for dsh-lw-PPOCR-C-project
 */

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const plugin = require('../src/index');
const { LwPpocrEngine, LwPpocrError, apply, defaultConfig } = plugin;

// Helper to create a minimal 1x1 valid PNG buffer
function createSamplePngBuffer() {
  return Buffer.from([
    0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG magic
    0x00, 0x00, 0x00, 0x0D, // IHDR chunk length (13)
    0x49, 0x48, 0x44, 0x52, // "IHDR"
    0x00, 0x00, 0x02, 0x80, // width: 640
    0x00, 0x00, 0x01, 0xE0, // height: 480
    0x08, 0x06, 0x00, 0x00, 0x00, // bit depth 8, truecolor with alpha
    0x35, 0x62, 0x1A, 0x56, // CRC
    0x00, 0x00, 0x00, 0x00, // IEND chunk length (0)
    0x49, 0x45, 0x4E, 0x44, // "IEND"
    0xAE, 0x42, 0x60, 0x82  // CRC
  ]);
}

async function runTests() {
  console.log('=== Starting dsh-lw-PPOCR-C-project Test Suite ===\n');

  // Test 1: Plugin metadata and exports
  console.log('Test 1: Verifying plugin exports and metadata...');
  assert.strictEqual(plugin.name, 'dsh-lw-ppocr', 'Plugin name should match');
  assert.strictEqual(typeof apply, 'function', 'apply should be a function');
  assert.strictEqual(defaultConfig.enabled, true, 'defaultConfig.enabled should be true');
  console.log('✔ Test 1 passed\n');

  // Test 2: Engine initialization and basic OCR
  console.log('Test 2: Engine initialization and basic OCR recognition...');
  const engine = new LwPpocrEngine({ confidenceThreshold: 0.3 });
  await engine.init();
  assert.strictEqual(engine.initialized, true, 'Engine should be initialized');

  const pngBuf = createSamplePngBuffer();
  const res = await engine.recognize(pngBuf);

  assert(typeof res.text === 'string', 'res.text should be string');
  assert(Array.isArray(res.lines), 'res.lines should be array');
  assert(res.lines.length > 0, 'Should return detected lines');
  assert(res.durationMs >= 0, 'Should record execution duration');
  assert.strictEqual(res.meta.width, 640, 'Width should match PNG header');
  assert.strictEqual(res.meta.height, 480, 'Height should match PNG header');
  console.log(`  Extracted text:\n  "${res.text}"`);
  console.log(`  Duration: ${res.durationMs}ms, Lines: ${res.lines.length}`);
  console.log('✔ Test 2 passed\n');

  // Test 3: Base64 Data URI input
  console.log('Test 3: Testing Base64 Data URI input...');
  const dataUri = `data:image/png;base64,${pngBuf.toString('base64')}`;
  const resBase64 = await engine.recognize(dataUri);
  assert(resBase64.lines.length > 0, 'Base64 image should be processed');
  assert.strictEqual(resBase64.meta.width, 640);
  console.log('✔ Test 3 passed\n');

  // Test 4: Pipeline configuration toggles
  console.log('Test 4: Testing DET / CLS / REC options...');
  const resNoDet = await engine.recognize(pngBuf, { det: false });
  assert.strictEqual(resNoDet.lines.length, 1, 'Without DET, should treat image as 1 box');
  assert.strictEqual(resNoDet.meta.det, false);

  const resNoRec = await engine.recognize(pngBuf, { rec: false });
  assert(resNoRec.lines.every(l => l.text === ''), 'Without REC, text should be empty');
  console.log('✔ Test 4 passed\n');

  // Test 5: Reading order sorting
  console.log('Test 5: Testing reading order options...');
  const resRtl = await engine.recognize(pngBuf, { readingOrder: 'vertical-rtl' });
  assert.strictEqual(resRtl.meta.readingOrder, 'vertical-rtl');
  console.log('✔ Test 5 passed\n');

  // Test 6: Cordis Plugin Lifecycle and Tool Registration
  console.log('Test 6: Testing Cordis context mounting and Agent Tool registration...');
  const mockTools = [];
  const mockContext = {
    ocr: null,
    provide(name, service) {
      this[name] = service;
    },
    tools: {
      register(tool, maybeDef) {
        mockTools.push(maybeDef || tool);
      }
    },
    on(event, handler) {
      this.disposeHandler = handler;
    }
  };

  const pluginEngine = apply(mockContext, { readingOrder: 'horizontal-ltr' });
  assert(mockContext.ocr instanceof LwPpocrEngine, 'ctx.ocr should be injected');
  assert.strictEqual(mockTools.length, 1, 'ocr_recognize tool should be registered');
  assert.strictEqual(mockTools[0].name, 'ocr_recognize');

  // Execute the registered agent tool directly
  const toolExecResult = await mockTools[0].execute({
    image: dataUri,
    det: true,
    rec: true
  });
  assert(typeof toolExecResult === 'string' && toolExecResult.length > 0, 'Agent tool execution should return text');
  console.log(`  Agent Tool output text: "${toolExecResult}"`);

  // Test dispose
  if (mockContext.disposeHandler) {
    mockContext.disposeHandler();
    assert.strictEqual(pluginEngine.initialized, false, 'Dispose should reset initialization');
  }
  console.log('✔ Test 6 passed\n');

  // Test 7: Error handling
  console.log('Test 7: Testing error handling for invalid input...');
  let caught = false;
  try {
    await engine.recognize('nonexistent-path-12345.png');
  } catch (err) {
    caught = true;
    assert(err instanceof LwPpocrError || err.name === 'LwPpocrError', 'Should throw LwPpocrError');
    assert.strictEqual(err.code, 'LW_FILE_NOT_FOUND');
  }
  assert(caught, 'Should catch file not found error');
  console.log('✔ Test 7 passed\n');

  console.log('==================================================');
  console.log('🎉 ALL 7 TESTS PASSED SUCCESSFULLY! 🎉');
  console.log('==================================================');
}

runTests().catch(err => {
  console.error('❌ Test failed:', err);
  process.exit(1);
});
