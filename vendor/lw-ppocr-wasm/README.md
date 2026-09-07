# lw.PPOCR.C Node/WASM runtime (0.1.0)

This archive contains the standalone Emscripten runtime and the bundled
PP-OCRv6 tiny LWM assets. It has no npm runtime dependencies and does not
decode JPEG/PNG files. Applications should provide BGR8 pixels to the WASM
Host ABI.

This package uses WASM Host ABI v1, LWM format 0.1, and
the `wasm128` execution backend.

## Requirements

- Node.js 18 or newer
- CommonJS `require()` support

## Minimal initialization

```javascript
const fs = require("node:fs");
const path = require("node:path");
const LwPpocrModule = require("./runtime.cjs");

async function main() {
  const runtime = await LwPpocrModule({});
  runtime.FS.mkdir("/models");
  for (const name of ["det.lwm", "cls.lwm", "rec.lwm", "ppocr_keys.txt"]) {
    runtime.FS.writeFile(`/models/${name}`,
      fs.readFileSync(path.join(__dirname, name)));
  }
  const status = runtime._lw_web_init(1);
  if (status !== 0) throw new Error(`OCR initialization failed: ${status}`);
  console.log("lw.PPOCR.C WASM runtime ready");
  runtime._lw_web_shutdown();
}

main().catch(error => { console.error(error); process.exitCode = 1; });
```

The exported `lw_web_*` symbols are the stable WASM Host ABI v1. Use one
runtime instance from one request at a time; create separate Node Worker
instances when an application needs concurrency. See `manifest.json` and
`SHA256SUMS.txt` before loading assets.
