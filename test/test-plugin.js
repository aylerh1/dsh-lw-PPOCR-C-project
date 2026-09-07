/**
 * Test Suite for dsh-lw-PPOCR-C-project
 * Validates pure C/WASM real offline OCR inference
 */

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const plugin = require('../src/index');
const { LwPpocrEngine, LwPpocrError, apply, defaultConfig } = plugin;

const sampleFixturePath = path.resolve(__dirname, 'fixtures/sample.png');

async function runTests() {
  console.log('=== Starting dsh-lw-PPOCR-C-project Test Suite ===\n');

  // Test 1: Plugin metadata and exports
  console.log('Test 1: Verifying plugin exports and metadata...');
  assert.strictEqual(plugin.name, 'dsh-lw-ppocr', 'Plugin name should match');
  assert.strictEqual(typeof apply, 'function', 'apply should be a function');
  assert.strictEqual(defaultConfig.enabled, true, 'defaultConfig.enabled should be true');
  console.log('✔ Test 1 passed\n');

  // Test 2: Real Engine initialization and offline OCR recognition
  console.log('Test 2: Real Engine initialization and offline WASM OCR recognition...');
  const engine = new LwPpocrEngine({ confidenceThreshold: 0.3 });
  await engine.init();
  assert.strictEqual(engine.initialized, true, 'Engine should be initialized');

  assert(fs.existsSync(sampleFixturePath), 'Sample fixture image must exist');
  const sampleBuf = fs.readFileSync(sampleFixturePath);
  const res = await engine.recognize(sampleBuf);

  assert(typeof res.text === 'string', 'res.text should be string');
  assert(Array.isArray(res.lines), 'res.lines should be array');
  assert(res.lines.length > 0, 'Should detect real text lines');
  assert(res.lines.some(l => l.text.includes('Container') || l.text.includes('Name') || l.text.includes('zip')),
    'Should accurately recognize text from image');
  assert(res.durationMs >= 0, 'Should record execution duration');
  assert(res.meta.width > 0 && res.meta.height > 0, 'Image dimensions should be valid');

  console.log(`  Extracted text:\n  "${res.text}"`);
  console.log(`  Duration: ${res.durationMs}ms, Lines: ${res.lines.length}`);
  console.log('✔ Test 2 passed\n');

  // Test 3: Base64 Data URI input
  console.log('Test 3: Testing Base64 Data URI input...');
  const dataUri = `data:image/png;base64,${sampleBuf.toString('base64')}`;
  const resBase64 = await engine.recognize(dataUri);
  assert(resBase64.lines.length > 0, 'Base64 image should be processed');
  assert.strictEqual(resBase64.text, res.text, 'Base64 recognition should match buffer recognition');
  console.log('✔ Test 3 passed\n');

  // Test 4: Pipeline configuration toggles
  console.log('Test 4: Testing reading order and confidence threshold...');
  const resHighConf = await engine.recognize(sampleBuf, { confidenceThreshold: 0.99999 });
  assert(resHighConf.lines.length <= res.lines.length, 'Higher threshold should filter results');

  const resRtl = await engine.recognize(sampleBuf, { readingOrder: 'vertical-rtl' });
  assert.strictEqual(resRtl.meta.readingOrder, 'vertical-rtl');
  console.log('✔ Test 4 passed\n');

  // Test 5: Cordis Plugin Lifecycle and Tool Registration
  console.log('Test 5: Testing Cordis context mounting and Agent Tool registration...');
  const mockTools = [];
  const mockContext = {
    ocr: null,
    provide(name, service) {
      this[name] = service;
    },
    tools: {
      register(tool) {
        mockTools.push(tool);
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
    image: sampleFixturePath
  });
  assert(typeof toolExecResult === 'string' && toolExecResult.includes('OCR 识别结果:'), 'Agent tool should output formatted result');
  console.log(`  Agent Tool output:\n${toolExecResult}`);

  // Test dispose
  if (mockContext.disposeHandler) {
    mockContext.disposeHandler();
    assert.strictEqual(pluginEngine.initialized, false, 'Dispose should reset initialization');
  }
  console.log('✔ Test 5 passed\n');

  // Test 6: Error handling
  console.log('Test 6: Testing error handling for invalid input...');
  let caught = false;
  try {
    await engine.recognize('nonexistent-path-12345.png');
  } catch (err) {
    caught = true;
    assert(err instanceof LwPpocrError || err.name === 'LwPpocrError', 'Should throw LwPpocrError');
    assert.strictEqual(err.code, 'LW_FILE_NOT_FOUND');
  }
  assert(caught, 'Should catch file not found error');
  console.log('✔ Test 6 passed\n');

  console.log('==================================================');
  console.log('🎉 ALL 6 REAL OCR TESTS PASSED SUCCESSFULLY! 🎉');
  console.log('==================================================');

  // Clean up WASM engine
  engine.dispose();
}

runTests().catch(err => {
  console.error('❌ Test failed:', err);
  process.exit(1);
});
