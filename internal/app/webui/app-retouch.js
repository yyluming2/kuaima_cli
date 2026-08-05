(function () {
  const state = { sourceURL: "", sourceBytes: 0, original: null, tool: "erase", drawing: false, busy: false, width: 0, height: 0, zoom: 1, last: null, sample: null, cloneOffset: null, undo: [], redo: [], batch: [], batchDone: false, estimateTimer: 0, profileNames: { product: "商品" } };
  const el = (id) => document.getElementById(id);
  const canvas = () => el("retouchCanvas");
  const overlay = () => el("retouchOverlay");
  const ctx = () => canvas().getContext("2d", { willReadFrequently: true });

  function setHint(text) { el("fineEditorHint").textContent = text; }
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
    } catch (_) {}
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
    [canvas(), overlay()].forEach((item) => { item.style.width = w + "px"; item.style.height = h + "px"; });
  }
  function updateHistoryButtons() {
    el("retouchUndo").disabled = state.undo.length < 2;
    el("retouchRedo").disabled = !state.redo.length;
  }
  function snapshot() {
    state.undo.push(ctx().getImageData(0, 0, state.width, state.height));
    const max = Math.max(2, Math.min(12, Math.floor(96 * 1024 * 1024 / (state.width * state.height * 4))));
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
    state.zoom = Math.max(.25, Math.min(3, value));
    el("retouchZoom").value = String(Math.round(state.zoom * 100));
    resizeDisplay();
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
  function exportCanvas() {
    const scale = Number(el("retouchExportScale").value) || 1, quality = Number(el("retouchExportQuality").value) || 1;
    const out = document.createElement("canvas"); out.width = Math.max(1, Math.round(state.width * scale)); out.height = Math.max(1, Math.round(state.height * scale));
    const context = out.getContext("2d", { willReadFrequently: quality < 1 }); context.imageSmoothingQuality = "high"; context.drawImage(canvas(), 0, 0, out.width, out.height);
    if (quality < 1) { const image = context.getImageData(0, 0, out.width, out.height), step = quality >= .9 ? 2 : 6; for (let i = 0; i < image.data.length; i += 4) for (let c = 0; c < 3; c++) image.data[i + c] = Math.round(image.data[i + c] / step) * step; context.putImageData(image, 0, 0); }
    return out;
  }
  async function estimateExport() {
    if (!state.width) return; const out = exportCanvas();
    el("retouchExportEstimate").textContent = out.width + " × " + out.height + " · 估算中…";
    try { const blob = await canvasBlob(out); el("retouchExportEstimate").textContent = out.width + " × " + out.height + " · 约 " + formatBytes(blob.size); } catch (_) {}
  }
  function scheduleEstimate() { clearTimeout(state.estimateTimer); state.estimateTimer = setTimeout(estimateExport, 180); }
  async function download() {
    if (!state.width || state.busy) return; const blob = await canvasBlob(exportCanvas());
    const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "retouched-" + Date.now() + ".png"; a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }
  function setBusy(busy, batch = false) {
    state.busy = busy; el("retouchBusy").hidden = !busy || batch;
    el("retouchEditor").querySelectorAll("button,input,select").forEach((item) => { if (!item.closest(".retouch-batch")) item.disabled = busy; });
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
    if (background === "white") { context.globalCompositeOperation = "destination-over"; context.fillStyle = "#fff"; context.fillRect(0, 0, w, h); context.globalCompositeOperation = "source-over"; }
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
      const xhr = new XMLHttpRequest(); xhr.open("POST", "/api/image/remove-background"); xhr.responseType = "blob"; xhr.timeout = 9 * 60 * 1000;
      xhr.onload = () => resolve({ ok: xhr.status >= 200 && xhr.status < 300, status: xhr.status, body: xhr.response, warning: xhr.getResponseHeader("X-Kuaima-Cutout-Warning") || "" });
      xhr.onerror = () => reject(new Error("抠图连接被中断（服务仍在运行时通常是系统内存不足或安全软件终止了模型进程）"));
      xhr.ontimeout = () => reject(new Error("抠图请求超过 9 分钟"));
      xhr.onabort = () => reject(new Error("抠图请求已取消"));
      xhr.send(form);
    });
  }
  async function rembgCanvas(target, upload, xhrTransport = false) {
    const source = upload || await canvasBlob(target), filename = upload?.name || "image.png";
    const form = new FormData(); form.append("image", source, filename); form.append("profile", el("retouchCutoutProfile").value); form.append("edge", el("retouchCutoutEdge").value);
    const response = xhrTransport ? await xhrCutout(form) : await fetch("/api/image/remove-background", { method: "POST", body: form });
    if (!response.ok) {
      let message = "模型抠图失败";
      try {
        const body = xhrTransport ? JSON.parse(await response.body.text()) : await response.json();
        message = body.error || body.message || message;
      } catch (_) {}
      const error = new Error(message); error.unavailable = response.status === 503; throw error;
    }
    const image = await fileImage(xhrTransport ? response.body : await response.blob());
    const context = target.getContext("2d"); context.clearRect(0, 0, target.width, target.height); context.drawImage(image, 0, 0, target.width, target.height);
    finishCutout(target, el("retouchAutoCenter").checked, el("retouchBackground").value);
    return xhrTransport ? response.warning : response.headers.get("X-Kuaima-Cutout-Warning") || "";
  }
  function finishCutout(target, autoCenter, background) {
    const context = target.getContext("2d", { willReadFrequently: true }), w = target.width, h = target.height;
    if (autoCenter) {
      const data = context.getImageData(0, 0, w, h).data; let minX = w, minY = h, maxX = -1, maxY = -1;
      for (let i = 0; i < w * h; i++) if (data[i * 4 + 3] > 24) { const x = i % w, y = (i / w) | 0; minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y); }
      if (maxX >= minX) { const copy = document.createElement("canvas"); copy.width = w; copy.height = h; copy.getContext("2d").drawImage(target, 0, 0); context.clearRect(0, 0, w, h); context.drawImage(copy, Math.round((w - maxX + minX - 1) / 2 - minX), Math.round((h - maxY + minY - 1) / 2 - minY)); }
    }
    if (background === "white") { context.globalCompositeOperation = "destination-over"; context.fillStyle = "#fff"; context.fillRect(0, 0, w, h); context.globalCompositeOperation = "source-over"; }
  }
  async function applyCutout() {
    if (!state.width || state.busy) return;
    const profileNames = state.profileNames, profile = el("retouchCutoutProfile").value;
    setHint("正在用“" + (profileNames[profile] || "商品") + "”模式抠图...");
    setBusy(true);
    try {
      const warning = await rembgCanvas(canvas()); clearOverlay(); snapshot();
      setHint(warning === "fallback_low_memory" ? "高质量模型内存不足，已自动降级并完成抠图" : (profileNames[profile] || "商品") + "模式抠图完成；可按住“看原图”对比，或继续用画笔补修");
    } catch (error) {
      if (!error.unavailable) { setHint(error.message || "模型抠图失败，已保留当前图片"); return; }
      try { cutoutCanvas(canvas(), el("retouchAutoCenter").checked, el("retouchBackground").value); clearOverlay(); snapshot(); setHint("安装包缺少离线抠图引擎，已使用基础抠图"); }
      catch (fallbackError) { setHint(error.message || fallbackError.message); }
    } finally { setBusy(false); scheduleEstimate(); }
  }
  function fileImage(file) { return new Promise((resolve, reject) => { const image = new Image(), url = URL.createObjectURL(file); image.onload = () => { URL.revokeObjectURL(url); resolve(image); }; image.onerror = reject; image.src = url; }); }
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
    state.batch = []; state.batchDone = false; el("retouchBatch").hidden = false; el("retouchBatch").classList.remove("collapsed"); renderBatch();
    setBusy(true, true);
    for (let i = 0; i < images.length; i++) {
      const file = images[i]; el("retouchBatchStatus").textContent = (i + 1) + " / " + images.length;
      try {
        const image = await fileImage(file), out = document.createElement("canvas"); out.width = image.naturalWidth; out.height = image.naturalHeight; out.getContext("2d").drawImage(image, 0, 0);
        // Batch input is encoded at its original dimensions but constrained to
        // a safe request size. The engine output remains full-size PNG.
        const upload = await batchUploadBlob(out);
        try { await rembgCanvas(out, upload, true); } catch (error) { if (!error.unavailable) throw error; cutoutCanvas(out, el("retouchAutoCenter").checked, el("retouchBackground").value); }
        const blob = await new Promise((resolve) => out.toBlob(resolve, "image/png")); state.batch.push({ name: file.name.replace(/\.[^.]+$/, "") + "-cutout.png", blob, url: URL.createObjectURL(blob) }); renderBatch();
      } catch (error) { state.batch.push({ name: file.name, error: error?.message || "抠图失败" }); renderBatch(); }
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    const failed = state.batch.filter((item) => item.error);
    state.batchDone = true; renderBatch();
    el("retouchBatchStatus").textContent = "完成 " + state.batch.filter((item) => item.blob).length + " 张" + (failed.length ? "，失败 " + failed.length + " 张：" + failed[0].error : "");
    setBusy(false);
  }
  function renderBatch() {
    const escape = (value) => String(value || "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
    el("retouchBatchList").innerHTML = state.batch.map((item, i) => '<div class="retouch-batch-item" title="' + escape(item.error || item.name) + '">' + (item.url ? '<img src="' + item.url + '" alt="">' : '<span></span>') + '<span>' + escape(item.name) + '</span><button class="btn small" data-batch-download="' + i + '" ' + (item.error ? 'disabled' : '') + '>' + (item.error ? '失败' : '下载') + '</button></div>').join("");
    el("downloadRetouchBatch").hidden = !state.batchDone || !state.batch.some((item) => item.blob);
  }
  function saveBatchItem(item) { if (!item || !item.url) return; const a = document.createElement("a"); a.href = item.url; a.download = item.name; a.click(); }
  function clearOverlay() { overlay().getContext("2d").clearRect(0, 0, state.width, state.height); }
  function line(a, b, draw) {
    const distance = Math.hypot(b.x - a.x, b.y - a.y);
    const step = Math.max(1, brushSize() * .18);
    for (let i = 0, n = Math.max(1, Math.ceil(distance / step)); i <= n; i++) draw({ x: a.x + (b.x - a.x) * i / n, y: a.y + (b.y - a.y) * i / n });
  }
  function markErase(p) {
    const paint = overlay().getContext("2d");
    paint.fillStyle = "rgba(239,68,68,.55)"; paint.beginPath(); paint.arc(p.x, p.y, brushSize() / 2, 0, Math.PI * 2); paint.fill();
  }
  function inpaint() {
    const mask = overlay().getContext("2d").getImageData(0, 0, state.width, state.height).data;
    const image = ctx().getImageData(0, 0, state.width, state.height), data = image.data;
    const unknown = new Uint8Array(state.width * state.height);
    let remaining = 0;
    // Transparent source pixels are already empty. Keeping them out of the
    // repair mask prevents their zeroed RGB channels from becoming opaque black.
    for (let i = 0; i < unknown.length; i++) if (mask[i * 4 + 3] > 12 && data[i * 4 + 3] > 0) { unknown[i] = 1; remaining++; }
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
    ctx().drawImage(patch, p.x - radius, p.y - radius, radius * 2, radius * 2);
  }
  function setTool(tool) {
    if (state.busy) return;
    state.tool = tool === "cutout" ? "cutout" : (tool === "clone" ? "clone" : "erase"); state.sample = null; state.cloneOffset = null; clearOverlay();
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.classList.toggle("active", button.dataset.retouchTool === state.tool));
    document.querySelectorAll("[data-tool-panel]").forEach((panel) => { panel.hidden = panel.dataset.toolPanel === "cutout" ? state.tool !== "cutout" : state.tool === "cutout"; });
    overlay().style.pointerEvents = state.tool === "cutout" ? "none" : "auto";
    setHint(state.tool === "cutout" ? "选择模型和边缘设置，然后点击“开始抠图”" : (state.tool === "erase" ? "在需要移除的物体上涂抹，松开后自动修复" : "先点击取样位置，再在目标位置涂抹；按 Alt 点击可重新取样"));
  }
  function open(url) {
    if (!url) return;
    el("finePointEditorView").hidden = true; el("retouchEditor").hidden = false;
    document.querySelectorAll("[data-editor-tab]").forEach((button) => { const active = button.dataset.editorTab === "manual"; button.classList.toggle("active", active); button.setAttribute("aria-selected", String(active)); });
    if (state.sourceURL === url && state.width) { setHint(state.tool === "erase" ? "在需要移除的物体上涂抹，松开后自动修复" : "先点击取样位置，再在目标位置涂抹；按 Alt 点击可重新取样"); resizeDisplay(); return; }
    state.sourceURL = url; const image = new Image(); image.crossOrigin = "anonymous"; setHint("正在读取图片...");
    image.onload = () => {
      state.width = image.naturalWidth; state.height = image.naturalHeight;
      [canvas(), overlay()].forEach((item) => { item.width = state.width; item.height = state.height; });
      ctx().drawImage(image, 0, 0); state.original = ctx().getImageData(0, 0, state.width, state.height); state.undo = []; state.redo = []; snapshot(); resizeDisplay(); setTool(state.tool || "erase");
      el("retouchEmpty").hidden = true; el("retouchCanvasWrap").hidden = false;
      el("retouchImageInfo").textContent = state.width + " × " + state.height + " px" + (state.sourceBytes ? " · " + formatBytes(state.sourceBytes) : "");
      el("imageModal").hidden = true; el("fineEditor").hidden = false; requestAnimationFrame(() => { fitToView(); scheduleEstimate(); });
    };
    image.onerror = () => setHint("图片读取失败，可能不允许跨域编辑"); image.src = url;
  }
  function openEmpty(tool = "cutout") {
    state.sourceURL = ""; state.sourceBytes = 0; state.width = 0; state.height = 0; state.original = null; state.undo = []; state.redo = []; updateHistoryButtons();
    el("finePointEditorView").hidden = true; el("retouchEditor").hidden = false; el("fineEditor").hidden = false; el("imageModal").hidden = true;
    el("retouchEmpty").hidden = false; el("retouchCanvasWrap").hidden = true; el("retouchImageInfo").textContent = "尚未打开图片";
    document.querySelectorAll("[data-editor-tab]").forEach((button) => { const active = button.dataset.editorTab === "manual"; button.classList.toggle("active", active); button.setAttribute("aria-selected", String(active)); });
    setTool(tool); setHint("选择或拖入图片后即可开始智能抠图");
  }
  function close() { state.drawing = false; el("fineEditor").hidden = true; }
  function init() {
    loadCutoutProfiles();
    document.addEventListener("click", (event) => { if (event.target.closest("[data-cutout-tab]")) openEmpty("cutout"); });
    el("retouchEmpty").addEventListener("click", () => el("retouchSourceFiles").click());
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.addEventListener("click", () => setTool(button.dataset.retouchTool)));
    el("retouchApplyCutout").addEventListener("click", applyCutout);
    el("retouchBrushSize").addEventListener("input", updateBrushValue); updateBrushValue();
    ["retouchExportScale", "retouchExportQuality"].forEach((id) => el(id).addEventListener("change", scheduleEstimate));
    el("retouchZoom").addEventListener("input", (event) => setZoom(Number(event.target.value) / 100));
    el("retouchUndo").addEventListener("click", undo); el("retouchRedo").addEventListener("click", redo);
    el("downloadRetouch").addEventListener("click", download);
    el("retouchCompare").addEventListener("pointerdown", () => { if (state.original) ctx().putImageData(state.original, 0, 0); });
    const stopCompare = () => { if (state.undo.length) restore(state.undo[state.undo.length - 1]); };
    el("retouchCompare").addEventListener("pointerup", stopCompare); el("retouchCompare").addEventListener("pointerleave", stopCompare); el("retouchCompare").addEventListener("pointercancel", stopCompare);
    el("retouchSourceFiles").addEventListener("change", (event) => { const files = Array.from(event.target.files || []); event.target.value = ""; if (files.length === 1) { state.sourceBytes = files[0].size; open(URL.createObjectURL(files[0])); } else processBatch(files); });
    const dropArea = document.querySelector(".retouch-canvas-scroll");
    ["dragenter", "dragover"].forEach((name) => dropArea.addEventListener(name, (event) => { event.preventDefault(); el("retouchDropHint").hidden = false; }));
    ["dragleave", "drop"].forEach((name) => dropArea.addEventListener(name, (event) => { event.preventDefault(); el("retouchDropHint").hidden = true; }));
    dropArea.addEventListener("drop", async (event) => { const files = (await droppedFiles(event.dataTransfer)).filter((file) => String(file.type || "").startsWith("image/")); if (files.length === 1) { state.sourceBytes = files[0].size; open(URL.createObjectURL(files[0])); } else processBatch(files); });
    el("retouchBatchToggle").addEventListener("click", () => el("retouchBatch").classList.toggle("collapsed"));
    el("retouchBatchList").addEventListener("click", (event) => { const button = event.target.closest("[data-batch-download]"); if (button) saveBatchItem(state.batch[Number(button.dataset.batchDownload)]); });
    el("downloadRetouchBatch").addEventListener("click", async () => { for (const item of state.batch) { saveBatchItem(item); await new Promise((resolve) => setTimeout(resolve, 180)); } });
    overlay().addEventListener("pointerdown", (event) => {
      event.preventDefault(); const p = point(event);
      if (state.tool === "clone" && (!state.sample || event.altKey)) { state.sample = p; state.cloneOffset = null; setHint("取样完成，请在要覆盖的位置按住并涂抹"); return; }
      state.drawing = true; state.last = p; overlay().setPointerCapture(event.pointerId);
      if (state.tool === "clone") { if (!state.cloneOffset) state.cloneOffset = { x: state.sample.x - p.x, y: state.sample.y - p.y }; cloneStamp(p); } else markErase(p);
    });
    overlay().addEventListener("pointermove", (event) => {
      if (!state.drawing) return; const p = point(event);
      line(state.last, p, state.tool === "clone" ? cloneStamp : markErase); state.last = p;
    });
    function finish() { if (!state.drawing) return; state.drawing = false; if (state.tool === "erase") inpaint(); else clearOverlay(); snapshot(); }
    overlay().addEventListener("pointerup", finish); overlay().addEventListener("pointercancel", finish);
    window.addEventListener("keydown", (event) => {
      if (el("retouchEditor").hidden || /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName) || event.target.isContentEditable) return;
      const command = event.ctrlKey || event.metaKey, key = event.key.toLowerCase();
      if (command && key === "z") { event.preventDefault(); event.shiftKey ? redo() : undo(); return; }
      if (command && key === "y") { event.preventDefault(); redo(); return; }
      if (command && key === "s") { event.preventDefault(); download(); return; }
      if (command && event.key === "0") { event.preventDefault(); fitToView(); return; }
      if (command && event.key === "1") { event.preventDefault(); setZoom(1); return; }
      if (command && (event.key === "+" || event.key === "=")) { event.preventDefault(); setZoom(state.zoom * 1.25); return; }
      if (command && event.key === "-") { event.preventDefault(); setZoom(state.zoom / 1.25); return; }
      if (!command && !event.altKey && key === "j") { event.preventDefault(); setTool("erase"); return; }
      if (!command && !event.altKey && key === "s") { event.preventDefault(); setTool("clone"); return; }
      if (!command && event.key === "[") { event.preventDefault(); changeBrush(1 / 1.2); return; }
      if (!command && event.key === "]") { event.preventDefault(); changeBrush(1.2); }
    });
  }
  window.KuaimaRetouchEditor = { open, openEmpty, close }; init();
})();
