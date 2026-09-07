/**
 * Frontend Interactive Controller for PP-OCR Web
 * Supports Drag & Drop, Clipboard Paste, Canvas Bounding Box Visuals, RESTful API
 */

document.addEventListener('DOMContentLoaded', () => {
  // Elements
  const dropZone = document.getElementById('drop-zone');
  const fileInput = document.getElementById('file-input');
  const previewContainer = document.getElementById('preview-container');
  const previewImage = document.getElementById('preview-image');
  const canvas = document.getElementById('annotation-canvas');
  const tooltip = document.getElementById('hover-tooltip');
  const btnRunOcr = document.getElementById('btn-run-ocr');
  const btnReset = document.getElementById('btn-reset');
  const btnLoadSample = document.getElementById('btn-load-sample');
  const btnCopyResult = document.getElementById('btn-copy-result');
  const copyBtnText = document.getElementById('copy-btn-text');

  // Metrics elements
  const durationVal = document.getElementById('duration-val');
  const linesCountVal = document.getElementById('lines-count-val');
  const roundtripVal = document.getElementById('roundtrip-val');
  const confidenceVal = document.getElementById('confidence-val');
  const healthIndicator = document.getElementById('health-indicator');
  const healthText = document.getElementById('health-text');

  // Settings
  const checkDet = document.getElementById('check-det');
  const checkCls = document.getElementById('check-cls');
  const checkRec = document.getElementById('check-rec');

  // Results & Tabs
  const tabBtns = document.querySelectorAll('.tab-btn');
  const tabPanes = document.querySelectorAll('.tab-pane');
  const resultEmpty = document.getElementById('result-empty');
  const loadingOverlay = document.getElementById('loading-overlay');
  const outputText = document.getElementById('output-text');
  const linesList = document.getElementById('lines-list');
  const outputJson = document.getElementById('output-json');
  const tabLinesBadge = document.getElementById('tab-lines-badge');

  // Internal State
  let currentImageDataUrl = null;
  let ocrResultData = null;
  let hoveredLineIndex = -1;

  // 1. Health Check Polling on Load
  async function checkHealth() {
    try {
      const res = await fetch('/api/v1/health');
      if (res.ok) {
        const data = await res.json();
        if (data.data && data.data.engine === 'ready') {
          healthIndicator.className = 'status-indicator ready';
          healthText.textContent = '引擎就绪 (Ready)';
        }
      }
    } catch (e) {
      healthIndicator.className = 'status-indicator';
      healthText.textContent = '服务连接中...';
    }
  }
  checkHealth();

  // 2. Tab Switching
  tabBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      tabBtns.forEach(b => b.classList.remove('active'));
      tabPanes.forEach(p => p.classList.add('hidden'));

      btn.classList.add('active');
      const targetId = btn.getAttribute('data-tab');
      const targetPane = document.getElementById(targetId);
      if (targetPane) {
        targetPane.classList.remove('hidden');
      }
    });
  });

  // 3. Image Loading & Preview
  function loadFile(file) {
    if (!file || !file.type.startsWith('image/')) {
      showToast('请选择有效的图片文件 (PNG, JPG, BMP)', 'error');
      return;
    }
    const reader = new FileReader();
    reader.onload = (e) => {
      setImageData(e.target.result);
      showToast(`已成功载入图片: ${file.name || '截图'}`, 'success');
    };
    reader.readAsDataURL(file);
  }

  function setImageData(dataUrl) {
    currentImageDataUrl = dataUrl;
    ocrResultData = null;
    hoveredLineIndex = -1;

    previewImage.src = dataUrl;
    previewImage.onload = () => {
      dropZone.classList.add('hidden');
      previewContainer.classList.remove('hidden');
      btnRunOcr.disabled = false;
      clearCanvas();
      // Auto run OCR recognition for seamless UX
      runOcr();
    };
  }

  // File picker click
  dropZone.addEventListener('click', () => fileInput.click());
  fileInput.addEventListener('change', (e) => {
    if (e.target.files && e.target.files[0]) {
      loadFile(e.target.files[0]);
    }
  });

  // Drag & Drop
  ['dragenter', 'dragover'].forEach(eventName => {
    dropZone.addEventListener(eventName, (e) => {
      e.preventDefault();
      dropZone.classList.add('dragover');
    });
  });

  ['dragleave', 'drop'].forEach(eventName => {
    dropZone.addEventListener(eventName, (e) => {
      e.preventDefault();
      dropZone.classList.remove('dragover');
    });
  });

  dropZone.addEventListener('drop', (e) => {
    if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]) {
      loadFile(e.dataTransfer.files[0]);
    }
  });

  // Global Clipboard Paste (Ctrl+V)
  window.addEventListener('paste', (e) => {
    const items = e.clipboardData && e.clipboardData.items;
    if (!items) return;

    for (let i = 0; i < items.length; i++) {
      if (items[i].type.indexOf('image') !== -1) {
        const file = items[i].getAsFile();
        if (file) {
          loadFile(file);
          break;
        }
      }
    }
  });

  // Reset
  btnReset.addEventListener('click', () => {
    currentImageDataUrl = null;
    ocrResultData = null;
    hoveredLineIndex = -1;
    fileInput.value = '';

    dropZone.classList.remove('hidden');
    previewContainer.classList.add('hidden');
    btnRunOcr.disabled = true;

    durationVal.textContent = '--';
    linesCountVal.textContent = '0';
    roundtripVal.textContent = '--';
    confidenceVal.textContent = '--%';

    outputText.value = '';
    linesList.innerHTML = '';
    tabLinesBadge.textContent = '0';
    outputJson.textContent = '// 等待识别生成 RESTful 结构化数据...';

    resultEmpty.classList.remove('hidden');
    clearCanvas();
    showToast('已重置状态', 'success');
  });

  // Load Built-in Sample Image
  btnLoadSample.addEventListener('click', async () => {
    try {
      showToast('正在获取内置样例图...', 'success');
      const res = await fetch('/api/v1/sample');
      if (!res.ok) throw new Error('获取样例图片失败');
      const json = await res.json();
      if (json.data && json.data.image) {
        setImageData(json.data.image);
      }
    } catch (err) {
      showToast(err.message, 'error');
    }
  });

  // 4. Run OCR Recognition Request (POST /api/v1/ocr/recognitions)
  async function runOcr() {
    if (!currentImageDataUrl) return;

    loadingOverlay.classList.remove('hidden');
    btnRunOcr.disabled = true;
    resultEmpty.classList.add('hidden');

    const startTime = performance.now();

    try {
      const payload = {
        image: currentImageDataUrl,
        det: checkDet.checked,
        cls: checkCls.checked,
        rec: checkRec.checked,
        confidenceThreshold: 0.3
      };

      const response = await fetch('/api/v1/ocr/recognitions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(payload)
      });

      const totalLatency = Math.round(performance.now() - startTime);
      roundtripVal.textContent = totalLatency;

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `识别失败 (HTTP ${response.status})`);
      }

      const resData = await response.json();
      if (resData.status !== 'success' || !resData.data) {
        throw new Error(resData.message || '返回数据格式不正确');
      }

      ocrResultData = resData.data;
      renderOcrResults(ocrResultData, totalLatency);
      showToast(`识别成功！WASM 耗时: ${ocrResultData.durationMs}ms`, 'success');

    } catch (err) {
      console.error('OCR Error:', err);
      showToast(`识别错误: ${err.message}`, 'error');
      resultEmpty.classList.remove('hidden');
    } finally {
      loadingOverlay.classList.add('hidden');
      btnRunOcr.disabled = false;
    }
  }

  btnRunOcr.addEventListener('click', runOcr);

  // 5. Render OCR Results
  function renderOcrResults(data, totalLatency) {
    // Top performance indicators
    durationVal.textContent = data.durationMs;
    const lines = Array.isArray(data.lines) ? data.lines : [];
    linesCountVal.textContent = lines.length;
    tabLinesBadge.textContent = lines.length;

    if (lines.length > 0) {
      const avgScore = lines.reduce((acc, cur) => acc + (cur.score || 0), 0) / lines.length;
      confidenceVal.textContent = (avgScore * 100).toFixed(1) + '%';
    } else {
      confidenceVal.textContent = '0%';
    }

    // Text Tab
    outputText.value = data.text || '（未识别到文本）';

    // Lines Tab
    linesList.innerHTML = '';
    lines.forEach((line, index) => {
      const scorePct = Math.round((line.score || 0) * 100);
      const card = document.createElement('div');
      card.className = 'line-item-card';
      card.id = `line-card-${index}`;
      card.innerHTML = `
        <div class="line-card-header">
          <span class="line-badge font-mono">#${index + 1}</span>
          <span class="line-score-badge font-mono" style="color: ${scorePct > 85 ? '#10b981' : '#818cf8'}">置信度: ${scorePct}%</span>
        </div>
        <div class="line-text-content">${escapeHtml(line.text)}</div>
      `;

      card.addEventListener('mouseenter', () => {
        hoveredLineIndex = index;
        card.classList.add('hovered');
        drawBoundingBoxes();
      });

      card.addEventListener('mouseleave', () => {
        hoveredLineIndex = -1;
        card.classList.remove('hovered');
        drawBoundingBoxes();
      });

      linesList.appendChild(card);
    });

    // RESTful JSON Tab
    outputJson.textContent = JSON.stringify({
      code: 200,
      status: 'success',
      data: data
    }, null, 2);

    // Render Canvas Boxes
    drawBoundingBoxes();
  }

  // 6. Canvas Bounding Box Rendering
  function clearCanvas() {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
  }

  function resizeCanvas() {
    if (!previewImage.naturalWidth) return;
    const rect = previewImage.getBoundingClientRect();
    canvas.width = rect.width;
    canvas.height = rect.height;
    canvas.style.width = `${rect.width}px`;
    canvas.style.height = `${rect.height}px`;
    drawBoundingBoxes();
  }

  window.addEventListener('resize', resizeCanvas);

  function drawBoundingBoxes() {
    if (!ocrResultData || !ocrResultData.lines || !ocrResultData.lines.length) {
      clearCanvas();
      return;
    }

    const rect = previewImage.getBoundingClientRect();
    if (canvas.width !== rect.width || canvas.height !== rect.height) {
      canvas.width = rect.width;
      canvas.height = rect.height;
    }

    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const scaleX = rect.width / previewImage.naturalWidth;
    const scaleY = rect.height / previewImage.naturalHeight;

    ocrResultData.lines.forEach((line, idx) => {
      const box = line.box;
      if (!box || box.length < 4) return;

      const isHovered = (idx === hoveredLineIndex);
      const isHighConf = (line.score || 0) >= 0.85;

      ctx.beginPath();
      ctx.moveTo(box[0][0] * scaleX, box[0][1] * scaleY);
      for (let i = 1; i < box.length; i++) {
        ctx.lineTo(box[i][0] * scaleX, box[i][1] * scaleY);
      }
      ctx.closePath();

      if (isHovered) {
        ctx.lineWidth = 3;
        ctx.strokeStyle = '#f43f5e';
        ctx.fillStyle = 'rgba(244, 63, 94, 0.25)';
      } else {
        ctx.lineWidth = 1.8;
        ctx.strokeStyle = isHighConf ? 'rgba(16, 185, 129, 0.9)' : 'rgba(99, 102, 241, 0.85)';
        ctx.fillStyle = isHighConf ? 'rgba(16, 185, 129, 0.12)' : 'rgba(99, 102, 241, 0.1)';
      }

      ctx.fill();
      ctx.stroke();
    });
  }

  // Canvas Mouse Move & Tooltip
  canvas.addEventListener('mousemove', (e) => {
    if (!ocrResultData || !ocrResultData.lines) return;

    const rect = canvas.getBoundingClientRect();
    const mouseX = e.clientX - rect.left;
    const mouseY = e.clientY - rect.top;

    const scaleX = rect.width / previewImage.naturalWidth;
    const scaleY = rect.height / previewImage.naturalHeight;

    let hitIndex = -1;
    for (let i = 0; i < ocrResultData.lines.length; i++) {
      const box = ocrResultData.lines[i].box;
      if (!box || box.length < 4) continue;
      const scaledPoints = box.map(pt => [pt[0] * scaleX, pt[1] * scaleY]);
      if (pointInPolygon([mouseX, mouseY], scaledPoints)) {
        hitIndex = i;
        break;
      }
    }

    if (hitIndex !== hoveredLineIndex) {
      hoveredLineIndex = hitIndex;
      drawBoundingBoxes();

      // Highlight in list
      document.querySelectorAll('.line-item-card').forEach(c => c.classList.remove('hovered'));
      if (hitIndex !== -1) {
        const card = document.getElementById(`line-card-${hitIndex}`);
        if (card) {
          card.classList.add('hovered');
          card.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }
    }

    if (hitIndex !== -1) {
      const line = ocrResultData.lines[hitIndex];
      tooltip.textContent = `[${Math.round((line.score || 0) * 100)}%] ${line.text}`;
      tooltip.style.left = `${mouseX + 12}px`;
      tooltip.style.top = `${mouseY + 12}px`;
      tooltip.classList.remove('hidden');
    } else {
      tooltip.classList.add('hidden');
    }
  });

  canvas.addEventListener('mouseleave', () => {
    hoveredLineIndex = -1;
    tooltip.classList.add('hidden');
    document.querySelectorAll('.line-item-card').forEach(c => c.classList.remove('hovered'));
    drawBoundingBoxes();
  });

  // Point in polygon test
  function pointInPolygon(point, vs) {
    const x = point[0], y = point[1];
    let inside = false;
    for (let i = 0, j = vs.length - 1; i < vs.length; j = i++) {
      const xi = vs[i][0], yi = vs[i][1];
      const xj = vs[j][0], yj = vs[j][1];
      const intersect = ((yi > y) !== (yj > y)) && (x < (xj - xi) * (y - yi) / (yj - yi) + xi);
      if (intersect) inside = !inside;
    }
    return inside;
  }

  // 7. Copy Text
  btnCopyResult.addEventListener('click', async () => {
    const text = outputText.value;
    if (!text || !text.trim()) {
      showToast('没有可复制的文本', 'error');
      return;
    }

    try {
      await navigator.clipboard.writeText(text);
      copyBtnText.textContent = '已复制!';
      btnCopyResult.style.borderColor = '#10b981';
      showToast('全文已成功复制到剪贴板', 'success');
      setTimeout(() => {
        copyBtnText.textContent = '一键复制';
        btnCopyResult.style.borderColor = '';
      }, 2000);
    } catch (e) {
      showToast('复制失败，请手动选择复制', 'error');
    }
  });

  // Helper Toast
  function showToast(message, type = 'info') {
    const container = document.getElementById('toast-container');
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.innerHTML = `
      <span>${type === 'success' ? '✔' : '⚠'}</span>
      <span>${escapeHtml(message)}</span>
    `;
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      setTimeout(() => toast.remove(), 300);
    }, 3000);
  }

  function escapeHtml(str) {
    if (!str) return '';
    return str.replace(/&/g, '&amp;')
              .replace(/</g, '&lt;')
              .replace(/>/g, '&gt;')
              .replace(/"/g, '&quot;');
  }
});
