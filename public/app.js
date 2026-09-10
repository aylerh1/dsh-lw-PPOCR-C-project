/**
 * Frontend Interactive Controller for PP-OCR Web (Single-Image Mode)
 * Features:
 * 1. Pixel-perfect 1:1 Bounding Box Canvas Annotation
 * 2. Drag & Drop, File Picker & Clipboard Paste for Single Image
 * 3. High-Res Boxed Image Export
 * 4. Ultra-Low Resident Memory (~10MB) Architecture Support
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
  const btnDownloadSingle = document.getElementById('btn-download-single');
  const btnChangeImage = document.getElementById('btn-change-image');
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

  // Internal Single Image State
  let currentFile = null; // { name, dataUrl }
  let currentResult = null;
  let hoveredLineIndex = -1;

  // 1. Health Check
  async function checkHealth() {
    try {
      const res = await fetch('/api/v1/health');
      if (res.ok) {
        const data = await res.json();
        if (data.data && data.data.engine === 'ready') {
          healthIndicator.className = 'status-indicator ready';
          healthText.textContent = '引擎就绪 (常驻~10M)';
        }
      }
    } catch (e) {
      healthIndicator.className = 'status-indicator';
      healthText.textContent = '服务连接中...';
    }
  }
  checkHealth();
  setInterval(checkHealth, 10000);

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

  // 3. Single Image Loading
  function loadSingleFile(file) {
    if (!file || !file.type.startsWith('image/')) {
      showToast('请选择有效的图片文件 (支持 PNG, JPG, BMP)', 'error');
      return;
    }

    const reader = new FileReader();
    reader.onload = (e) => {
      currentFile = {
        name: file.name || 'image.png',
        dataUrl: e.target.result
      };
      currentResult = null;
      renderImagePreview();
      showToast(`已加载图片: ${currentFile.name}`, 'success');
      // Automatically trigger OCR recognition
      runOcr();
    };
    reader.readAsDataURL(file);
  }

  function renderImagePreview() {
    if (!currentFile) {
      dropZone.classList.remove('hidden');
      previewContainer.classList.add('hidden');
      btnRunOcr.disabled = true;
      btnDownloadSingle.disabled = true;
      clearCanvas();
      resetMetricsDisplay();
      return;
    }

    dropZone.classList.add('hidden');
    previewContainer.classList.remove('hidden');
    btnRunOcr.disabled = false;
    btnDownloadSingle.disabled = true;

    previewImage.src = currentFile.dataUrl;
    previewImage.onload = () => {
      syncCanvasResolution();
      clearCanvas();
      resetMetricsDisplay();
    };
  }

  function resetMetricsDisplay() {
    durationVal.textContent = '--';
    linesCountVal.textContent = '0';
    roundtripVal.textContent = '--';
    confidenceVal.textContent = '--%';
    outputText.value = '';
    linesList.innerHTML = '';
    tabLinesBadge.textContent = '0';
    outputJson.textContent = '// 等待识别生成 RESTful 结构化数据...';
    resultEmpty.classList.remove('hidden');
    loadingOverlay.classList.add('hidden');
  }

  // 4. OCR Execution
  async function runOcr() {
    if (!currentFile) {
      showToast('请先选择或拖入图片', 'error');
      return;
    }

    btnRunOcr.disabled = true;
    loadingOverlay.classList.remove('hidden');
    resultEmpty.classList.add('hidden');

    const startTime = performance.now();

    try {
      const payload = {
        image: currentFile.dataUrl,
        det: checkDet.checked,
        cls: checkCls.checked,
        rec: checkRec.checked,
        confidenceThreshold: 0.3
      };

      const response = await fetch('/api/v1/ocr/recognitions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      const totalLatency = Math.round(performance.now() - startTime);

      if (!response.ok) {
        const errJson = await response.json().catch(() => ({}));
        throw new Error(errJson.message || `HTTP ${response.status}`);
      }

      const resData = await response.json();
      if (resData.status !== 'success' || !resData.data) {
        throw new Error(resData.message || '数据格式异常');
      }

      currentResult = resData.data;
      renderOcrResults(currentResult, totalLatency);
      btnDownloadSingle.disabled = false;
      showToast(`识别成功！共提取 ${currentResult.lines ? currentResult.lines.length : 0} 行文本`, 'success');
    } catch (err) {
      console.error('OCR recognition failed:', err);
      showToast(`识别失败: ${err.message}`, 'error');
      outputJson.textContent = `// 识别失败: ${err.message}`;
    } finally {
      btnRunOcr.disabled = false;
      loadingOverlay.classList.add('hidden');
    }
  }

  btnRunOcr.addEventListener('click', runOcr);

  // 5. Pixel-Perfect 1:1 Canvas Bounding Box Rendering
  function clearCanvas() {
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
  }

  function syncCanvasResolution() {
    if (!previewImage.naturalWidth) return;
    canvas.width = previewImage.naturalWidth;
    canvas.height = previewImage.naturalHeight;
    drawBoundingBoxes();
  }

  window.addEventListener('resize', () => {
    drawBoundingBoxes();
  });

  function drawBoundingBoxes() {
    if (!currentResult || !currentResult.lines || !currentResult.lines.length) {
      clearCanvas();
      return;
    }

    if (canvas.width !== previewImage.naturalWidth || canvas.height !== previewImage.naturalHeight) {
      canvas.width = previewImage.naturalWidth;
      canvas.height = previewImage.naturalHeight;
    }

    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const baseLineWidth = Math.max(2, Math.round(canvas.width / 500));

    currentResult.lines.forEach((line, idx) => {
      const box = line.box;
      if (!box || box.length < 4) return;

      const isHovered = (idx === hoveredLineIndex);
      const isHighConf = (line.score || 0) >= 0.85;

      ctx.beginPath();
      ctx.moveTo(box[0][0], box[0][1]);
      for (let i = 1; i < box.length; i++) {
        ctx.lineTo(box[i][0], box[i][1]);
      }
      ctx.closePath();

      if (isHovered) {
        ctx.lineWidth = baseLineWidth + 2;
        ctx.strokeStyle = '#f43f5e';
        ctx.fillStyle = 'rgba(244, 63, 94, 0.35)';
      } else {
        ctx.lineWidth = baseLineWidth;
        ctx.strokeStyle = isHighConf ? 'rgba(16, 185, 129, 0.95)' : 'rgba(99, 102, 241, 0.9)';
        ctx.fillStyle = isHighConf ? 'rgba(16, 185, 129, 0.15)' : 'rgba(99, 102, 241, 0.12)';
      }

      ctx.fill();
      ctx.stroke();
    });
  }

  // Hover detection with physical 1:1 pixel coordinate transform
  canvas.addEventListener('mousemove', (e) => {
    if (!currentResult || !currentResult.lines) return;

    const rect = canvas.getBoundingClientRect();
    const mouseX = (e.clientX - rect.left) * (canvas.width / rect.width);
    const mouseY = (e.clientY - rect.top) * (canvas.height / rect.height);

    let hitIndex = -1;
    for (let i = 0; i < currentResult.lines.length; i++) {
      const box = currentResult.lines[i].box;
      if (!box || box.length < 4) continue;
      if (pointInPolygon([mouseX, mouseY], box)) {
        hitIndex = i;
        break;
      }
    }

    if (hitIndex !== hoveredLineIndex) {
      hoveredLineIndex = hitIndex;
      drawBoundingBoxes();

      if (hitIndex !== -1) {
        const line = currentResult.lines[hitIndex];
        const scorePct = Math.round((line.score || 0) * 100);
        tooltip.innerHTML = `<strong>#${hitIndex + 1} (${scorePct}%)</strong><br>${escapeHtml(line.text)}`;
        tooltip.classList.remove('hidden');

        const stageRect = document.getElementById('stage-wrapper').getBoundingClientRect();
        const tooltipX = Math.min(stageRect.width - 240, Math.max(10, e.clientX - stageRect.left + 15));
        const tooltipY = Math.min(stageRect.height - 80, Math.max(10, e.clientY - stageRect.top + 15));
        tooltip.style.left = `${tooltipX}px`;
        tooltip.style.top = `${tooltipY}px`;

        const card = document.getElementById(`line-card-${hitIndex}`);
        if (card) {
          card.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
          document.querySelectorAll('.line-item-card').forEach(c => c.classList.remove('hovered'));
          card.classList.add('hovered');
        }
      } else {
        tooltip.classList.add('hidden');
        document.querySelectorAll('.line-item-card').forEach(c => c.classList.remove('hovered'));
      }
    }
  });

  canvas.addEventListener('mouseleave', () => {
    hoveredLineIndex = -1;
    drawBoundingBoxes();
    tooltip.classList.add('hidden');
    document.querySelectorAll('.line-item-card').forEach(c => c.classList.remove('hovered'));
  });

  // 6. Render OCR Results Panel
  function renderOcrResults(data, totalLatency) {
    resultEmpty.classList.add('hidden');
    durationVal.textContent = data.durationMs || 0;
    roundtripVal.textContent = totalLatency || '--';

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

    // Draw Canvas Bounding Boxes
    drawBoundingBoxes();
  }

  // 7. High-Res Boxed Image Single Download
  async function downloadCurrentBoxedImage() {
    if (!currentFile) {
      showToast('未选择图片', 'error');
      return;
    }

    try {
      showToast('正在合成高清带框图片...', 'success');
      const img = new Image();
      img.onload = () => {
        const offscreen = document.createElement('canvas');
        offscreen.width = img.naturalWidth;
        offscreen.height = img.naturalHeight;
        const ctx = offscreen.getContext('2d');

        // Draw original image
        ctx.drawImage(img, 0, 0);

        // Draw boxes
        if (currentResult && currentResult.lines) {
          const baseLineWidth = Math.max(2, Math.round(offscreen.width / 500));
          const fontSize = Math.max(12, Math.round(offscreen.width / 80));
          ctx.font = `600 ${fontSize}px sans-serif`;

          currentResult.lines.forEach((line, idx) => {
            const box = line.box;
            if (!box || box.length < 4) return;
            const isHighConf = (line.score || 0) >= 0.85;

            ctx.beginPath();
            ctx.moveTo(box[0][0], box[0][1]);
            for (let i = 1; i < box.length; i++) {
              ctx.lineTo(box[i][0], box[i][1]);
            }
            ctx.closePath();

            ctx.lineWidth = baseLineWidth;
            ctx.strokeStyle = isHighConf ? 'rgba(16, 185, 129, 0.95)' : 'rgba(99, 102, 241, 0.95)';
            ctx.fillStyle = isHighConf ? 'rgba(16, 185, 129, 0.2)' : 'rgba(99, 102, 241, 0.2)';
            ctx.fill();
            ctx.stroke();

            // Label tag on top of box
            const tagX = box[0][0];
            const tagY = Math.max(fontSize + 4, box[0][1] - 4);
            ctx.fillStyle = 'rgba(0, 0, 0, 0.75)';
            ctx.fillRect(tagX, tagY - fontSize - 2, fontSize * 2 + 8, fontSize + 4);
            ctx.fillStyle = isHighConf ? '#34d399' : '#a5b4fc';
            ctx.fillText(`#${idx + 1}`, tagX + 4, tagY - 2);
          });
        }

        offscreen.toBlob((blob) => {
          if (!blob) {
            showToast('生成图片失败', 'error');
            return;
          }
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          const baseName = currentFile.name.replace(/\.[^/.]+$/, '');
          a.download = `${baseName}_ocr_boxed.png`;
          a.href = url;
          document.body.appendChild(a);
          a.click();
          setTimeout(() => {
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
          }, 1000);
          showToast(`已成功下载: ${a.download}`, 'success');
        }, 'image/png');
      };
      img.src = currentFile.dataUrl;
    } catch (err) {
      showToast(`导出图片失败: ${err.message}`, 'error');
    }
  }

  btnDownloadSingle.addEventListener('click', downloadCurrentBoxedImage);

  // 8. File Picker & Drag-and-Drop Listeners
  dropZone.addEventListener('click', () => fileInput.click());
  btnChangeImage.addEventListener('click', () => fileInput.click());

  fileInput.addEventListener('change', (e) => {
    if (e.target.files && e.target.files.length) {
      loadSingleFile(e.target.files[0]);
      fileInput.value = '';
    }
  });

  // Global Drag & Drop
  ['dragenter', 'dragover'].forEach(eventName => {
    window.addEventListener(eventName, (e) => {
      e.preventDefault();
      dropZone.classList.add('dragover');
    });
  });

  ['dragleave', 'drop'].forEach(eventName => {
    window.addEventListener(eventName, (e) => {
      e.preventDefault();
      dropZone.classList.remove('dragover');
    });
  });

  window.addEventListener('drop', (e) => {
    if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length) {
      loadSingleFile(e.dataTransfer.files[0]);
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
          loadSingleFile(file);
          break;
        }
      }
    }
  });

  // Reset
  function resetAll() {
    currentFile = null;
    currentResult = null;
    hoveredLineIndex = -1;
    fileInput.value = '';
    renderImagePreview();
    showToast('已清空当前图片与识别记录', 'info');
  }

  btnReset.addEventListener('click', resetAll);

  // Load Built-in Sample Image
  btnLoadSample.addEventListener('click', async () => {
    try {
      showToast('正在获取内置样例图...', 'info');
      const res = await fetch('/api/v1/sample');
      if (!res.ok) throw new Error('获取样例图片失败');
      const json = await res.json();
      if (json.data && json.data.image) {
        currentFile = {
          name: json.data.filename || 'sample.png',
          dataUrl: json.data.image
        };
        currentResult = null;
        renderImagePreview();
        showToast('已成功加载系统样例图！', 'success');
        runOcr();
      }
    } catch (err) {
      showToast(err.message, 'error');
    }
  });

  // Copy Result Text
  btnCopyResult.addEventListener('click', async () => {
    const text = outputText.value;
    if (!text) {
      showToast('暂无识别结果可复制', 'error');
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      copyBtnText.textContent = '已复制！';
      showToast('已成功复制文本到剪贴板', 'success');
      setTimeout(() => copyBtnText.textContent = '一键复制', 2000);
    } catch (err) {
      showToast('复制失败，请手动选择复制', 'error');
    }
  });

  // Utility Functions
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

  function escapeHtml(str) {
    if (!str) return '';
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function showToast(message, type = 'info') {
    const container = document.getElementById('toast-container');
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;

    let iconSvg = '';
    if (type === 'success') {
      iconSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2.2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>';
    } else if (type === 'error') {
      iconSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#f43f5e" stroke-width="2.2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>';
    } else {
      iconSvg = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#818cf8" stroke-width="2.2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>';
    }

    toast.innerHTML = `
      <div class="toast-icon">${iconSvg}</div>
      <span class="toast-text">${escapeHtml(message)}</span>
    `;

    container.appendChild(toast);
    setTimeout(() => toast.classList.add('show'), 10);
    setTimeout(() => {
      toast.classList.remove('show');
      setTimeout(() => toast.remove(), 300);
    }, 3200);
  }
});
