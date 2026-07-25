(function () {
  const state = { sourceURL: "", tool: "erase", drawing: false, width: 0, height: 0, zoom: 1, last: null, sample: null, cloneOffset: null, undo: [], redo: [] };
  const el = (id) => document.getElementById(id);
  const canvas = () => el("retouchCanvas");
  const overlay = () => el("retouchOverlay");
  const ctx = () => canvas().getContext("2d", { willReadFrequently: true });

  function setHint(text) { el("fineEditorHint").textContent = text; }
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
  }
  function download() {
    canvas().toBlob((blob) => {
      if (!blob) return;
      const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "retouched-" + Date.now() + ".png"; a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    }, "image/png");
  }
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
    for (let i = 0; i < unknown.length; i++) if (mask[i * 4 + 3] > 12) { unknown[i] = 1; remaining++; }
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
      let r = 0, g = 0, b = 0, count = 0;
      const around = neighbors(i);
      around.forEach((n) => { if (!unknown[n]) { r += data[n * 4]; g += data[n * 4 + 1]; b += data[n * 4 + 2]; count++; } });
      if (!count) continue;
      const o = i * 4; data[o] = r / count; data[o + 1] = g / count; data[o + 2] = b / count; data[o + 3] = 255; unknown[i] = 0; remaining--;
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
  function drawCursor(p) {
    clearOverlay(); const paint = overlay().getContext("2d");
    paint.strokeStyle = "rgba(255,255,255,.95)"; paint.lineWidth = Math.max(1, 2 / state.zoom); paint.beginPath(); paint.arc(p.x, p.y, brushSize() / 2, 0, Math.PI * 2); paint.stroke();
    paint.strokeStyle = "rgba(15,23,42,.8)"; paint.setLineDash([4 / state.zoom, 4 / state.zoom]); paint.beginPath(); paint.arc(p.x, p.y, brushSize() / 2 + 2 / state.zoom, 0, Math.PI * 2); paint.stroke();
    if (state.sample) { paint.strokeStyle = "#22c55e"; paint.setLineDash([]); paint.beginPath(); paint.moveTo(state.sample.x - 7, state.sample.y); paint.lineTo(state.sample.x + 7, state.sample.y); paint.moveTo(state.sample.x, state.sample.y - 7); paint.lineTo(state.sample.x, state.sample.y + 7); paint.stroke(); }
  }
  function setTool(tool) {
    state.tool = tool === "clone" ? "clone" : "erase"; state.sample = null; state.cloneOffset = null; clearOverlay();
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.classList.toggle("active", button.dataset.retouchTool === state.tool));
    setHint(state.tool === "erase" ? "在需要移除的物体上涂抹，松开后自动修复" : "先点击取样位置，再在目标位置涂抹；按 Alt 点击可重新取样");
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
      ctx().drawImage(image, 0, 0); state.undo = []; state.redo = []; snapshot(); resizeDisplay(); setTool("erase");
      el("imageModal").hidden = true; el("fineEditor").hidden = false;
    };
    image.onerror = () => setHint("图片读取失败，可能不允许跨域编辑"); image.src = url;
  }
  function close() { state.drawing = false; el("fineEditor").hidden = true; }
  function init() {
    document.querySelectorAll("[data-retouch-tool]").forEach((button) => button.addEventListener("click", () => setTool(button.dataset.retouchTool)));
    el("retouchZoom").addEventListener("input", (event) => setZoom(Number(event.target.value) / 100));
    el("retouchUndo").addEventListener("click", undo); el("retouchRedo").addEventListener("click", redo);
    el("downloadRetouch").addEventListener("click", download);
    overlay().addEventListener("pointerdown", (event) => {
      event.preventDefault(); const p = point(event);
      if (state.tool === "clone" && (!state.sample || event.altKey)) { state.sample = p; state.cloneOffset = null; drawCursor(p); setHint("取样完成，请在要覆盖的位置按住并涂抹"); return; }
      state.drawing = true; state.last = p; overlay().setPointerCapture(event.pointerId);
      if (state.tool === "clone") { if (!state.cloneOffset) state.cloneOffset = { x: state.sample.x - p.x, y: state.sample.y - p.y }; cloneStamp(p); } else markErase(p);
    });
    overlay().addEventListener("pointermove", (event) => {
      const p = point(event); if (!state.drawing) { if (state.tool === "clone") drawCursor(p); return; }
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
  window.KuaimaRetouchEditor = { open, close }; init();
})();
