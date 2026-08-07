(function () {
  const state = { sourceURL: "", sourceBytes: 0, original: null, tool: "pan", drawing: false, comparing: false, busy: false, cutoutProcessing: false, width: 0, height: 0, zoom: 1, panX: 0, panY: 0, spacePressed: false, panning: null, last: null, rectStart: null, sample: null, sampleDragging: null, cloneOffset: null, region: null, regionScope: "inside", regionTool: "", regionAdjusting: false, regionStart: null, regionPoints: [], regionHandleDrag: null, regionSelectedHandle: null, regionSelectedVertex: null, undo: [], redo: [], batch: [], batchDone: false, batchProcessing: false, batchCanceled: false, batchXHR: null, batchTimer: 0, estimateTimer: 0, estimateSeq: 0, profileNames: { product: "商品" } };
  const el = (id) => document.getElementById(id);
  const canvas = () => el("retouchCanvas");
  const overlay = () => el("retouchOverlay");
  const regionOverlay = () => el("retouchRegionOverlay");
  const ctx = () => canvas().getContext("2d", { willReadFrequently: true });

  function setHint(text) { el("fineEditorHint").textContent = text; }
  function operationMode() {
    if (state.regionTool) return "绘制" + ({ rect: "矩形", circle: "圆形", polygon: "多边形" }[state.regionTool] || "") + "蒙版";
    if (state.regionAdjusting) return "调整" + ({ rect: "矩形", circle: "圆形", polygon: "多边形" }[state.region?.type] || "") + "蒙版";
    return { pan: "拖动画布", erase: "自由涂抹修复", rect: "框选消除", clone: "复制涂抹", cutout: "智能抠图" }[state.tool] || "查看";
  }
  function updateCanvasInfo() {
    const width = canvas().width, height = canvas().height;
    if (!state.width || !width || !height) { el("retouchImageInfo").textContent = "尚未打开图片"; return; }
    const displayWidth = Math.max(1, Math.round(state.width * state.zoom)), displayHeight = Math.max(1, Math.round(state.height * state.zoom));
    el("retouchImageInfo").textContent = "" + displayWidth + " × " + displayHeight + " · " + Math.round(state.zoom * 100) + "% · " + operationMode();
  }
  async function loadCutoutProfiles() {
    try {
      const response = await fetch("/api/image/remove-background/profiles", { cache: "no-store" });
      if (!response.ok) return;
      const body = await response.json(), profiles = Array.isArray(body.profiles) ? body.profiles : [];
      if (!profiles.length) return;
      const select = el("retouchCutoutProfile"), previous = select.value;
      select.replaceChildren(); state.profileNames = {};
      profiles.forEach((profile) => {
        const option = document.createElement("option"); option.value = profile.id; option.textContent = profile.label;
        option.title = profile.description || ""; option.selected = previous === profile.id || (!previous && profile.default);
        select.appendChild(option); state.profileNames[profile.id] = profile.label;
      });
      if (!select.value) select.value = profiles.find((profile) => profile.default)?.id || profiles[0].id;
    } catch (_) { }
  }
  function brushSize() { return Math.max(2, Number(el("retouchBrushSize").value) || 42); }
  function point(event) {
    const rect = overlay().getBoundingClientRect();
    return { x: (event.clientX - rect.left) * state.width / rect.width, y: (event.clientY - rect.top) * state.height / rect.height };
  }
  function resizeDisplay() {
    if (!state.width) return;
    const w = Math.round(state.width * state.zoom), h = Math.round(state.height * state.zoom);
    el("retouchCanvasWrap").style.width = w + "px";
    el("retouchCanvasWrap").style.height = h + "px";
    el("retouchCanvasWrap").style.transform = "translate(" + state.panX + "px," + state.panY + "px)";
    [canvas(), regionOverlay(), overlay()].forEach((item) => { item.style.width = w + "px"; item.style.height = h + "px"; });
  }
  function updateHistoryButtons() {
    el("retouchUndo").disabled = state.undo.length < 2;
    el("retouchRedo").disabled = !state.redo.length;
    if (el("retouchCompare")) el("retouchCompare").disabled = !state.original;
  }
  function pixelBytes(width = state.width, height = state.height) { return width * height * 4; }
  function lowMemoryImage(width = state.width, height = state.height) { return pixelBytes(width, height) > 64 * 1024 * 1024; }
  function snapshot() {
    state.undo.push(ctx().getImageData(0, 0, state.width, state.height));
    const max = Math.max(1, Math.min(12, Math.floor(96 * 1024 * 1024 / pixelBytes())));
    while (state.undo.length > max) state.undo.shift();
    state.redo = [];
    updateHistoryButtons();
  }
  function restore(image) { ctx().putImageData(image, 0, 0); }
  function undo() {
    if (state.undo.length < 2) return;
    state.redo.push(state.undo.pop()); restore(state.undo[state.undo.length - 1]); updateHistoryButtons();
  }
  function redo() {
    if (!state.redo.length) return;
    const image = state.redo.pop(); state.undo.push(image); restore(image); updateHistoryButtons();
  }
  function setZoom(value) {
    state.zoom = Math.max(.05, Math.min(5, value));
    el("retouchZoom").value = String(Math.round(state.zoom * 100));
    resizeDisplay(); updateCanvasInfo(); if (state.regionSelectedHandle) showSelectedMagnifier();
  }
  function backgroundColor(background) {
    if (background === "white") return "#ffffff";
    if (background === "green") return "#00ff00";
    if (background === "custom") return el("retouchBackgroundColor").value || "#2563eb";
    return "";
  }
  function fillBackground(target, background) {
    const color = backgroundColor(background);
    if (!color) return;
    const context = target.getContext("2d");
    context.globalCompositeOperation = "destination-over"; context.fillStyle = color;
    context.fillRect(0, 0, target.width, target.height); context.globalCompositeOperation = "source-over";
  }
  function fitToView() {
    const area = document.querySelector(".retouch-canvas-scroll");
    if (!area || !state.width || !state.height) return;
    setZoom(Math.min(1, (area.clientWidth - 48) / state.width, (area.clientHeight - 48) / state.height));
  }
  function changeBrush(multiplier) {
    const input = el("retouchBrushSize");
    input.value = String(Math.max(Number(input.min), Math.min(Number(input.max), Math.round(brushSize() * multiplier))));
    updateBrushValue();
  }
  function formatBytes(bytes) { if (!bytes) return "—"; const units = ["B", "KB", "MB", "GB"], i = Math.min(3, Math.floor(Math.log(bytes) / Math.log(1024))); return (bytes / 1024 ** i).toFixed(i ? 1 : 0) + " " + units[i]; }
  function updateBrushValue() { el("retouchBrushValue").textContent = Math.round(brushSize()) + " px"; }
  function exportSize() {
    if (el("retouchExportMode").value === "pixels") {
      const width = Math.max(1, Number(el("retouchExportWidth").value) || state.width || 1);
      return { width: Math.round(width), height: Math.max(1, Math.round(width * state.height / state.width)) };
    }
    const scale = Math.max(.01, (Number(el("retouchExportPercent").value) || 100) / 100);
    return { width: Math.max(1, Math.round(state.width * scale)), height: Math.max(1, Math.round(state.height * scale)) };
  }
  function exportCanvas() {
    const size = exportSize(), quality = Math.max(.01, Math.min(1, (Number(el("retouchExportQuality").value) || 90) / 100));
    const out = document.createElement("canvas"); out.width = size.width; out.height = size.height;
    const context = out.getContext("2d", { willReadFrequently: quality < 1 }); context.imageSmoothingQuality = "high";
    if (el("retouchExportFormat").value === "jpeg") { context.fillStyle = "#fff"; context.fillRect(0, 0, out.width, out.height); }
    context.drawImage(canvas(), 0, 0, out.width, out.height);
    if (quality < 1) { const image = context.getImageData(0, 0, out.width, out.height), step = quality >= .9 ? 2 : 6; for (let i = 0; i < image.data.length; i += 4) for (let c = 0; c < 3; c++) image.data[i + c] = Math.round(image.data[i + c] / step) * step; context.putImageData(image, 0, 0); }
    return out;
  }
  async function estimateExport() {
    if (!state.width) return; const seq = ++state.estimateSeq, out = exportCanvas(), jpeg = el("retouchExportFormat").value === "jpeg", quality = Math.max(.01, Math.min(1, Number(el("retouchExportQuality").value) / 100 || .9));
    el("retouchExportEstimate").textContent = out.width + " × " + out.height + " · 估算中…";
    try { const blob = await canvasBlob(out, jpeg ? "image/jpeg" : "image/png", quality); if (seq === state.estimateSeq) el("retouchExportEstimate").textContent = out.width + " × " + out.height + " · 约 " + formatBytes(blob.size); } catch (_) { }
  }
  function scheduleEstimate() { clearTimeout(state.estimateTimer); state.estimateSeq++; state.estimateTimer = setTimeout(estimateExport, 360); }
  async function download() {
    if (!state.width || state.busy) return; const jpeg = el("retouchExportFormat").value === "jpeg", quality = Math.max(.01, Math.min(1, Number(el("retouchExportQuality").value) / 100 || .9));
    const blob = await canvasBlob(exportCanvas(), jpeg ? "image/jpeg" : "image/png", quality);
    const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "retouched-" + Date.now() + (jpeg ? ".jpg" : ".png"); a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }
  function setBusy(busy, batch = false) {
    state.busy = busy; el("retouchBusy").hidden = !busy || batch;
    const batchProcessing = el("fineEditor").classList.contains("retouch-batch-processing");
    el("fineEditor").classList.toggle("retouch-batch-processing", busy && batch);
    el("fineEditor").setAttribute("aria-busy", String(busy));
    const controls = batch || batchProcessing ? el("fineEditor") : el("retouchEditor");
    controls.querySelectorAll("button,input,select,textarea").forEach((item) => {
      if (item.hasAttribute("data-retouch-allow-busy")) return;
      if (busy) { item.dataset.retouchWasDisabled = String(item.disabled); item.disabled = true; }
      else { item.disabled = item.dataset.retouchWasDisabled === "true"; delete item.dataset.retouchWasDisabled; }
    });
    document.querySelectorAll("[data-editor-tab]").forEach((item) => {
      if (busy) { item.dataset.retouchTabWasDisabled = String(item.disabled); item.disabled = true; }
      else { item.disabled = item.dataset.retouchTabWasDisabled === "true"; delete item.dataset.retouchTabWasDisabled; }
    });
    if (!busy) updateHistoryButtons();
  }
  function colorDistance(data, a, b) {
    const dr = data[a] - data[b], dg = data[a + 1] - data[b + 1], db = data[a + 2] - data[b + 2];
    return Math.sqrt(dr * dr * .3 + dg * dg * .59 + db * db * .11);
  }
  function cutoutCanvas(target, autoCenter, background) {
    const context = target.getContext("2d", { willReadFrequently: true }), w = target.width, h = target.height;
    const image = context.getImageData(0, 0, w, h), data = image.data, size = w * h;
    const outside = new Uint8Array(size), queue = new Int32Array(size); let head = 0, tail = 0;
    const seed = (i) => { if (!outside[i]) { outside[i] = 1; queue[tail++] = i; } };
    const corners = [0, w - 1, (h - 1) * w, size - 1];
    const matchingCorners = corners.filter((corner) => corners.some((other) => other !== corner && colorDistance(data, corner * 4, other * 4) < 36));
    const backgroundCorners = matchingCorners.length >= 2 ? matchingCorners : corners;
    // A pixel must stay close to an actual corner sample. Comparing only adjacent
    // pixels lets a flood-fill walk through gentle gradients and erase the subject.
    const looksLikeBackground = (i) => backgroundCorners.some((corner) => colorDistance(data, i * 4, corner * 4) < 42);
    for (let x = 0; x < w; x++) { if (looksLikeBackground(x)) seed(x); const bottom = (h - 1) * w + x; if (looksLikeBackground(bottom)) seed(bottom); }
    for (let y = 1; y < h - 1; y++) { const left = y * w, right = left + w - 1; if (looksLikeBackground(left)) seed(left); if (looksLikeBackground(right)) seed(right); }
    const threshold = 34;
    while (head < tail) {
      const i = queue[head++], x = i % w, y = (i / w) | 0, around = [];
      if (x) around.push(i - 1); if (x + 1 < w) around.push(i + 1); if (y) around.push(i - w); if (y + 1 < h) around.push(i + w);
      for (const n of around) if (!outside[n] && looksLikeBackground(n) && colorDistance(data, i * 4, n * 4) < threshold) { outside[n] = 1; queue[tail++] = n; }
    }
    // Refuse a clearly implausible mask instead of returning an almost empty image.
    let foregroundCount = 0;
    for (let i = 0; i < size; i++) if (!outside[i]) foregroundCount++;
    if (foregroundCount < size * .015) throw new Error("未能可靠识别主体，请换用背景更清晰的图片或手动补修");
    const alpha = new Uint8ClampedArray(size);
    for (let i = 0; i < size; i++) alpha[i] = outside[i] ? 0 : 255;
    // Two-pass feathering smooths stair-stepped edges while keeping the subject crisp.
    for (let pass = 0; pass < 2; pass++) {
      const next = new Uint8ClampedArray(alpha);
      for (let y = 1; y < h - 1; y++) for (let x = 1; x < w - 1; x++) {
        const i = y * w + x; if (alpha[i] === 0 || alpha[i] === 255) {
          const sum = alpha[i] * 4 + alpha[i - 1] + alpha[i + 1] + alpha[i - w] + alpha[i + w];
          if (sum > 0 && sum < 2040) next[i] = sum / 8;
        }
      }
      alpha.set(next);
    }
    let minX = w, minY = h, maxX = -1, maxY = -1;
    for (let i = 0; i < size; i++) {
      const a = alpha[i], o = i * 4; data[o + 3] = a;
      if (a > 24) { const x = i % w, y = (i / w) | 0; minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y); }
    }
    context.clearRect(0, 0, w, h); context.putImageData(image, 0, 0);
    if (autoCenter && maxX >= minX) {
      const copy = document.createElement("canvas"); copy.width = w; copy.height = h; copy.getContext("2d").drawImage(target, 0, 0);
      const dx = Math.round((w - (maxX - minX + 1)) / 2 - minX), dy = Math.round((h - (maxY - minY + 1)) / 2 - minY);
      context.clearRect(0, 0, w, h); context.drawImage(copy, dx, dy);
    }
    fillBackground(target, background);
    return target;
  }
  function canvasBlob(target, type = "image/png", quality) { return new Promise((resolve, reject) => target.toBlob((blob) => blob ? resolve(blob) : reject(new Error("图片编码失败")), type, quality)); }
  async function batchUploadBlob(target) {
    // Keep pixel dimensions but bound the multipart body. Large source files
    // can make Windows reset the connection before an HTTP error is readable.
    for (const quality of [.92, .8, .65]) {
      const blob = await canvasBlob(target, "image/jpeg", quality);
      if (blob.size <= 30 * 1024 * 1024 || quality === .65) return blob;
    }
  }
  function xhrCutout(form) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest(); xhr.open("POST", "/api/image/remove-background"); xhr.responseType = "json"; xhr.timeout = 9 * 60 * 1000; xhr.setRequestHeader("Accept", "application/json");
      if (state.batchProcessing) state.batchXHR = xhr;
      const finish = (callback) => { if (state.batchXHR === xhr) state.batchXHR = null; callback(); };
      xhr.onload = () => finish(() => resolve({ ok: xhr.status >= 200 && xhr.status < 300, status: xhr.status, body: xhr.response, warning: xhr.response?.warning || "" }));
      xhr.onerror = () => finish(() => reject(new Error("抠图连接被中断（服务仍在运行时通常是系统内存不足或安全软件终止了模型进程）")));
      xhr.ontimeout = () => finish(() => reject(new Error("抠图请求超过 9 分钟")));
      xhr.onabort = () => finish(() => reject(new Error("抠图请求已取消")));
      xhr.send(form);
    });
  }
  async function rembgCanvas(target, upload, xhrTransport = true) {
    // The visible canvas itself preserves the current image if the request fails;
    // history/compare buffers are expendable and otherwise add 125 MB each at 6720×4880.
    if (target === canvas() && lowMemoryImage()) { state.original = null; state.undo = []; state.redo = []; updateHistoryButtons(); }
    const source = upload || await canvasBlob(target), filename = upload?.name || "image.png";
    const form = new FormData(); form.append("image", source, filename); form.append("profile", el("retouchCutoutProfile").value); form.append("edge", el("retouchCutoutEdge").value);
    const response = xhrTransport ? await xhrCutout(form) : await fetch("/api/image/remove-background", { method: "POST", body: form });
    if (!response.ok) {
      let message = "模型抠图失败";
      try {
        const body = xhrTransport ? response.body : await response.json();
        message = body.error || body.message || message;
      } catch (_) { }
      const error = new Error(message); error.unavailable = response.status === 503; throw error;
    }
    const resultURL = xhrTransport ? response.body?.url : "";
    if (xhrTransport && !resultURL) throw new Error("抠图服务未返回结果地址");
    const image = xhrTransport ? await urlImage(resultURL, true) : await fileImage(await response.blob());
    // Once a large result has decoded successfully, release the old 100+ MB
    // history buffer before allocating another full-resolution canvas snapshot.
    if (target === canvas() && lowMemoryImage()) { state.original = null; state.undo = []; state.redo = []; updateHistoryButtons(); }
    const context = target.getContext("2d"); context.clearRect(0, 0, target.width, target.height); context.drawImage(image, 0, 0, target.width, target.height); image.src = "";
    finishCutout(target, el("retouchAutoCenter").checked, el("retouchBackground").value);
    if (resultURL) fetch(resultURL, { method: "DELETE" }).catch(() => { });
    return xhrTransport ? response.warning : response.headers.get("X-Kuaima-Cutout-Warning") || "";
  }
  function finishCutout(target, autoCenter, background) {
    const context = target.getContext("2d", { willReadFrequently: true }), w = target.width, h = target.height;
    if (autoCenter) {
      let minX = w, minY = h, maxX = -1, maxY = -1;
      // Scan in <=8 MB strips instead of allocating one extra full-size ImageData.
      const stripHeight = Math.max(1, Math.floor(8 * 1024 * 1024 / (w * 4)));
      for (let top = 0; top < h; top += stripHeight) {
        const rows = Math.min(stripHeight, h - top), data = context.getImageData(0, top, w, rows).data;
        for (let i = 0; i < w * rows; i++) if (data[i * 4 + 3] > 24) { const x = i % w, y = top + ((i / w) | 0); minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y); }
      }
      if (maxX >= minX) { const copy = document.createElement("canvas"); copy.width = w; copy.height = h; copy.getContext("2d").drawImage(target, 0, 0); context.clearRect(0, 0, w, h); context.drawImage(copy, Math.round((w - maxX + minX - 1) / 2 - minX), Math.round((h - maxY + minY - 1) / 2 - minY)); copy.width = copy.height = 1; }
    }
    fillBackground(target, background);
  }
  async function applyCutout() {
    if (!state.width || state.busy) return;
    const profileNames = state.profileNames, profile = el("retouchCutoutProfile").value;
    setHint("正在用“" + (profileNames[profile] || "商品") + "”模式抠图...");
    state.cutoutProcessing = true; setBusy(true);
    try {
      const warning = await rembgCanvas(canvas()); clearOverlay(); snapshot();
      setHint(warning === "fallback_low_memory" ? "高质量模型内存不足，已自动降级并完成抠图" : (profileNames[profile] || "商品") + "模式抠图完成；可按住“看原图”对比，或继续用画笔补修");
    } catch (error) {
      if (!error.unavailable) { setHint(error.message || "模型抠图失败，已保留当前图片"); return; }
      try { cutoutCanvas(canvas(), el("retouchAutoCenter").checked, el("retouchBackground").value); clearOverlay(); snapshot(); setHint("安装包缺少离线抠图引擎，已使用基础抠图"); }
      catch (fallbackError) { setHint(error.message || fallbackError.message); }
    } finally { state.cutoutProcessing = false; setBusy(false); scheduleEstimate(); }
  }
  function fileImage(file) { return new Promise((resolve, reject) => { const image = new Image(), url = URL.createObjectURL(file); image.onload = () => { URL.revokeObjectURL(url); resolve(image); }; image.onerror = () => { URL.revokeObjectURL(url); reject(new Error("抠图结果不是可识别的图片，响应可能不完整")); }; image.src = url; }); }
  function urlImage(url, retry = false) { return new Promise((resolve, reject) => { const image = new Image(); image.onload = () => resolve(image); image.onerror = () => { if (retry) { setTimeout(() => { urlImage(url, false).then(resolve, reject); }, 250); } else reject(new Error("抠图结果下载失败")); }; image.src = url + (url.includes("?") ? "&" : "?") + "t=" + Date.now(); }); }
  async function droppedFiles(transfer) {
    const readEntry = (entry) => new Promise((resolve) => {
      if (!entry) return resolve([]);
      if (entry.isFile) return entry.file((file) => resolve([file]), () => resolve([]));
      if (!entry.isDirectory) return resolve([]);
      const reader = entry.createReader(), all = [];
      const next = () => reader.readEntries(async (entries) => {
        if (!entries.length) return resolve(all.flat());
        all.push(await Promise.all(entries.map(readEntry))); next();
      }, () => resolve(all.flat())); next();
    });
    const items = Array.from(transfer.items || []), entries = items.map((item) => item.webkitGetAsEntry && item.webkitGetAsEntry()).filter(Boolean);
    return entries.length ? (await Promise.all(entries.map(readEntry))).flat() : Array.from(transfer.files || []);
  }
  async function processBatch(files) {
    const images = Array.from(files || []).filter((file) => String(file.type || "").startsWith("image/")); if (!images.length) return;
    state.batch.forEach((item) => { if (item.url) URL.revokeObjectURL(item.url); if (item.originalURL) URL.revokeObjectURL(item.originalURL); });
    state.batch = images.map((file) => ({ name: file.name.replace(/\.[^.]+$/, "") + "-cutout.png", originalName: file.name, originalURL: URL.createObjectURL(file), status: "等待中" }));
    state.batchDone = false; state.batchCanceled = false; state.batchProcessing = true; state.cutoutProcessing = true; el("cancelRetouchBatch").disabled = false; el("retouchBatch").hidden = false; el("retouchBatch").classList.remove("collapsed"); renderBatch();
    setBusy(true, true);
    for (let i = 0; i < images.length; i++) {
      if (state.batchCanceled) break;
      const file = images[i], item = state.batch[i]; item.status = "处理中"; item.startedAt = Date.now(); el("retouchBatchStatus").textContent = (i + 1) + " / " + images.length; renderBatch(); startBatchTimer();
      let out = null;
      try {
        const image = await fileImage(file); out = document.createElement("canvas"); out.width = image.naturalWidth; out.height = image.naturalHeight; out.getContext("2d").drawImage(image, 0, 0); image.src = "";
        // Batch input is encoded at its original dimensions but constrained to
        // a safe request size. The engine output remains full-size PNG.
        const upload = await batchUploadBlob(out);
        if (state.batchCanceled) throw new Error("抠图请求已取消");
        try { await rembgCanvas(out, upload, true); } catch (error) { if (!error.unavailable) throw error; cutoutCanvas(out, el("retouchAutoCenter").checked, el("retouchBackground").value); }
        const blob = await new Promise((resolve) => out.toBlob(resolve, "image/png")); item.blob = blob; item.url = URL.createObjectURL(blob); item.status = "已完成"; renderBatch();
      } catch (error) { if (state.batchCanceled) item.status = "已取消"; else { item.error = error?.message || "抠图失败"; item.status = "失败"; } renderBatch(); }
      finally { item.elapsedMs = Date.now() - item.startedAt; delete item.startedAt; stopBatchTimer(); if (out) out.width = out.height = 1; renderBatch(); }
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    if (state.batchCanceled) state.batch.forEach((item) => { if (item.status === "等待中") item.status = "已取消"; });
    const failed = state.batch.filter((item) => item.error);
    state.batchDone = true; state.batchProcessing = false; state.cutoutProcessing = false; state.batchXHR = null; renderBatch();
    el("retouchBatchStatus").textContent = state.batchCanceled ? "已取消，完成 " + state.batch.filter((item) => item.blob).length + " 张" : "完成 " + state.batch.filter((item) => item.blob).length + " 张" + (failed.length ? "，失败 " + failed.length + " 张：" + failed[0].error : "");
    setBusy(false);
    if (!state.batchCanceled && el("retouchBatchPlaySound").checked && typeof Audio !== "undefined") {
      try { new Audio("done.mp3").play().catch(() => { }); } catch (_) { }
    }
  }
  function formatBatchTime(ms) {
    const seconds = Math.max(0, Math.floor(ms / 1000)), minutes = Math.floor(seconds / 60), rest = seconds % 60;
    return minutes ? minutes + "分" + String(rest).padStart(2, "0") + "秒" : seconds + "秒";
  }
  function updateBatchTimers() {
    state.batch.forEach((item, i) => {
      if (!item.startedAt && item.elapsedMs == null) return;
      const timer = el("retouchBatchList").querySelector('[data-batch-time="' + i + '"]');
      if (timer) timer.textContent = "耗时 " + formatBatchTime(item.startedAt ? Date.now() - item.startedAt : item.elapsedMs || 0);
    });
  }
  function startBatchTimer() { stopBatchTimer(); updateBatchTimers(); state.batchTimer = window.setInterval(updateBatchTimers, 250); }
  function stopBatchTimer() { if (state.batchTimer) { window.clearInterval(state.batchTimer); state.batchTimer = 0; } }
  function renderBatch() {
    const escape = (value) => String(value || "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
    el("retouchBatchList").innerHTML = state.batch.map((item, i) => '<div class="retouch-batch-item" title="' + escape(item.error || item.name) + '"><div class="retouch-batch-previews"><figure><div class="retouch-batch-thumb"><img src="' + item.originalURL + '" alt="原图预览"></div><figcaption>原图</figcaption></figure><figure><div class="retouch-batch-thumb retouch-batch-result">' + (item.url ? '<img src="' + item.url + '" alt="抠图结果预览">' : '<span>' + escape(item.status) + '</span>') + '</div><figcaption>抠图结果</figcaption></figure></div><div class="retouch-batch-meta"><strong>' + escape(item.originalName) + '</strong>' + (item.error ? '<span class="retouch-batch-error">' + escape(item.error) + '</span>' : '<span>' + escape(item.status) + '</span>') + '<span class="retouch-batch-time" data-batch-time="' + i + '">耗时 ' + ((item.startedAt || item.elapsedMs != null) ? formatBatchTime(item.startedAt ? Date.now() - item.startedAt : item.elapsedMs) : '—') + '</span></div><button class="btn small" data-batch-download="' + i + '" ' + (!item.blob || state.busy ? 'disabled' : '') + '>' + (item.error ? '失败' : '下载') + '</button></div>').join("");
    el("downloadRetouchBatch").hidden = !state.batchDone || !state.batch.some((item) => item.blob);
    el("cancelRetouchBatch").hidden = !state.batchProcessing;
  }
  function cancelBatch() {
    if (!state.batchProcessing || state.batchCanceled) return;
    state.batchCanceled = true; el("retouchBatchStatus").textContent = "正在取消…"; el("cancelRetouchBatch").disabled = true;
    if (state.batchXHR) state.batchXHR.abort();
  }
  function saveBatchItem(item) { if (!item || !item.url) return; const a = document.createElement("a"); a.href = item.url; a.download = item.name; a.click(); }
  function clearOverlay() { overlay().getContext("2d").clearRect(0, 0, state.width, state.height); }
  function regionPath(region = state.region) {
    if (!region) return null;
    const path = new Path2D();
    if (region.type === "rect") path.rect(region.x, region.y, region.width, region.height);
    else if (region.type === "circle") path.ellipse(region.cx, region.cy, region.rx, region.ry, 0, 0, Math.PI * 2);
    else if (region.points?.length) { path.moveTo(region.points[0].x, region.points[0].y); region.points.slice(1).forEach((p) => path.lineTo(p.x, p.y)); if (region.points.length > 2) path.closePath(); }
    return path;
  }
  function renderRegion(preview = state.region) {
    const target = regionOverlay(), context = target.getContext("2d"); context.clearRect(0, 0, state.width, state.height);
    const path = regionPath(preview); if (!path) return;
    context.save(); context.fillStyle = "rgba(37,99,235,.18)";
    if (state.regionScope === "outside") { const outside = new Path2D(); outside.rect(0, 0, state.width, state.height); outside.addPath(path); context.fill(outside, "evenodd"); }
    else context.fill(path);
    context.strokeStyle = "#2563eb"; context.lineWidth = Math.max(1, 2 / state.zoom); context.setLineDash([Math.max(4, 7 / state.zoom), Math.max(3, 5 / state.zoom)]); context.stroke(path);
    if (state.regionAdjusting || preview !== state.region || preview.type === "polygon") {
      context.setLineDash([]); const radius = 7 / state.zoom;
      regionHandles(preview).forEach((handle) => {
        const active = state.regionHandleDrag?.id === handle.id, selected = handle.id === state.regionSelectedHandle;
        context.beginPath(); context.arc(handle.x, handle.y, handle.kind === "move" ? radius * 1.15 : radius, 0, Math.PI * 2); context.fillStyle = active ? "#f59e0b" : (selected ? "#fed7aa" : (handle.kind === "move" ? "#dbeafe" : "#fff")); context.fill();
        context.lineWidth = Math.max(2, 3 / state.zoom); context.strokeStyle = active || selected ? "#b45309" : "#2563eb"; context.stroke();
      });
    }
    context.restore();
  }
  function regionHandles(region = state.region) {
    if (!region) return [];
    if (region.type === "polygon") {
      const vertices = region.points.map((point, index) => ({ ...point, id: "vertex-" + index, kind: "vertex", index }));
      const center = region.points.reduce((sum, point) => ({ x: sum.x + point.x / region.points.length, y: sum.y + point.y / region.points.length }), { x: 0, y: 0 });
      return vertices.concat({ ...center, id: "move", kind: "move" });
    }
    if (region.type === "circle") return [{ x: region.cx, y: region.cy, id: "move", kind: "move" }, { x: region.cx + region.rx, y: region.cy, id: "radius", kind: "radius" }];
    const x2 = region.x + region.width, y2 = region.y + region.height;
    return [{ x: region.x, y: region.y, id: "nw", kind: "corner" }, { x: x2, y: region.y, id: "ne", kind: "corner" }, { x: x2, y: y2, id: "se", kind: "corner" }, { x: region.x, y: y2, id: "sw", kind: "corner" }, { x: region.x + region.width / 2, y: region.y + region.height / 2, id: "move", kind: "move" }];
  }
  function regionHandleAt(p) {
    if (!state.regionAdjusting || !state.region) return null; const tolerance = 12 / state.zoom;
    return regionHandles().find((handle) => Math.hypot(handle.x - p.x, handle.y - p.y) <= tolerance) || null;
  }
  function polygonSegmentAt(p) {
    if (!state.regionAdjusting || state.region?.type !== "polygon") return null;
    const tolerance = 9 / state.zoom, points = state.region.points; let best = null;
    points.forEach((a, index) => {
      const b = points[(index + 1) % points.length], dx = b.x - a.x, dy = b.y - a.y, length2 = dx * dx + dy * dy;
      const t = length2 ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / length2)) : 0;
      const projected = { x: a.x + dx * t, y: a.y + dy * t }, distance = Math.hypot(p.x - projected.x, p.y - projected.y);
      if (distance <= tolerance && (!best || distance < best.distance)) best = { index, point: projected, distance };
    });
    return best;
  }
  function moveRegionHandleTo(p) {
    const drag = state.regionHandleDrag; if (!drag) return;
    const original = JSON.parse(JSON.stringify(drag.original)), dx = p.x - drag.start.x, dy = p.y - drag.start.y;
    if (original.type === "polygon" && drag.kind === "vertex") original.points[drag.index] = { x: Math.max(0, Math.min(state.width, p.x)), y: Math.max(0, Math.min(state.height, p.y)) };
    else if (original.type === "polygon") {
      const minX = Math.min(...original.points.map((point) => point.x)), maxX = Math.max(...original.points.map((point) => point.x)), minY = Math.min(...original.points.map((point) => point.y)), maxY = Math.max(...original.points.map((point) => point.y));
      const moveX = Math.max(-minX, Math.min(state.width - maxX, dx)), moveY = Math.max(-minY, Math.min(state.height - maxY, dy));
      original.points.forEach((point) => { point.x += moveX; point.y += moveY; });
    } else if (original.type === "circle") {
      if (drag.kind === "move") { original.cx = Math.max(0, Math.min(state.width, original.cx + dx)); original.cy = Math.max(0, Math.min(state.height, original.cy + dy)); }
      else { const radius = Math.max(2, Math.hypot(p.x - original.cx, p.y - original.cy)); original.rx = radius; original.ry = radius; }
    } else if (drag.kind === "move") {
      original.x = Math.max(0, Math.min(state.width - original.width, original.x + dx)); original.y = Math.max(0, Math.min(state.height - original.height, original.y + dy));
    } else {
      const right = original.x + original.width, bottom = original.y + original.height;
      const oppositeX = drag.id.includes("w") ? right : original.x, oppositeY = drag.id.includes("n") ? bottom : original.y;
      original.x = Math.min(p.x, oppositeX); original.y = Math.min(p.y, oppositeY); original.width = Math.max(2, Math.abs(p.x - oppositeX)); original.height = Math.max(2, Math.abs(p.y - oppositeY));
    }
    state.region = original; renderRegion();
  }
  function hideMagnifier() { const magnifier = el("retouchMagnifier"); if (magnifier) magnifier.hidden = true; }
  function updateMagnifier(p, handle = state.regionHandleDrag) {
    const magnifier = el("retouchMagnifier");
    if (!handle || handle.kind === "move" || !magnifier) { hideMagnifier(); return; }
    const target = el("retouchMagnifierCanvas"), context = target.getContext("2d"), crop = 32, half = crop / 2, scale = target.width / crop;
    const focus = { x: Math.max(0, Math.min(state.width, p.x)), y: Math.max(0, Math.min(state.height, p.y)) };
    const sx = focus.x - half, sy = focus.y - half, clippedX = Math.max(0, sx), clippedY = Math.max(0, sy), clippedRight = Math.min(state.width, sx + crop), clippedBottom = Math.min(state.height, sy + crop);
    context.clearRect(0, 0, target.width, target.height); context.fillStyle = "#e2e8f0"; context.fillRect(0, 0, target.width, target.height);
    context.imageSmoothingEnabled = false;
    if (clippedRight > clippedX && clippedBottom > clippedY) context.drawImage(canvas(), clippedX, clippedY, clippedRight - clippedX, clippedBottom - clippedY, (clippedX - sx) * scale, (clippedY - sy) * scale, (clippedRight - clippedX) * scale, (clippedBottom - clippedY) * scale);
    const rightSide = focus.x / state.width < .72, lowerSide = focus.y / state.height < .55;
    magnifier.style.left = focus.x / state.width * 100 + "%"; magnifier.style.top = focus.y / state.height * 100 + "%";
    magnifier.style.transform = "translate(" + (rightSide ? "18px" : "calc(-100% - 18px)") + "," + (lowerSide ? "18px" : "calc(-100% - 18px)") + ")";
    magnifier.hidden = false;
  }
  function selectedRegionHandle() { return regionHandles().find((handle) => handle.id === state.regionSelectedHandle) || null; }
  function showSelectedMagnifier() { const handle = selectedRegionHandle(); if (handle) updateMagnifier(handle, handle); else hideMagnifier(); }
  function nudgeSelectedRegionHandle(dx, dy) {
    const handle = selectedRegionHandle(); if (!handle || !state.region) return false;
    if (state.region.type === "circle" && handle.kind === "radius") {
      const delta = dx || dy; state.region.rx = state.region.ry = Math.max(2, state.region.rx + delta);
    } else {
      state.regionHandleDrag = { ...handle, start: handle, original: JSON.parse(JSON.stringify(state.region)) };
      moveRegionHandleTo({ x: handle.x + dx, y: handle.y + dy }); state.regionHandleDrag = null;
    }
    renderRegion(); showSelectedMagnifier(); return true;
  }
  function finishRegion(region) {
    state.region = region; state.regionTool = ""; state.regionAdjusting = true; state.regionStart = null; state.regionPoints = []; state.regionHandleDrag = null; state.regionSelectedHandle = null; state.regionSelectedVertex = null; overlay().style.pointerEvents = "auto"; renderRegion();
    document.querySelectorAll("[data-retouch-region-shape]").forEach((button) => button.classList.toggle("active", button.dataset.retouchRegionShape === region.type));
    el("retouchClearRegion").disabled = false;
    el("retouchRegionHint").textContent = (state.regionScope === "inside" ? "只处理蒙版内部" : "只处理蒙版外部") + "；重新选择形状可替换";
    updateCanvasInfo();
    setHint("蒙版区域已设置，接下来涂抹修复只会处理蒙版" + (state.regionScope === "inside" ? "内部" : "外部"));
  }
  function clearRegion() {
    state.region = null; state.regionTool = ""; state.regionAdjusting = false; state.regionStart = null; state.regionPoints = []; state.regionHandleDrag = null; state.regionSelectedHandle = null; state.regionSelectedVertex = null; hideMagnifier(); renderRegion();
    el("retouchClearRegion").disabled = true;
    document.querySelectorAll("[data-retouch-region-shape]").forEach((button) => button.classList.remove("active")); el("retouchRegionHint").textContent = "选择形状后立即在图片上绘制";
    updateCanvasInfo();
  }
  function setRegionTool(type) {
    if (!state.width || state.busy) return;
    if (state.region?.type === type) {
      state.regionTool = ""; state.regionAdjusting = true; state.regionStart = null; state.regionPoints = []; state.regionHandleDrag = null; state.regionSelectedHandle = null; overlay().style.pointerEvents = "auto"; renderRegion();
      document.querySelectorAll("[data-retouch-region-shape]").forEach((button) => button.classList.toggle("active", button.dataset.retouchRegionShape === type));
      el("retouchPaintMenu").open = false; updateCanvasInfo(); setHint(type === "polygon" ? "多边形调整：拖动中心移动；点击边线加端点；双击端点删除" : (type === "rect" ? "矩形调整模式：拖动中心移动，拖动四角调整大小" : "圆形调整模式：拖动中心移动，拖动边缘点调整大小")); return;
    }
    state.regionTool = type; state.regionAdjusting = false; state.regionStart = null; state.regionPoints = []; overlay().style.pointerEvents = "auto"; clearOverlay();
    document.querySelectorAll("[data-retouch-region-shape]").forEach((button) => button.classList.toggle("active", button.dataset.retouchRegionShape === type));
    el("retouchPaintMenu").open = false; updateCanvasInfo(); setHint(type === "polygon" ? "依次点击多边形顶点，双击或按 Enter 完成" : "在图片上拖动绘制" + (type === "rect" ? "矩形" : "圆形") + "蒙版");
  }
  function clipToRegion(context) {
    const path = regionPath(); if (!path) return;
    if (state.regionScope === "inside") context.clip(path);
    else { const outside = new Path2D(); outside.rect(0, 0, state.width, state.height); outside.addPath(path); context.clip(outside, "evenodd"); }
  }
  function line(a, b, draw) {
    const distance = Math.hypot(b.x - a.x, b.y - a.y);
    const step = Math.max(1, brushSize() * .18);
    for (let i = 0, n = Math.max(1, Math.ceil(distance / step)); i <= n; i++) draw({ x: a.x + (b.x - a.x) * i / n, y: a.y + (b.y - a.y) * i / n });
  }
  function markErase(p) {
    const paint = overlay().getContext("2d");
    paint.fillStyle = "rgba(239,68,68,.55)"; paint.beginPath(); paint.arc(p.x, p.y, brushSize() / 2, 0, Math.PI * 2); paint.fill();
  }
  async function inpaint() {
    const mask = overlay().getContext("2d").getImageData(0, 0, state.width, state.height).data;
    let regionMask = null;
    if (state.region) { const temp = document.createElement("canvas"); temp.width = state.width; temp.height = state.height; const rc = temp.getContext("2d"); rc.fillStyle = "#fff"; rc.fill(regionPath()); regionMask = rc.getImageData(0, 0, state.width, state.height).data; }
    const image = ctx().getImageData(0, 0, state.width, state.height), data = image.data;
    const unknown = new Uint8Array(state.width * state.height);
    let remaining = 0;
    // Transparent source pixels are already empty. Keeping them out of the
    // repair mask prevents their zeroed RGB channels from becoming opaque black.
    for (let i = 0; i < unknown.length; i++) {
      const inRegion = !regionMask || (regionMask[i * 4 + 3] > 0) === (state.regionScope === "inside");
      if (inRegion && mask[i * 4 + 3] > 12 && data[i * 4 + 3] > 0) { unknown[i] = 1; remaining++; }
      if (i && i % 500000 === 0) await new Promise(requestAnimationFrame);
    }
    if (!remaining) return false;
    const queued = new Uint8Array(unknown.length), queue = [], neighbors = (i) => {
      const x = i % state.width, y = Math.floor(i / state.width), list = [];
      for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) {
        if ((!dx && !dy) || x + dx < 0 || x + dx >= state.width || y + dy < 0 || y + dy >= state.height) continue;
        list.push(i + dy * state.width + dx);
      }
      return list;
    };
    for (let i = 0; i < unknown.length; i++) {
      if (unknown[i] && neighbors(i).some((n) => !unknown[n])) { queue.push(i); queued[i] = 1; }
      if (i && i % 300000 === 0) await new Promise(requestAnimationFrame);
    }
    for (let head = 0; head < queue.length; head++) {
      const i = queue[head]; if (!unknown[i]) continue;
      let r = 0, g = 0, b = 0, alpha = 0, count = 0;
      const around = neighbors(i);
      around.forEach((n) => { if (!unknown[n]) { const a = data[n * 4 + 3] / 255; r += data[n * 4] * a; g += data[n * 4 + 1] * a; b += data[n * 4 + 2] * a; alpha += a; count++; } });
      if (!count) continue;
      const o = i * 4, outputAlpha = alpha / count;
      data[o] = alpha ? r / alpha : 0; data[o + 1] = alpha ? g / alpha : 0; data[o + 2] = alpha ? b / alpha : 0; data[o + 3] = Math.round(outputAlpha * 255); unknown[i] = 0; remaining--;
      around.forEach((n) => { if (unknown[n] && !queued[n]) { queued[n] = 1; queue.push(n); } });
      if (head && head % 12000 === 0) await new Promise(requestAnimationFrame);
    }
    ctx().putImageData(image, 0, 0); clearOverlay(); return true;
  }
  function cloneStamp(p) {
    const radius = brushSize() / 2, source = { x: p.x + state.cloneOffset.x, y: p.y + state.cloneOffset.y };
    const size = Math.max(2, Math.ceil(radius * 2)), patch = document.createElement("canvas"); patch.width = patch.height = size;
    const pc = patch.getContext("2d");
    pc.drawImage(canvas(), source.x - radius, source.y - radius, radius * 2, radius * 2, 0, 0, size, size);
    pc.globalCompositeOperation = "destination-in";
    const gradient = pc.createRadialGradient(size / 2, size / 2, size * .28, size / 2, size / 2, size / 2);
    gradient.addColorStop(0, "#fff"); gradient.addColorStop(1, "rgba(255,255,255,0)"); pc.fillStyle = gradient; pc.fillRect(0, 0, size, size);
    const context = ctx(); context.save(); clipToRegion(context); context.drawImage(patch, p.x - radius, p.y - radius, radius * 2, radius * 2); context.restore();
  }
  function updateCloneSample() {
    const marker = el("retouchCloneSample"), visible = state.tool === "clone" && state.sample && state.width && state.height;
    marker.hidden = !visible;
    if (!visible) return;
    marker.style.left = state.sample.x / state.width * 100 + "%";
    marker.style.top = state.sample.y / state.height * 100 + "%";
  }
  function moveCloneSample(event) {
    const rect = overlay().getBoundingClientRect();
    state.sample = {
      x: Math.max(0, Math.min(state.width, (event.clientX - rect.left) * state.width / rect.width)),
      y: Math.max(0, Math.min(state.height, (event.clientY - rect.top) * state.height / rect.height)),
    };
    state.cloneOffset = null; updateCloneSample();
  }
  function setTool(tool) {
    if (state.busy) return;
    state.tool = ["pan", "cutout", "clone", "rect"].includes(tool) ? tool : "erase"; state.sample = null; state.sampleDragging = null; state.cloneOffset = null; state.rectStart = null; state.regionTool = ""; state.regionAdjusting = false; state.regionHandleDrag = null; state.regionSelectedHandle = null; state.regionSelectedVertex = null; state.regionStart = null; state.regionPoints = []; hideMagnifier(); clearOverlay(); renderRegion(); updateCloneSample();
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.classList.toggle("active", button.dataset.retouchTool === state.tool));
    const names = { erase: "自由涂抹", rect: "框选消除", clone: "复制涂抹" };
    if (names[state.tool]) el("retouchPaintLabel").textContent = names[state.tool];
    el("retouchPaintMenu").querySelector("summary").classList.toggle("active", ["erase", "rect", "clone"].includes(state.tool));
    el("retouchCutoutMenu").querySelector("summary").classList.toggle("active", state.tool === "cutout");
    document.querySelector(".retouch-canvas-scroll")?.classList.toggle("pan-tool", state.tool === "pan");
    overlay().style.pointerEvents = state.tool === "cutout" ? "none" : "auto";
    updateCanvasInfo();
    setHint(state.tool === "pan" ? "按住并拖动画布；滚轮缩放，选择涂抹工具后可编辑图片" : (state.tool === "cutout" ? "打开抠图设置并点击“开始抠图”" : (state.tool === "rect" ? "拖出矩形框，松开后自动消除框内内容" : (state.tool === "erase" ? "在需要移除的物体上涂抹，松开后自动修复" : "先点击取样位置，再在目标位置涂抹；按 Alt 点击可重新取样"))));
  }
  function open(url, initialTool) {
    if (!url) return;
    el("finePointEditorView").hidden = true; el("retouchEditor").hidden = false;
    document.querySelectorAll("[data-editor-tab]").forEach((button) => { const active = button.dataset.editorTab === "manual"; button.classList.toggle("active", active); button.setAttribute("aria-selected", String(active)); });
    if (state.sourceURL === url && state.width) { resizeDisplay(); setTool(initialTool || state.tool); return; }
    state.sourceURL = url; const image = new Image(); image.crossOrigin = "anonymous"; setHint("正在读取图片...");
    image.onload = () => {
      state.width = image.naturalWidth; state.height = image.naturalHeight;
      state.panX = 0; state.panY = 0;
      [canvas(), regionOverlay(), overlay()].forEach((item) => { item.width = state.width; item.height = state.height; });
      clearRegion();
      ctx().drawImage(image, 0, 0); state.original = null; state.undo = []; state.redo = []; snapshot();
      // Reuse the first undo buffer as the compare image. For very large images,
      // keeping even one additional 100+ MB original defeats the low-memory path.
      if (!lowMemoryImage()) state.original = state.undo[0];
      updateHistoryButtons(); resizeDisplay(); setTool(initialTool || state.tool || "erase");
      el("retouchEmpty").hidden = true; el("retouchCanvasWrap").hidden = false;
      updateCanvasInfo();
      el("retouchExportWidth").value = String(state.width); el("retouchExportHeight").value = String(state.height);
      el("imageModal").hidden = true; el("fineEditor").hidden = false; requestAnimationFrame(() => { fitToView(); scheduleEstimate(); });
    };
    image.onerror = () => setHint("图片读取失败，可能不允许跨域编辑"); image.src = url;
  }
  function openEmpty(tool = "cutout") {
    state.sourceURL = ""; state.sourceBytes = 0; state.width = 0; state.height = 0; state.original = null; state.undo = []; state.redo = []; updateHistoryButtons();
    el("finePointEditorView").hidden = true; el("retouchEditor").hidden = false; el("fineEditor").hidden = false; el("imageModal").hidden = true;
    el("retouchEmpty").hidden = false; el("retouchCanvasWrap").hidden = true; updateCanvasInfo();
    document.querySelectorAll("[data-editor-tab]").forEach((button) => { const active = button.dataset.editorTab === "manual"; button.classList.toggle("active", active); button.setAttribute("aria-selected", String(active)); });
    setTool(tool); setHint("选择或拖入图片后即可开始一键抠图");
  }
  function close() { state.drawing = false; el("fineEditor").hidden = true; }
  function init() {
    loadCutoutProfiles();
    try { el("retouchBatchPlaySound").checked = localStorage.getItem("retouchBatchPlaySound") !== "false"; } catch (_) { }
    el("retouchBatchPlaySound").addEventListener("change", (event) => { try { localStorage.setItem("retouchBatchPlaySound", String(event.target.checked)); } catch (_) { } });
    document.addEventListener("click", (event) => {
      document.querySelectorAll(".retouch-menu[open]").forEach((menu) => { if (!menu.contains(event.target)) menu.open = false; });
    });
    document.querySelectorAll(".retouch-menu").forEach((menu) => menu.addEventListener("toggle", () => {
      if (!menu.open) return;
      document.querySelectorAll(".retouch-menu[open]").forEach((other) => { if (other !== menu) other.open = false; });
    }));
    document.addEventListener("click", (event) => { if (event.target.closest("[data-cutout-tab]")) openEmpty("cutout"); });
    el("retouchEmpty").addEventListener("click", () => el("retouchSourceFiles").click());
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.addEventListener("click", () => setTool(button.dataset.retouchTool)));
    document.querySelectorAll("[data-retouch-region-shape]").forEach((button) => button.addEventListener("click", () => setRegionTool(button.dataset.retouchRegionShape)));
    document.querySelectorAll("[data-retouch-region-scope]").forEach((button) => button.addEventListener("click", () => {
      state.regionScope = button.dataset.retouchRegionScope;
      document.querySelectorAll("[data-retouch-region-scope]").forEach((item) => item.classList.toggle("active", item === button)); renderRegion();
      if (state.region) el("retouchRegionHint").textContent = (state.regionScope === "inside" ? "只处理蒙版内部" : "只处理蒙版外部") + "；重新选择形状可替换";
    }));
    el("retouchClearRegion").addEventListener("click", clearRegion);
    el("retouchApplyCutout").addEventListener("click", applyCutout);
    const syncBackgroundColor = () => {
      const custom = el("retouchBackground").value === "custom";
      el("retouchBackgroundColorRow").hidden = !custom;
      el("retouchBackgroundColorValue").value = el("retouchBackgroundColor").value.toUpperCase();
    };
    el("retouchBackground").addEventListener("change", syncBackgroundColor);
    el("retouchBackgroundColor").addEventListener("input", syncBackgroundColor); syncBackgroundColor();
    el("retouchBrushSize").addEventListener("input", updateBrushValue); updateBrushValue();
    function syncExportSize(changed) {
      if (!state.width) return;
      const ratio = state.width / state.height;
      if (changed === "height") el("retouchExportWidth").value = String(Math.max(1, Math.round((Number(el("retouchExportHeight").value) || state.height) * ratio)));
      else el("retouchExportHeight").value = String(Math.max(1, Math.round((Number(el("retouchExportWidth").value) || state.width) / ratio)));
      scheduleEstimate();
    }
    el("retouchExportMode").addEventListener("change", () => {
      const pixels = el("retouchExportMode").value === "pixels";
      el("retouchExportPercentRow").hidden = pixels; el("retouchExportPixelsRow").hidden = !pixels;
      if (pixels) { el("retouchExportWidth").value = String(state.width || 1); el("retouchExportHeight").value = String(state.height || 1); }
      scheduleEstimate();
    });
    el("retouchExportFormat").addEventListener("change", scheduleEstimate);
    el("retouchExportPercent").addEventListener("input", scheduleEstimate);
    el("retouchExportQuality").addEventListener("input", scheduleEstimate);
    el("retouchExportWidth").addEventListener("input", () => syncExportSize("width"));
    el("retouchExportHeight").addEventListener("input", () => syncExportSize("height"));
    el("retouchZoom").addEventListener("input", (event) => setZoom(Number(event.target.value) / 100));
    el("retouchUndo").addEventListener("click", undo); el("retouchRedo").addEventListener("click", redo);
    el("downloadRetouch").addEventListener("click", download);
    const startCompare = () => { if (state.original && !state.comparing) { state.comparing = true; ctx().putImageData(state.original, 0, 0); } };
    const stopCompare = () => { if (state.comparing && state.undo.length) restore(state.undo[state.undo.length - 1]); state.comparing = false; };
    el("retouchCompare").addEventListener("pointerdown", startCompare);
    el("retouchCompare").addEventListener("pointerup", stopCompare); el("retouchCompare").addEventListener("pointerleave", stopCompare); el("retouchCompare").addEventListener("pointercancel", stopCompare);
    el("retouchSourceFiles").addEventListener("change", (event) => { const files = Array.from(event.target.files || []); event.target.value = ""; if (files.length === 1) { state.sourceBytes = files[0].size; open(URL.createObjectURL(files[0])); } else processBatch(files); });
    const dropArea = document.querySelector(".retouch-canvas-scroll");
    dropArea.addEventListener("pointerdown", (event) => {
      if (event.button === 0 && !state.spacePressed && (state.regionTool || state.regionAdjusting)) return;
      if (!state.width || state.busy || !(event.button === 1 || (event.button === 0 && (state.spacePressed || state.tool === "pan")))) return;
      event.preventDefault(); event.stopPropagation();
      state.panning = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, left: dropArea.scrollLeft, top: dropArea.scrollTop, panX: state.panX, panY: state.panY };
      dropArea.setPointerCapture(event.pointerId); dropArea.classList.add("panning");
    }, true);
    dropArea.addEventListener("pointermove", (event) => {
      if (!state.panning || state.panning.pointerId !== event.pointerId) return;
      const dx = event.clientX - state.panning.x, dy = event.clientY - state.panning.y;
      dropArea.scrollLeft = state.panning.left - dx;
      dropArea.scrollTop = state.panning.top - dy;
      state.panX = state.panning.panX + dx + (dropArea.scrollLeft - state.panning.left);
      state.panY = state.panning.panY + dy + (dropArea.scrollTop - state.panning.top);
      el("retouchCanvasWrap").style.transform = "translate(" + state.panX + "px," + state.panY + "px)";
    });
    const stopPan = (event) => {
      if (!state.panning || (event.pointerId != null && state.panning.pointerId !== event.pointerId)) return;
      state.panning = null; dropArea.classList.remove("panning");
    };
    dropArea.addEventListener("pointerup", stopPan); dropArea.addEventListener("pointercancel", stopPan);
    dropArea.addEventListener("wheel", (event) => {
      if (!state.width || state.busy) return;
      event.preventDefault();
      const before = state.zoom, next = before * (event.deltaY < 0 ? 1.12 : 1 / 1.12);
      const rect = dropArea.getBoundingClientRect(), x = event.clientX - rect.left + dropArea.scrollLeft, y = event.clientY - rect.top + dropArea.scrollTop;
      setZoom(next); const factor = state.zoom / before;
      dropArea.scrollLeft = x * factor - (event.clientX - rect.left); dropArea.scrollTop = y * factor - (event.clientY - rect.top);
    }, { passive: false });
    ["dragenter", "dragover"].forEach((name) => dropArea.addEventListener(name, (event) => { event.preventDefault(); el("retouchDropHint").hidden = false; }));
    ["dragleave", "drop"].forEach((name) => dropArea.addEventListener(name, (event) => { event.preventDefault(); el("retouchDropHint").hidden = true; }));
    dropArea.addEventListener("drop", async (event) => { const files = (await droppedFiles(event.dataTransfer)).filter((file) => String(file.type || "").startsWith("image/")); if (files.length === 1) { state.sourceBytes = files[0].size; open(URL.createObjectURL(files[0])); } else processBatch(files); });
    el("retouchBatchToggle").addEventListener("click", () => el("retouchBatch").classList.toggle("collapsed"));
    el("retouchBatchList").addEventListener("click", (event) => { const button = event.target.closest("[data-batch-download]"); if (button) saveBatchItem(state.batch[Number(button.dataset.batchDownload)]); });
    el("cancelRetouchBatch").addEventListener("click", cancelBatch);
    el("downloadRetouchBatch").addEventListener("click", async () => { for (const item of state.batch) { saveBatchItem(item); await new Promise((resolve) => setTimeout(resolve, 180)); } });
    overlay().addEventListener("pointerdown", (event) => {
      event.preventDefault(); const p = point(event);
      if (state.regionTool) {
        if (state.regionTool === "polygon") {
          const first = state.regionPoints[0], closeTolerance = 12 / state.zoom;
          if (state.regionPoints.length >= 3 && first && Math.hypot(p.x - first.x, p.y - first.y) <= closeTolerance) {
            finishRegion({ type: "polygon", points: state.regionPoints.slice() }); state.drawing = false; setHint("多边形已闭合，可拖动控制点继续调整"); return;
          }
          state.regionPoints.push(p); state.drawing = true; renderRegion({ type: "polygon", points: state.regionPoints });
          el("retouchRegionHint").textContent = state.regionPoints.length < 3 ? "继续点击顶点（至少 3 个）" : "点击起点、双击或按 Enter 完成";
        } else { state.drawing = true; state.regionStart = p; overlay().setPointerCapture(event.pointerId); }
        return;
      }
      const handle = regionHandleAt(p);
      if (handle) {
        state.regionSelectedHandle = handle.id;
        state.regionSelectedVertex = handle.kind === "vertex" ? handle.index : null;
        state.regionHandleDrag = { ...handle, start: p, original: JSON.parse(JSON.stringify(state.region)) }; state.drawing = true; overlay().setPointerCapture(event.pointerId); renderRegion();
        updateMagnifier(p);
        setHint(handle.kind === "move" ? "拖动调整蒙版位置" : "拖动调整蒙版大小或端点位置"); return;
      }
      const segment = polygonSegmentAt(p);
      if (segment) {
        state.region.points.splice(segment.index + 1, 0, segment.point);
        const handle = { ...segment.point, id: "vertex-" + (segment.index + 1), kind: "vertex", index: segment.index + 1 };
        state.regionSelectedHandle = handle.id;
        state.regionSelectedVertex = handle.index;
        state.regionHandleDrag = { ...handle, start: p, original: JSON.parse(JSON.stringify(state.region)) }; state.drawing = true; overlay().setPointerCapture(event.pointerId); renderRegion();
        updateMagnifier(p);
        setHint("已在线段中增加端点，可直接拖动调整位置"); return;
      }
      if (state.regionAdjusting) return;
      if (state.tool === "clone" && (!state.sample || event.altKey)) { state.sample = p; state.cloneOffset = null; updateCloneSample(); setHint("取样完成，可拖动标记调整位置，然后在目标位置按住并涂抹"); return; }
      state.drawing = true; state.last = p; overlay().setPointerCapture(event.pointerId);
      if (state.tool === "rect") { state.rectStart = p; clearOverlay(); }
      else if (state.tool === "clone") { if (!state.cloneOffset) state.cloneOffset = { x: state.sample.x - p.x, y: state.sample.y - p.y }; cloneStamp(p); } else markErase(p);
    });
    overlay().addEventListener("pointermove", (event) => {
      const p = point(event);
      if (!state.drawing) { overlay().style.cursor = regionHandleAt(p) ? "grab" : (polygonSegmentAt(p) ? "copy" : "crosshair"); return; }
      if (state.regionHandleDrag) { moveRegionHandleTo(p); updateMagnifier(p); return; }
      if (state.regionTool && state.regionTool !== "polygon") {
        const a = state.regionStart;
        const radius = Math.hypot(p.x - a.x, p.y - a.y);
        renderRegion(state.regionTool === "rect" ? { type: "rect", x: Math.min(a.x, p.x), y: Math.min(a.y, p.y), width: Math.abs(p.x - a.x), height: Math.abs(p.y - a.y) } : { type: "circle", cx: a.x, cy: a.y, rx: radius, ry: radius }); return;
      }
      if (state.regionTool === "polygon") return;
      if (state.tool === "rect") {
        clearOverlay(); const context = overlay().getContext("2d"), x = Math.min(state.rectStart.x, p.x), y = Math.min(state.rectStart.y, p.y);
        context.fillStyle = "rgba(239,68,68,.28)"; context.strokeStyle = "#ef4444"; context.lineWidth = Math.max(1, 2 / state.zoom);
        context.setLineDash([Math.max(4, 6 / state.zoom), Math.max(3, 4 / state.zoom)]); context.fillRect(x, y, Math.abs(p.x - state.rectStart.x), Math.abs(p.y - state.rectStart.y)); context.strokeRect(x, y, Math.abs(p.x - state.rectStart.x), Math.abs(p.y - state.rectStart.y));
      } else line(state.last, p, state.tool === "clone" ? cloneStamp : markErase); state.last = p;
    });
    async function finish(event) {
      if (!state.drawing) return; state.drawing = false;
      if (state.regionHandleDrag) {
        moveRegionHandleTo(point(event));
        state.regionHandleDrag = null; renderRegion(); showSelectedMagnifier(); overlay().style.cursor = "crosshair"; setHint("控制点已选中，可用方向键精确调整；按 Shift 每次移动 10 像素"); return;
      }
      if (state.regionTool) {
        if (state.regionTool === "polygon") return;
        const p = point(event), a = state.regionStart;
        const radius = Math.hypot(p.x - a.x, p.y - a.y);
        const region = state.regionTool === "rect" ? { type: "rect", x: Math.min(a.x, p.x), y: Math.min(a.y, p.y), width: Math.abs(p.x - a.x), height: Math.abs(p.y - a.y) } : { type: "circle", cx: a.x, cy: a.y, rx: radius, ry: radius };
        if ((region.width || region.rx * 2) >= 2 && (region.height || region.ry * 2) >= 2) finishRegion(region); else renderRegion(); return;
      }
      if (state.tool === "erase" || state.tool === "rect") {
        state.busy = true; el("retouchCanvasWrap").classList.add("erasing"); overlay().style.pointerEvents = "none";
        document.querySelectorAll("[data-editor-tab]").forEach((button) => { button.dataset.eraseWasDisabled = String(button.disabled); button.disabled = true; });
        await new Promise(requestAnimationFrame);
        try { if (await inpaint()) snapshot(); }
        finally {
          state.busy = false; el("retouchCanvasWrap").classList.remove("erasing"); overlay().style.pointerEvents = state.tool === "cutout" ? "none" : "auto";
          document.querySelectorAll("[data-editor-tab]").forEach((button) => { button.disabled = button.dataset.eraseWasDisabled === "true"; delete button.dataset.eraseWasDisabled; });
        }
      } else { clearOverlay(); snapshot(); }
      state.rectStart = null;
    }
    overlay().addEventListener("pointerup", finish); overlay().addEventListener("pointercancel", finish);
    overlay().addEventListener("dblclick", (event) => {
      if (state.regionTool === "polygon" && state.regionPoints.length >= 3) { event.preventDefault(); finishRegion({ type: "polygon", points: state.regionPoints.slice() }); state.drawing = false; return; }
      if (state.regionAdjusting && state.region?.type === "polygon") {
        const handle = regionHandleAt(point(event));
        if (handle?.kind === "vertex") {
          event.preventDefault();
          if (state.region.points.length <= 3) { setHint("多边形至少需要保留 3 个端点"); return; }
          state.region.points.splice(handle.index, 1); state.regionHandleDrag = null; state.regionSelectedHandle = null; state.regionSelectedVertex = null; state.drawing = false; hideMagnifier(); renderRegion(); setHint("端点已删除；多边形至少保留 3 个端点");
        }
      }
    });
    el("retouchCloneSample").addEventListener("pointerdown", (event) => {
      if (state.busy || state.tool !== "clone") return;
      event.preventDefault(); event.stopPropagation(); state.sampleDragging = event.pointerId;
      el("retouchCloneSample").setPointerCapture(event.pointerId); el("retouchCloneSample").classList.add("dragging");
    });
    el("retouchCloneSample").addEventListener("pointermove", (event) => {
      if (state.sampleDragging !== event.pointerId) return; event.preventDefault(); moveCloneSample(event);
    });
    const stopSampleDrag = (event) => {
      if (state.sampleDragging !== event.pointerId) return; moveCloneSample(event); state.sampleDragging = null;
      el("retouchCloneSample").classList.remove("dragging"); setHint("取样点已调整，请在目标位置按住并涂抹");
    };
    el("retouchCloneSample").addEventListener("pointerup", stopSampleDrag);
    el("retouchCloneSample").addEventListener("pointercancel", stopSampleDrag);
    window.addEventListener("keydown", (event) => {
      if (el("retouchEditor").hidden || /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName) || event.target.isContentEditable) return;
      if (state.busy) return;
      const command = event.ctrlKey || event.metaKey, key = event.key.toLowerCase();
      if (!command && !event.altKey && event.code === "Space") { event.preventDefault(); state.spacePressed = true; dropArea.classList.add("pan-ready"); return; }
      if (key === "control") { startCompare(); return; }
      if (command && key === "z") { event.preventDefault(); event.shiftKey ? redo() : undo(); return; }
      if (command && key === "y") { event.preventDefault(); redo(); return; }
      if (command && key === "s") { event.preventDefault(); download(); return; }
      if (command && event.key === "0") { event.preventDefault(); fitToView(); return; }
      if (command && event.key === "1") { event.preventDefault(); setZoom(1); return; }
      if (command && (event.key === "+" || event.key === "=")) { event.preventDefault(); setZoom(state.zoom * 1.25); return; }
      if (command && event.key === "-") { event.preventDefault(); setZoom(state.zoom / 1.25); return; }
      if (!command && state.regionAdjusting && state.regionSelectedHandle && ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) {
        event.preventDefault(); const step = event.shiftKey ? 10 : 1;
        const dx = event.key === "ArrowLeft" ? -step : (event.key === "ArrowRight" ? step : 0), dy = event.key === "ArrowUp" ? -step : (event.key === "ArrowDown" ? step : 0);
        if (nudgeSelectedRegionHandle(dx, dy)) setHint("已用方向键精确调整控制点" + (event.shiftKey ? " 10" : " 1") + " 像素"); return;
      }
      if (!command && (event.key === "Delete" || event.key === "Backspace") && state.regionAdjusting && state.region?.type === "polygon" && state.regionSelectedVertex != null) {
        event.preventDefault();
        if (state.region.points.length <= 3) { setHint("多边形至少需要保留 3 个端点"); return; }
        state.region.points.splice(state.regionSelectedVertex, 1); state.regionSelectedHandle = null; state.regionSelectedVertex = null; state.regionHandleDrag = null; state.drawing = false; hideMagnifier(); renderRegion(); setHint("选中的端点已删除"); return;
      }
      if (!command && event.key === "Enter" && state.regionTool === "polygon" && state.regionPoints.length >= 3) { event.preventDefault(); finishRegion({ type: "polygon", points: state.regionPoints.slice() }); state.drawing = false; return; }
      if (!command && event.key === "Escape" && (state.regionTool || state.regionAdjusting)) { event.preventDefault(); state.regionTool = ""; state.regionAdjusting = false; state.regionHandleDrag = null; state.regionSelectedHandle = null; state.regionSelectedVertex = null; state.regionPoints = []; state.drawing = false; hideMagnifier(); overlay().style.pointerEvents = state.tool === "cutout" ? "none" : "auto"; renderRegion(); updateCanvasInfo(); return; }
      if (!command && !event.altKey && key === "j") { event.preventDefault(); setTool("erase"); return; }
      if (!command && !event.altKey && key === "h") { event.preventDefault(); setTool("pan"); return; }
      if (!command && !event.altKey && key === "r") { event.preventDefault(); setTool("rect"); return; }
      if (!command && !event.altKey && key === "s") { event.preventDefault(); setTool("clone"); return; }
      if (!command && event.key === "[") { event.preventDefault(); changeBrush(1 / 1.2); return; }
      if (!command && event.key === "]") { event.preventDefault(); changeBrush(1.2); }
    });
    window.addEventListener("keyup", (event) => { if (event.key === "Control") stopCompare(); if (event.code === "Space") { state.spacePressed = false; dropArea.classList.remove("pan-ready"); } });
    window.addEventListener("blur", () => { stopCompare(); state.spacePressed = false; stopPan({}); dropArea.classList.remove("pan-ready"); });
    window.addEventListener("beforeunload", (event) => { if (state.cutoutProcessing) { event.preventDefault(); event.returnValue = ""; } });
  }
  window.KuaimaRetouchEditor = { open, openEmpty, close }; init();
})();
