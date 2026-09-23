/**
 * Frontend Interactive Controller for PP-OCR Web & Multi-Modal Document Pipeline
 * Features:
 * 1. Pixel-perfect 1:1 Bounding Box Canvas Annotation (Color-coded by Layout Region)
 * 2. Multi-Modal Pipeline: PP-DocLayout-S Routing + SLANet Table + RapidLaTeX Formula + lw.PPOCR.C Native
 * 3. Drag & Drop, File Picker & Clipboard Paste for Single Image
 * 4. High-Res Boxed Image Export
 * 5. Ultra-Low Resident Memory (~30MB) Architecture Support
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
  const btnOcrText = document.getElementById('btn-ocr-text');
  const btnReset = document.getElementById('btn-reset');
  const btnLoadSample = document.getElementById('btn-load-sample');
  const btnDownloadSingle = document.getElementById('btn-download-single');
  const btnChangeImage = document.getElementById('btn-change-image');
  const btnCopyResult = document.getElementById('btn-copy-result');
  const copyBtnText = document.getElementById('copy-btn-text');
  const btnExportZip = document.getElementById('btn-export-zip');
  const exportZipText = document.getElementById('export-zip-text');

  // Mode Selection
  const modeOcrBtn = document.getElementById('mode-ocr');
  const modeDocBtn = document.getElementById('mode-doc');
  let currentMode = 'doc'; // 'ocr' or 'doc', default 'doc'

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
  const loadingMsg = document.getElementById('loading-msg');
  const outputText = document.getElementById('output-text');
  const markdownPreview = document.getElementById('markdown-preview');
  const layoutRegionsList = document.getElementById('layout-regions-list');
  const linesList = document.getElementById('lines-list');
  const outputJson = document.getElementById('output-json');
  const tabLinesBadge = document.getElementById('tab-lines-badge');
  const tabLayoutBadge = document.getElementById('tab-layout-badge');

  // Internal State
  let currentFile = null; // { name, dataUrl }
  let currentResult = null;
  let hoveredLineIndex = -1;
  let hoveredRegionIndex = -1;

  // 1. Health Check
  async function checkHealth() {
    try {
      const res = await fetch('/api/v1/health');
      if (res.ok) {
        const data = await res.json();
        if (data.status === 'success') {
          healthIndicator.className = 'status-indicator ready';
          healthText.textContent = '引擎就绪 (Native C/AVX2)';
        }
      }
    } catch (e) {
      healthIndicator.className = 'status-indicator';
      healthText.textContent = '服务连接中...';
    }
  }
  checkHealth();
  setInterval(checkHealth, 10000);

  // Mode Switching
  if (modeOcrBtn && modeDocBtn) {
    modeOcrBtn.addEventListener('click', () => {
      currentMode = 'ocr';
      modeOcrBtn.classList.add('active');
      modeDocBtn.classList.remove('active');
      btnOcrText.textContent = '开始极速识别';
    });

    modeDocBtn.addEventListener('click', () => {
      currentMode = 'doc';
      modeDocBtn.classList.add('active');
      modeOcrBtn.classList.remove('active');
      btnOcrText.textContent = '开始深度解析';
    });
  }

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

  // 3. AnyDoc Multi-Format File Loading
  function loadSingleFile(file) {
    if (!file) return;

    const lowerName = (file.name || '').toLowerCase();
    const isImage = file.type.startsWith('image/') || /\.(png|jpg|jpeg|bmp|webp|tiff|gif|ico|svg)$/i.test(lowerName);
    const isPDF = file.type === 'application/pdf' || lowerName.endsWith('.pdf');
    const isExcel = /\.(xlsx|xls|csv|tsv)$/i.test(lowerName) || file.type.includes('spreadsheet') || file.type.includes('excel') || file.type === 'text/csv';
    const isPPT = /\.(pptx|ppt)$/i.test(lowerName) || file.type.includes('presentation') || file.type.includes('powerpoint');
    const isWord = /\.(docx|doc)$/i.test(lowerName) || file.type.includes('wordprocessing') || file.type.includes('msword');
    const isData = /\.(json|xml|yaml|yml)$/i.test(lowerName) || file.type === 'application/json' || file.type === 'text/xml';
    const isText = /\.(txt|md|markdown|html|htm|xhtml|rtf|log|ini|conf)$/i.test(lowerName) || file.type.startsWith('text/');

    const reader = new FileReader();
    reader.onload = (e) => {
      let previewUrl = e.target.result;

      if (!isImage) {
        let badgeColor = '#3b82f6';
        let badgeText = 'DOC';
        let subText = 'AnyDoc 结构化直接转换 (0ms 延迟)';

        if (isPDF) {
          badgeColor = '#ef4444';
          badgeText = 'PDF';
          subText = 'AnyDoc 智能双轨探测 (纯矢量免OCR / 扫描件多模态)';
        } else if (isExcel) {
          badgeColor = '#10b981';
          badgeText = lowerName.endsWith('.csv') ? 'CSV' : 'XLSX';
          subText = 'AnyDoc 电子表格转换 (提取为 Markdown 与 HTML 表格)';
        } else if (isPPT) {
          badgeColor = '#f97316';
          badgeText = 'PPTX';
          subText = 'AnyDoc 幻灯片逐页解析 (提取为结构化章节)';
        } else if (isWord) {
          badgeColor = '#2563eb';
          badgeText = 'DOCX';
          subText = 'AnyDoc 文档结构化转换 (提取标题、正文与表格)';
        } else if (isData) {
          badgeColor = '#06b6d4';
          badgeText = lowerName.endsWith('.json') ? 'JSON' : 'DATA';
          subText = 'AnyDoc 结构化数据解析 (格式化缩进与代码呈现)';
        } else if (isText) {
          badgeColor = '#8b5cf6';
          badgeText = lowerName.endsWith('.md') ? 'MD' : (lowerName.endsWith('.html') ? 'HTML' : 'TEXT');
          subText = 'AnyDoc 纯文本与富文本排版重构';
        } else {
          badgeColor = '#64748b';
          const extPart = lowerName.includes('.') ? lowerName.split('.').pop().toUpperCase() : 'FILE';
          badgeText = extPart.length > 5 ? extPart.slice(0, 4) : extPart;
          subText = 'AnyDoc 智能多类型文档解析';
        }

        const fileSizeStr = (file.size / 1024).toFixed(1);
        const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="1100" viewBox="0 0 800 1100">
          <rect width="800" height="1100" fill="#0f172a"/>
          <rect x="100" y="80" width="600" height="940" rx="16" fill="#1e293b" stroke="#334155" stroke-width="2"/>
          <circle cx="400" cy="380" r="76" fill="${badgeColor}" fill-opacity="0.12" stroke="${badgeColor}" stroke-width="3"/>
          <text x="400" y="405" font-family="Outfit, -apple-system, BlinkMacSystemFont, sans-serif" font-size="48" fill="${badgeColor}" text-anchor="middle" font-weight="700">${badgeText}</text>
          <text x="400" y="520" font-family="Outfit, -apple-system, BlinkMacSystemFont, sans-serif" font-size="22" fill="#f8fafc" text-anchor="middle" font-weight="600">${escapeHtml(file.name || 'document')}</text>
          <text x="400" y="560" font-family="Outfit, -apple-system, BlinkMacSystemFont, sans-serif" font-size="16" fill="#94a3b8" text-anchor="middle">${subText}</text>
          <text x="400" y="600" font-family="Outfit, -apple-system, BlinkMacSystemFont, sans-serif" font-size="14" fill="#64748b" text-anchor="middle">文件大小: ${fileSizeStr} KB · 点击下方“开始深度解析”执行极速转换</text>
        </svg>`;
        previewUrl = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
      }

      currentFile = {
        name: file.name || 'document',
        dataUrl: e.target.result,
        previewUrl: previewUrl,
        isPDF: isPDF,
        isImage: isImage
      };
      currentResult = null;
      renderImagePreview();
      resetMetricsDisplay();
    };
    reader.readAsDataURL(file);
  }

  function renderImagePreview() {
    if (!currentFile) return;
    previewImage.src = currentFile.previewUrl || currentFile.dataUrl;
    dropZone.classList.add('hidden');
    previewContainer.classList.remove('hidden');
    btnRunOcr.disabled = false;
    btnDownloadSingle.disabled = true;

    previewImage.onload = () => {
      syncCanvasResolution();
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
    if (tabLayoutBadge) tabLayoutBadge.textContent = '0';
    if (layoutRegionsList) layoutRegionsList.innerHTML = '';
    if (markdownPreview) {
      markdownPreview.innerHTML = '<div class="markdown-empty-hint">等待解析完成...</div>';
    }
    outputJson.textContent = '// 等待识别生成 RESTful 结构化数据...';
    resultEmpty.classList.remove('hidden');
    loadingOverlay.classList.add('hidden');
    if (btnExportZip) btnExportZip.disabled = true;
  }

  // 4. OCR & Multi-Modal Document Execution
  async function runOcr() {
    if (!currentFile) {
      showToast('请先选择或拖入图片', 'error');
      return;
    }

    btnRunOcr.disabled = true;
    loadingOverlay.classList.remove('hidden');
    resultEmpty.classList.add('hidden');

    if (loadingMsg) {
      loadingMsg.textContent = currentMode === 'doc'
        ? 'PP-DocLayout-S 版面路由与多流解析中...'
        : 'Native C11 / AVX2 推理计算中...';
    }

    const startTime = performance.now();

    try {
      const payload = {
        image: currentFile.dataUrl,
        filename: currentFile.name,
        det: checkDet.checked,
        cls: checkCls.checked,
        rec: checkRec.checked,
        confidenceThreshold: 0.3
      };

      const endpoint = currentMode === 'doc' ? '/api/v1/document' : '/api/v1/ocr';
      const response = await fetch(endpoint, {
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
      if (btnExportZip) btnExportZip.disabled = false;
      showToast(currentMode === 'doc' ? '文档多模态深度解析完成！' : '极速文字识别完成！', 'success');
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

  // Get color configuration based on layout category
  function getCategoryColor(label) {
    switch (label) {
      case 'title':
        return { stroke: 'rgba(56, 189, 248, 0.95)', fill: 'rgba(56, 189, 248, 0.18)' };
      case 'table':
        return { stroke: 'rgba(52, 211, 153, 0.95)', fill: 'rgba(52, 211, 153, 0.18)' };
      case 'formula':
        return { stroke: 'rgba(251, 191, 36, 0.95)', fill: 'rgba(251, 191, 36, 0.18)' };
      case 'figure':
        return { stroke: 'rgba(192, 132, 252, 0.95)', fill: 'rgba(192, 132, 252, 0.18)' };
      case 'header':
      case 'footer':
        return { stroke: 'rgba(148, 163, 184, 0.95)', fill: 'rgba(148, 163, 184, 0.15)' };
      default: // 'text'
        return { stroke: 'rgba(129, 140, 248, 0.95)', fill: 'rgba(129, 140, 248, 0.15)' };
    }
  }

  function drawBoundingBoxes() {
    if (!currentResult) {
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

    // Priority 1: When layout regions exist, ONLY display layout recognition categories on original content!
    if (currentResult.regions && currentResult.regions.length) {
      currentResult.regions.forEach((reg, idx) => {
        const box = reg.box;
        if (!box || box.length < 4) return;

        const isHovered = (idx === hoveredRegionIndex);
        const col = getCategoryColor(reg.label);

        ctx.beginPath();
        ctx.moveTo(box[0][0], box[0][1]);
        for (let i = 1; i < box.length; i++) {
          ctx.lineTo(box[i][0], box[i][1]);
        }
        ctx.closePath();

        if (isHovered) {
          ctx.lineWidth = baseLineWidth + 2;
          ctx.strokeStyle = '#f43f5e';
          ctx.fillStyle = 'rgba(244, 63, 94, 0.22)';
        } else {
          ctx.lineWidth = Math.max(2, baseLineWidth);
          ctx.strokeStyle = col.stroke;
          ctx.fillStyle = col.fill;
        }

        ctx.fill();
        ctx.stroke();

        // Draw region label tag (FIGURE, TEXT, HEADER, TABLE, FOOTER)
        const tagX = box[0][0];
        const tagY = Math.max(20, box[0][1] - 4);
        const labelText = reg.label.toUpperCase();
        const badgeFontSz = Math.max(12, Math.round(canvas.width / 52));
        ctx.font = `bold ${badgeFontSz}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
        const tw = ctx.measureText(labelText).width;

        ctx.fillStyle = isHovered ? '#f43f5e' : (col.stroke || 'rgba(15, 23, 42, 0.9)');
        ctx.fillRect(tagX, tagY - badgeFontSz - 5, tw + 14, badgeFontSz + 7);
        ctx.fillStyle = '#ffffff';
        ctx.fillText(labelText, tagX + 7, tagY - 2);
      });
      return;
    }

    // Priority 2: Fallback to simple line boxes only in pure OCR mode without layout regions
    if (currentResult.lines && currentResult.lines.length) {
      currentResult.lines.forEach((line, idx) => {
        const box = line.box;
        if (!box || box.length < 4) return;

        const isHovered = (idx === hoveredLineIndex);
        ctx.beginPath();
        ctx.moveTo(box[0][0], box[0][1]);
        for (let i = 1; i < box.length; i++) {
          ctx.lineTo(box[i][0], box[i][1]);
        }
        ctx.closePath();

        ctx.lineWidth = isHovered ? baseLineWidth + 2 : Math.max(1, baseLineWidth - 1);
        ctx.strokeStyle = isHovered ? '#38bdf8' : 'rgba(16, 185, 129, 0.9)';
        ctx.fillStyle = isHovered ? 'rgba(56, 189, 248, 0.3)' : 'rgba(16, 185, 129, 0.1)';
        ctx.fill();
        ctx.stroke();
      });
    }
  }

  // Track hover state for tooltip and box highlight
  canvas.addEventListener('mousemove', (e) => {
    if (!currentResult) return;

    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;

    const mouseX = (e.clientX - rect.left) * scaleX;
    const mouseY = (e.clientY - rect.top) * scaleY;

    // 1. If layout regions exist, track layout regions
    if (currentResult.regions && currentResult.regions.length) {
      let hitRegion = -1;
      for (let i = 0; i < currentResult.regions.length; i++) {
        const box = currentResult.regions[i].box;
        if (!box || box.length < 4) continue;
        if (pointInPolygon([mouseX, mouseY], box)) {
          hitRegion = i;
          break;
        }
      }

      if (hitRegion !== hoveredRegionIndex) {
        hoveredRegionIndex = hitRegion;
        drawBoundingBoxes();

        const stageRect = document.getElementById('stage-wrapper').getBoundingClientRect();
        const tooltipX = Math.min(stageRect.width - 240, Math.max(10, e.clientX - stageRect.left + 15));
        const tooltipY = Math.min(stageRect.height - 80, Math.max(10, e.clientY - stageRect.top + 15));
        tooltip.style.left = `${tooltipX}px`;
        tooltip.style.top = `${tooltipY}px`;

        if (hitRegion !== -1) {
          const reg = currentResult.regions[hitRegion];
          tooltip.innerHTML = `<strong>#${reg.id} [${reg.label.toUpperCase()}]</strong><br>版面类别: ${reg.label}<br>置信度: ${Math.round(reg.score * 100)}%`;
          tooltip.classList.remove('hidden');
        } else {
          tooltip.classList.add('hidden');
        }
      }
      return;
    }

    // 2. Pure OCR fallback
    let hitLine = -1;
    if (currentResult.lines && currentResult.lines.length) {
      for (let i = 0; i < currentResult.lines.length; i++) {
        const box = currentResult.lines[i].box;
        if (!box || box.length < 4) continue;
        if (pointInPolygon([mouseX, mouseY], box)) {
          hitLine = i;
          break;
        }
      }
    }

    if (hitLine !== hoveredLineIndex) {
      hoveredLineIndex = hitLine;
      drawBoundingBoxes();

      const stageRect = document.getElementById('stage-wrapper').getBoundingClientRect();
      const tooltipX = Math.min(stageRect.width - 240, Math.max(10, e.clientX - stageRect.left + 15));
      const tooltipY = Math.min(stageRect.height - 80, Math.max(10, e.clientY - stageRect.top + 15));
      tooltip.style.left = `${tooltipX}px`;
      tooltip.style.top = `${tooltipY}px`;

      if (hitLine !== -1) {
        const line = currentResult.lines[hitLine];
        const scorePct = Math.round((line.score || 0) * 100);
        tooltip.innerHTML = `<strong>#${hitLine + 1} (${scorePct}%)</strong><br>${escapeHtml(line.text)}`;
        tooltip.classList.remove('hidden');
      } else {
        tooltip.classList.add('hidden');
      }
    }
  });

  canvas.addEventListener('mouseleave', () => {
    hoveredLineIndex = -1;
    hoveredRegionIndex = -1;
    drawBoundingBoxes();
    tooltip.classList.add('hidden');
  });

  // 6. Result Rendering Logic
  function renderOcrResults(data, totalLatency) {
    resultEmpty.classList.add('hidden');

    const duration = data.totalDurationMs || data.durationMs || 0;
    durationVal.textContent = duration;
    roundtripVal.textContent = totalLatency;

    // Render Inspector Banner if available
    const inspBanner = document.getElementById('inspector-banner');
    const inspBadge = document.getElementById('inspector-badge');
    const inspSummary = document.getElementById('inspector-summary');
    if (inspBanner && inspBadge && inspSummary) {
      if (data.inspector) {
        inspBanner.classList.remove('hidden');
        if (data.inspector.isPdf) {
          if (data.inspector.needsOcr) {
            inspBadge.textContent = '扫描版 PDF (需要 OCR)';
            inspBadge.style.background = '#f59e0b';
            inspSummary.innerHTML = `AnyDoc 探测: 检测为图像扫描件 (图像覆盖率 ${(data.inspector.imageCoverage * 100).toFixed(0)}%)。已执行 <strong>PP-DocLayout 版面识别</strong>，并多模型并发路由正文、表格与公式。`;
          } else {
            inspBadge.textContent = '原生矢量 PDF (无需 OCR)';
            inspBadge.style.background = '#10b981';
            inspSummary.innerHTML = `AnyDoc 探测: 包含 <strong>${data.inspector.totalChars}</strong> 字符矢量文本流。已执行 <strong>版面结构识别与表格/公式提取</strong>，普通正文直接精准直取 (0ms OCR，0 误识率)。`;
          }
        } else if (data.inspector.primaryType === 'anydoc-direct') {
          inspBadge.textContent = 'AnyDoc 结构化直接转换';
          inspBadge.style.background = '#3b82f6';
          inspSummary.innerHTML = `AnyDoc 直转: 非 PDF 结构化文档 (${data.inspector.totalChars} 字)，无需模型开销，已直接输出标准 Markdown 与结构化 JSON。`;
        } else {
          inspBanner.classList.add('hidden');
        }
      } else {
        inspBanner.classList.add('hidden');
      }
    }

    // Collect all lines
    let allLines = data.lines || [];
    if (!allLines.length && data.textBlocks) {
      data.textBlocks.forEach(tb => {
        if (tb.lines) allLines = allLines.concat(tb.lines);
      });
    }

    linesCountVal.textContent = allLines.length;

    // Average confidence
    if (allLines.length > 0) {
      const sum = allLines.reduce((acc, cur) => acc + (cur.score || 0), 0);
      confidenceVal.textContent = `${Math.round((sum / allLines.length) * 100)}%`;
    } else {
      confidenceVal.textContent = '--%';
    }

    // A. Full Text Tab
    let fullText = data.text || '';
    if (!fullText && data.markdown) {
      fullText = data.markdown;
    }
    outputText.value = fullText;

    // B. Markdown Preview Tab
    if (markdownPreview) {
      if (data.markdown) {
        markdownPreview.innerHTML = renderMarkdownHTML(data.markdown);
      } else {
        markdownPreview.innerHTML = renderMarkdownHTML(fullText);
      }
    }

    // C. Layout Regions Tab
    if (layoutRegionsList && data.regions) {
      tabLayoutBadge.textContent = data.regions.length;
      layoutRegionsList.innerHTML = '';
      data.regions.forEach((reg, index) => {
        const card = document.createElement('div');
        card.className = 'layout-card';
        card.innerHTML = `
          <div class="layout-card-info">
            <span class="region-badge ${reg.label}">${reg.label}</span>
            <span style="font-size:0.85rem;color:#f8fafc;font-weight:600;">区域 #${reg.id} (顺序: ${reg.orderNum || index + 1})</span>
          </div>
          <span style="font-size:0.8rem;color:var(--text-muted);font-family:var(--font-mono)">${Math.round(reg.score * 100)}% 置信度</span>
        `;
        card.addEventListener('mouseenter', () => {
          hoveredRegionIndex = index;
          drawBoundingBoxes();
        });
        card.addEventListener('mouseleave', () => {
          hoveredRegionIndex = -1;
          drawBoundingBoxes();
        });
        layoutRegionsList.appendChild(card);
      });
    }

    // D. Lines Tab
    linesList.innerHTML = '';
    tabLinesBadge.textContent = allLines.length;
    allLines.forEach((line, index) => {
      const card = document.createElement('div');
      card.className = 'line-card';
      card.id = `line-card-${index}`;
      const scorePct = Math.round((line.score || 0) * 100);

      card.innerHTML = `
        <div class="line-card-header">
          <span class="line-index font-mono">#${index + 1}</span>
          <span class="line-score font-mono ${scorePct >= 85 ? 'high' : 'med'}">${scorePct}%</span>
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

    // E. RESTful JSON Tab
    outputJson.textContent = JSON.stringify({
      code: 200,
      status: 'success',
      data: data
    }, null, 2);

    drawBoundingBoxes();
  }

  // Render Markdown string to formatted HTML with tables, math formula and cropped inline figures
  function renderMarkdownHTML(md) {
    if (!md) return '<div class="markdown-empty-hint">暂无内容</div>';

    let text = md.trim();

    // 1. Math display blocks: $$...$$
    const formulaBlocks = [];
    text = text.replace(/\$\$([\s\S]*?)\$\$/g, (match, formula) => {
      const idx = formulaBlocks.length;
      formulaBlocks.push(`<div class="formula-display">$$\n${escapeHtml(formula.trim())}\n$$</div>`);
      return `\n\n__FORMULA_BLOCK_${idx}__\n\n`;
    });

    // 2. Math inline: $...$
    text = text.replace(/\$([^\$\n]+)\$/g, (match, inline) => {
      return `<code class="formula-inline">$${escapeHtml(inline)}$</code>`;
    });

    // 3. Figures: ![caption](images/fig_X.png)
    const figureBlocks = [];
    text = text.replace(/!\[(.*?)\]\((.*?)\)/g, (match, caption, imgSrc) => {
      let displaySrc = imgSrc;
      if (currentResult && currentResult.figures) {
        const matchedFig = currentResult.figures.find(f => f.filename === imgSrc);
        if (matchedFig && matchedFig.dataUrl) {
          displaySrc = matchedFig.dataUrl;
        }
      }
      const idx = figureBlocks.length;
      figureBlocks.push(`
        <div class="figure-container">
          <img src="${displaySrc}" alt="文档插图" class="markdown-inline-figure" />
        </div>
      `);
      return `\n\n__FIGURE_BLOCK_${idx}__\n\n`;
    });

    // 4. Split by double newlines into blocks
    const rawBlocks = text.split(/\n{2,}/);
    const renderedBlocks = [];

    for (let b of rawBlocks) {
      b = b.trim();
      if (!b) continue;

      // Restore formulas
      if (b.startsWith('__FORMULA_BLOCK_') && b.endsWith('__')) {
        const idx = parseInt(b.replace('__FORMULA_BLOCK_', '').replace('__', ''));
        renderedBlocks.push(formulaBlocks[idx]);
        continue;
      }

      // Restore figures
      if (b.startsWith('__FIGURE_BLOCK_') && b.endsWith('__')) {
        const idx = parseInt(b.replace('__FIGURE_BLOCK_', '').replace('__', ''));
        renderedBlocks.push(figureBlocks[idx]);
        continue;
      }

      // Code blocks: ```language ... ```
      if (b.startsWith('```') && b.endsWith('```')) {
        const firstLineEnd = b.indexOf('\n');
        let codeLang = '';
        let codeBody = '';
        if (firstLineEnd !== -1) {
          codeLang = b.slice(3, firstLineEnd).trim();
          codeBody = b.slice(firstLineEnd + 1, -3).trim();
        } else {
          codeBody = b.slice(3, -3).trim();
        }
        renderedBlocks.push(`<pre style="background:rgba(15,23,42,0.85);border:1px solid rgba(51,65,85,0.7);padding:12px;border-radius:8px;overflow-x:auto;font-family:var(--font-mono);font-size:0.85rem;"><code class="code-block ${escapeHtml(codeLang)}">${escapeHtml(codeBody)}</code></pre>`);
        continue;
      }

      // Headers: # Header / ## Header / ### Header / #### Header
      if (b.startsWith('#### ')) {
        renderedBlocks.push(`<h4>${escapeHtml(b.substring(5).trim())}</h4>`);
        continue;
      }
      if (b.startsWith('### ')) {
        renderedBlocks.push(`<h3>${escapeHtml(b.substring(4).trim())}</h3>`);
        continue;
      }
      if (b.startsWith('## ')) {
        renderedBlocks.push(`<h2>${escapeHtml(b.substring(3).trim())}</h2>`);
        continue;
      }
      if (b.startsWith('# ')) {
        renderedBlocks.push(`<h1>${escapeHtml(b.substring(2).trim())}</h1>`);
        continue;
      }

      // Table parsing
      if (b.includes('|') && b.split('\n').some(l => l.trim().startsWith('|') && l.trim().endsWith('|'))) {
        const tLines = b.split('\n').map(l => l.trim()).filter(l => l.startsWith('|') && l.endsWith('|'));
        let tableHtml = '<table>';
        for (let tl of tLines) {
          if (tl.includes('---')) continue;
          const cols = tl.split('|').filter((_, idx, arr) => idx > 0 && idx < arr.length - 1);
          tableHtml += '<tr>' + cols.map(c => `<td>${escapeHtml(c.trim())}</td>`).join('') + '</tr>';
        }
        tableHtml += '</table>';
        renderedBlocks.push(tableHtml);
        continue;
      }

      // Paragraph: Merge soft linebreaks within the block
      const pLines = b.split('\n').map(l => l.trim()).filter(l => l);
      let mergedP = '';
      for (let j = 0; j < pLines.length; j++) {
        const line = pLines[j];
        if (j === 0) {
          mergedP = line;
        } else {
          const lastChar = mergedP.slice(-1);
          const firstChar = line.charAt(0);
          const isChineseLast = /[\u4e00-\u9fa5]/.test(lastChar);
          const isChineseFirst = /[\u4e00-\u9fa5]/.test(firstChar);

          if (isChineseLast && isChineseFirst) {
            mergedP += line;
          } else if (lastChar === '-') {
            mergedP = mergedP.slice(0, -1) + line;
          } else {
            mergedP += (/[\u4e00-\u9fa5]/.test(lastChar) || /[\u4e00-\u9fa5]/.test(firstChar)) ? line : ' ' + line;
          }
        }
      }

      renderedBlocks.push(`<p>${escapeHtml(mergedP)}</p>`);
    }

    return renderedBlocks.join('\n');
  }

  // 7. High-Definition Annotated Image Download (Base image + Bounding Boxes + Recognized Text)
  btnDownloadSingle.addEventListener('click', () => {
    if (!currentFile || !currentResult) {
      showToast('暂无识别结果可下载', 'error');
      return;
    }

    const exportCanvas = document.createElement('canvas');
    const width = previewImage.naturalWidth || canvas.width || 800;
    const height = previewImage.naturalHeight || canvas.height || 1100;
    exportCanvas.width = width;
    exportCanvas.height = height;
    const ctx = exportCanvas.getContext('2d');

    // 1. Draw original base image (eliminates transparent checkerboard background!)
    if (previewImage && previewImage.complete && previewImage.naturalWidth > 0) {
      ctx.drawImage(previewImage, 0, 0, width, height);
    } else {
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);
    }

    // 2. Draw macro layout regions (FIGURE, TEXT, HEADER, TABLE, FOOTER)
    if (currentResult.regions && currentResult.regions.length) {
      currentResult.regions.forEach(reg => {
        const box = reg.box;
        if (!box || box.length < 4) return;
        const x = box[0][0], y = box[0][1];
        const w = box[1][0] - box[0][0];
        const h = box[2][1] - box[1][1];

        const col = getCategoryColor(reg.label);
        ctx.fillStyle = col.fill || 'rgba(59, 130, 246, 0.1)';
        ctx.fillRect(x, y, w, h);
        ctx.strokeStyle = col.stroke || '#3b82f6';
        ctx.lineWidth = Math.max(2, Math.round(width / 450));
        ctx.strokeRect(x, y, w, h);

        // Region label badge (e.g. FIGURE, TEXT, HEADER)
        const labelText = reg.label.toUpperCase();
        const badgeFontSz = Math.max(14, Math.round(width / 45));
        ctx.font = `bold ${badgeFontSz}px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`;
        const tw = ctx.measureText(labelText).width;
        ctx.fillStyle = col.stroke || 'rgba(15, 23, 42, 0.9)';
        ctx.fillRect(x, Math.max(0, y - badgeFontSz - 6), tw + 14, badgeFontSz + 8);
        ctx.fillStyle = '#ffffff';
        ctx.fillText(labelText, x + 7, Math.max(badgeFontSz, y - 4));
      });
    } else if (currentResult.lines && currentResult.lines.length) {
      // Fallback only for pure OCR mode without layout analysis
      currentResult.lines.forEach((line) => {
        const box = line.box;
        if (!box || box.length < 4) return;
        ctx.beginPath();
        ctx.moveTo(box[0][0], box[0][1]);
        for (let i = 1; i < 4; i++) ctx.lineTo(box[i][0], box[i][1]);
        ctx.closePath();
        ctx.strokeStyle = '#06b6d4';
        ctx.lineWidth = Math.max(1.5, Math.round(width / 550));
        ctx.stroke();
      });
    }

    const a = document.createElement('a');
    const rawName = (currentFile.name || 'document').replace(/\.[^/.]+$/, '');
    a.download = `layout_${rawName}.png`;
    a.href = exportCanvas.toDataURL('image/png');
    a.click();
    showToast('版面布局带框图下载成功！', 'success');
  });

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

  // Reset
  btnReset.addEventListener('click', () => {
    currentFile = null;
    currentResult = null;
    previewContainer.classList.add('hidden');
    dropZone.classList.remove('hidden');
    clearCanvas();
    resetMetricsDisplay();
    btnRunOcr.disabled = true;
    showToast('已重置工作区', 'info');
  });

  btnChangeImage.addEventListener('click', () => {
    fileInput.click();
  });

  dropZone.addEventListener('click', () => {
    fileInput.click();
  });

  fileInput.addEventListener('change', (e) => {
    if (e.target.files && e.target.files[0]) {
      loadSingleFile(e.target.files[0]);
    }
  });

  // Drag and drop
  dropZone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropZone.classList.add('drag-over');
  });

  dropZone.addEventListener('dragleave', () => {
    dropZone.classList.remove('drag-over');
  });

  dropZone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropZone.classList.remove('drag-over');
    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
      loadSingleFile(e.dataTransfer.files[0]);
    }
  });

  // Clipboard Paste (Ctrl+V)
  window.addEventListener('paste', (e) => {
    const items = (e.clipboardData || e.originalEvent?.clipboardData)?.items;
    if (items) {
      for (let item of items) {
        if (item.kind === 'file') {
          const file = item.getAsFile();
          if (file) {
            loadSingleFile(file);
            showToast(`已通过剪贴板载入: ${file.name || '文件'}`, 'success');
            break;
          }
        }
      }
    }
  });

  // Copy Result Text or Markdown
  btnCopyResult.addEventListener('click', async () => {
    let textToCopy = outputText.value;
    const activeTab = document.querySelector('.tab-btn.active');
    if (activeTab && activeTab.getAttribute('data-tab') === 'markdown-tab' && currentResult && currentResult.markdown) {
      textToCopy = currentResult.markdown;
    } else if (activeTab && activeTab.getAttribute('data-tab') === 'json-tab' && currentResult) {
      textToCopy = JSON.stringify(currentResult, null, 2);
    }

    if (!textToCopy) {
      showToast('暂无识别结果可复制', 'error');
      return;
    }
    try {
      await navigator.clipboard.writeText(textToCopy);
      copyBtnText.textContent = '已复制！';
      showToast('已成功复制到剪贴板', 'success');
      setTimeout(() => copyBtnText.textContent = '一键复制', 2000);
    } catch (err) {
      showToast('复制失败，请手动选择复制', 'error');
    }
  });

  // Markdown ZIP Export with Cropped Figures & Paragraph Merging
  if (btnExportZip) {
    btnExportZip.addEventListener('click', async () => {
      if (!currentFile) {
        showToast('请先选择或识别文档图片', 'error');
        return;
      }

      const originalText = exportZipText ? exportZipText.textContent : '导出 MD (ZIP)';
      if (exportZipText) exportZipText.textContent = '打包中...';
      btnExportZip.disabled = true;

      try {
        showToast('正在构建 Markdown 与插图归档包...', 'info');

        const payload = {
          file: currentFile.dataUrl,
          filename: currentFile.name,
          image: currentFile.dataUrl,
          det: checkDet ? checkDet.checked : true,
          cls: checkCls ? checkCls.checked : true,
          rec: checkRec ? checkRec.checked : true,
          confidenceThreshold: 0.3
        };

        const res = await fetch('/api/v1/document/zip', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });

        if (!res.ok) {
          const errText = await res.text().catch(() => '');
          throw new Error(`导出失败 (HTTP ${res.status}): ${errText}`);
        }

        const blob = await res.blob();
        const baseName = currentFile.name.replace(/\.[^/.]+$/, '') || 'document';
        const downloadUrl = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = downloadUrl;
        a.download = `${baseName}_export.zip`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(downloadUrl);

        showToast('Markdown ZIP 导出成功！', 'success');
      } catch (err) {
        console.error('Export ZIP error:', err);
        showToast(`导出 ZIP 失败: ${err.message}`, 'error');
      } finally {
        if (exportZipText) exportZipText.textContent = originalText;
        btnExportZip.disabled = false;
      }
    });
  }

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
    const toast = document.getElementById('toast');
    if (!toast) return;

    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    toast.classList.remove('hidden');

    setTimeout(() => {
      toast.classList.add('hidden');
    }, 2500);
  }
});
