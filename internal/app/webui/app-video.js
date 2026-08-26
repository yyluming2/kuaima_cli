(function () {
  const form = document.querySelector("#videoForm");
  if (!form) return;
  const mode = document.querySelector("#videoMode");
  const status = document.querySelector("#videoStatus");
  const previews = document.querySelector("#videoImagePreviews");
  const referencesInput = document.querySelector("#videoReferences");
  const submit = document.querySelector("#videoSubmit");
  const historyPanel = document.querySelector("#videoHistory");
  const durationInput = document.querySelector("#videoDuration");
  const ratioInput = document.querySelector("#videoRatio");
  const resolutionInput = document.querySelector("#videoResolution");
  const modelInput = form.elements.model;
  const imageModelInput = document.querySelector("#imageModel");
  let videoModels = [];
  let imageModels = [];
  const faceReferenceWarning = document.querySelector("#videoFaceReferenceWarning");
  const faceReferenceWarningClose = document.querySelector("#videoFaceReferenceWarningClose");
  const faceReferenceWarningDismissedKey = "kuaima.webui.video.face-reference-warning-dismissed";
  let faceReferenceWarningDismissed = localStorage.getItem(faceReferenceWarningDismissedKey) === "1";
  const legacyHistoryKey = "kuaima.webui.video.history.v1";
  const dbName = "kuaima-webui-video-history";
  const storeName = "tasks";
  let referenceFiles = [];
  let previewURLs = [];
  let historyPreviewURLs = [];
  let draggedReferenceIndex = -1;
  let polling = new Set();
  let dbPromise;
  const sketchOriginals = new WeakMap();
  let sketchProcessingKey = "";

  function setStatus(text, error) {
    status.textContent = text || "";
    status.className = "status" + (error ? " error" : "");
    const globalStatus = document.querySelector("#genStatus");
    if (globalStatus && !document.querySelector("#videoPane").hidden) {
      globalStatus.textContent = text || "准备生成视频";
      globalStatus.className = "status gen-status" + (error ? " error" : "");
    }
  }
  function statusLabel(value) {
    return ({ queued: "排队中", pending: "等待中", in_progress: "生成中", processing: "处理中", completed: "已完成", succeeded: "已完成", failed: "失败", cancelled: "已取消", canceled: "已取消" })[String(value || "").toLowerCase()] || "等待中";
  }
  function elapsedLabel(ms) {
    if (!Number.isFinite(ms) || ms < 0) return "";
    const seconds = Math.round(ms / 1000);
    if (seconds < 60) return seconds + " 秒";
    return Math.floor(seconds / 60) + " 分 " + (seconds % 60) + " 秒";
  }
  function syncReferenceInput() {
    const transfer = new DataTransfer();
    referenceFiles.forEach((file) => transfer.items.add(file));
    referencesInput.files = transfer.files;
  }
  function isSeedanceModel() { return /seedance/i.test(String(modelInput.value || "")); }
  function syncFaceReferenceWarning() {
    faceReferenceWarning.hidden = faceReferenceWarningDismissed || !isSeedanceModel();
  }
  function loadSketchImage(file) {
    return new Promise((resolve, reject) => {
      const image = new Image();
      const url = URL.createObjectURL(file);
      image.onload = () => { URL.revokeObjectURL(url); resolve(image); };
      image.onerror = () => { URL.revokeObjectURL(url); reject(new Error("图片读取失败：" + file.name)); };
      image.src = url;
    });
  }
  function canvasBlob(canvas) {
    return new Promise((resolve, reject) => canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error("素描图片生成失败")), "image/jpeg", .9));
  }
  async function sketchFile(file) {
    const image = await loadSketchImage(file);
    const maxSide = 2048;
    const scale = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
    const width = Math.max(1, Math.round(image.naturalWidth * scale));
    const height = Math.max(1, Math.round(image.naturalHeight * scale));
    const canvas = document.createElement("canvas"); canvas.width = width; canvas.height = height;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    context.drawImage(image, 0, 0, width, height);
    const pixels = context.getImageData(0, 0, width, height); const data = pixels.data;
    const gray = new Uint8ClampedArray(width * height);
    for (let index = 0; index < gray.length; index++) { const offset = index * 4; gray[index] = Math.round(data[offset] * .299 + data[offset + 1] * .587 + data[offset + 2] * .114); }
    const sample = (x, y) => gray[Math.max(0, Math.min(height - 1, y)) * width + Math.max(0, Math.min(width - 1, x))];
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const gx = -sample(x - 1, y - 1) + sample(x + 1, y - 1) - 2 * sample(x - 1, y) + 2 * sample(x + 1, y) - sample(x - 1, y + 1) + sample(x + 1, y + 1);
      const gy = -sample(x - 1, y - 1) - 2 * sample(x, y - 1) - sample(x + 1, y - 1) + sample(x - 1, y + 1) + 2 * sample(x, y + 1) + sample(x + 1, y + 1);
      const tone = Math.max(0, 255 - Math.min(255, Math.hypot(gx, gy) * 1.35)); const offset = (y * width + x) * 4;
      data[offset] = data[offset + 1] = data[offset + 2] = tone; data[offset + 3] = 255;
    }
    context.putImageData(pixels, 0, 0);
    const blob = await canvasBlob(canvas); const base = String(file.name || "reference").replace(/\.[^.]*$/, "").replace(/-sketch$/, "");
    return new File([blob], base + "-sketch.jpg", { type: "image/jpeg", lastModified: Date.now() });
  }
  function replaceInputFile(input, file) { const transfer = new DataTransfer(); if (file) transfer.items.add(file); input.files = transfer.files; }
  function replaceEntryFile(field, index, file) {
    if (field === "references") { referenceFiles[index] = file; syncReferenceInput(); }
    else replaceInputFile(form.elements[field], file);
  }
  async function aiSketchFile(file) {
    const data = new FormData();
    data.set("prompt", "将这张参考图片转换为清晰自然的黑白铅笔素描，保留原始人物身份特征、五官、姿态、构图和画面比例，不添加文字，不改变主体。");
    data.set("image_model", (document.querySelector('#genForm [name="image_model"]') || {}).value || "gpt-image-2");
    data.set("image_endpoint", "response"); data.set("image_count", "1"); data.set("image_size", "1024x1024"); data.set("image_quality", "medium");
    data.append("images", file, file.name || "reference-image.png");
    const result = await api("/api/generate", { method: "POST", body: data });
    const url = result.images && result.images[0]; if (!url) throw new Error("AI 未返回素描图片");
    const response = await fetch(url); if (!response.ok) throw new Error("AI 素描图片下载失败");
    const blob = await response.blob(); const base = String(file.name || "reference").replace(/\.[^.]*$/, "").replace(/-(ai-)?sketch$/, "");
    return new File([blob], base + "-ai-sketch." + (blob.type.includes("jpeg") ? "jpg" : "png"), { type: blob.type || "image/png", lastModified: Date.now() });
  }
  async function transformEntry(field, index, method) {
    const file = field === "references" ? referenceFiles[index] : form.elements[field].files[0]; if (!file) return;
    const key = field + ":" + index; sketchProcessingKey = key; renderPreviews();
    setStatus(method === "ai" ? "正在用 AI 转换该图片为素描..." : "正在本地转换该图片为素描...");
    try {
      const original = sketchOriginals.get(file) || file;
      const transformed = method === "ai" ? await aiSketchFile(file) : await sketchFile(file);
      sketchOriginals.set(transformed, original); replaceEntryFile(field, index, transformed);
      setStatus(method === "ai" ? "AI 素描已替换，可撤销恢复原图" : "快速素描已替换，可撤销恢复原图");
    } catch (error) { setStatus(error.message || "转素描失败", true); } finally { sketchProcessingKey = ""; renderPreviews(); }
  }
  function undoEntrySketch(field, index) {
    const file = field === "references" ? referenceFiles[index] : form.elements[field].files[0]; const original = file && sketchOriginals.get(file); if (!original) return;
    replaceEntryFile(field, index, original); renderPreviews(); setStatus("已撤销素描转换，恢复原图");
  }
  function activeFiles() {
    if (mode.value === "frames") return [document.querySelector("#videoFirstFrame"), document.querySelector("#videoLastFrame")].flatMap((input) => Array.from(input.files || []).map((file) => ({ file, field: input.name })));
    const result = referenceFiles.map((file, index) => ({ file, field: "references", referenceIndex: index }));
    if (mode.value === "edit") Array.from(document.querySelector("#videoEditInput").files || []).forEach((file) => result.unshift({ file, field: "video" }));
    return result;
  }
  function syncMode() {
    document.querySelector("#videoFramesFields").hidden = mode.value !== "frames";
    document.querySelector("#videoReferencesField").hidden = mode.value === "frames";
    document.querySelector("#videoEditField").hidden = mode.value !== "edit";
    document.querySelector("#videoReferenceUpload").hidden = mode.value === "frames";
    syncFaceReferenceWarning();
    renderPreviews();
  }
  function syncDuration() {
    document.querySelector("#videoDurationValue").textContent = durationInput.value + " 秒";
    syncVideoModelInfo();
  }
  function parseDuration(value) {
    const parts = String(value || "").match(/^\s*(\d+)\s*-\s*(\d+)\s*$/);
    return parts ? [Number(parts[1]), Number(parts[2])] : [4, 30];
  }
  function modelResolutions(model) {
    const info = model && Array.isArray(model.price_info) ? model.price_info[0] : null;
    return info && typeof info === "object" ? Object.keys(info) : [];
  }
  function modelInfo(model) {
    if (!model) return "";
    const info = Array.isArray(model.price_info) ? model.price_info[0] : null;
    const prices = info && typeof info === "object" ? Object.entries(info).map(([name, price]) => name.toUpperCase() + " ¥" + price).join(" / ") : "";
    return [model.description || "", prices ? "价格：" + prices : ""].filter(Boolean).join(" · ");
  }
  function syncVideoModelInfo() {
    const selected = videoModels.find((item) => item.model_name === modelInput.value);
    if (!selected) return;
    const info = Array.isArray(selected.price_info) ? selected.price_info[0] : null;
    const unitPrice = info && typeof info === "object" ? Number(info[resolutionInput.value]) : NaN;
    const duration = Number(durationInput.value);
    const estimate = Number.isFinite(unitPrice) && Number.isFinite(duration)
      ? "预估价格：¥" + (unitPrice * duration).toFixed(2).replace(/\.00$/, "") + "（" + resolutionInput.value.toUpperCase() + " · " + duration + " 秒）"
      : "";
    document.querySelector("#videoModelInfo").textContent = [selected.description || "", estimate].filter(Boolean).join(" · ");
  }
  function syncImageModelInfo() {
    const selected = imageModels.find((item) => item.model_name === imageModelInput.value);
    document.querySelector("#imageModelInfo").textContent = modelInfo(selected);
  }
  function syncModelOptions() {
    const selected = videoModels.find((item) => item.model_name === modelInput.value) || videoModels[0];
    if (!selected) return;
    if (modelInput.value !== selected.model_name) modelInput.value = selected.model_name;
    const [min, max] = parseDuration(selected.duration);
    durationInput.min = String(min); durationInput.max = String(max);
    durationInput.value = String(Math.min(max, Math.max(min, Number(durationInput.value) || min)));
    const previousResolution = resolutionInput.value;
    const resolutions = modelResolutions(selected);
    resolutionInput.innerHTML = resolutions.map((value) => '<option value="' + esc(value) + '">' + esc(value.toUpperCase()) + '</option>').join("");
    if (resolutions.includes(previousResolution)) resolutionInput.value = previousResolution;
    syncDuration(); syncResolutionAccess(); renderPreviews();
  }
  async function loadPricingModels() {
    const payload = await api("/api/ipricing");
    const data = payload && payload.data;
    const models = data && data.video;
    if (!Array.isArray(models) || !models.length) throw new Error("接口没有返回视频模型");
    const previous = modelInput.value;
    videoModels = models.filter((item) => item && item.model_name);
    if (!videoModels.length) throw new Error("接口没有返回有效的视频模型");
    modelInput.innerHTML = videoModels.map((item) => '<option value="' + esc(item.model_name) + '" title="' + esc(item.description || "") + '">' + esc(item.model_name) + '</option>').join("");
    if (videoModels.some((item) => item.model_name === previous)) modelInput.value = previous;
    syncModelOptions();
    const imagePrevious = imageModelInput.value;
    imageModels = Array.isArray(data.img) ? data.img.filter((item) => item && item.model_name) : [];
    if (imageModels.length) {
      imageModelInput.innerHTML = imageModels.map((item) => '<option value="' + esc(item.model_name) + '">' + esc(item.model_name) + '</option>').join("");
      if (imageModels.some((item) => item.model_name === imagePrevious)) imageModelInput.value = imagePrevious;
      syncImageModelInfo();
    }
  }
  function syncRatioPreview() {
    const parts = String(ratioInput.value || "16:9").split(":").map(Number);
    const ratio = parts[0] > 0 && parts[1] > 0 ? parts[0] / parts[1] : 16 / 9;
    const preview = document.querySelector("#videoRatioPreview");
    if (ratio >= 1) { preview.style.width = "44px"; preview.style.height = Math.max(12, 44 / ratio) + "px"; }
    else { preview.style.height = "38px"; preview.style.width = Math.max(12, 38 * ratio) + "px"; }
    preview.title = ratioInput.value ? "视频比例 " + ratioInput.value : "自动比例示意";
  }
  function syncResolutionAccess() {
    resolutionInput.disabled = resolutionInput.options.length === 0;
    document.querySelector("#videoResolutionNote").textContent = resolutionInput.disabled ? "当前模型没有可用分辨率。" : "分辨率由当前视频模型决定。";
  }
  function renderPreviews() {
    previewURLs.forEach(URL.revokeObjectURL);
    const entries = activeFiles();
    previewURLs = entries.map((entry) => URL.createObjectURL(entry.file));
    previews.classList.toggle("empty", entries.length === 0);
    syncFaceReferenceWarning();
    if (!entries.length) { previews.textContent = "上传素材后可预览、删除和调整顺序"; return; }
    previews.innerHTML = entries.map((entry, index) => {
      const file = entry.file;
      const preview = file.type.startsWith("image/") ? '<button class="video-preview-button" type="button" data-video-image-preview aria-label="放大预览 ' + esc(file.name) + '"><img src="' + previewURLs[index] + '" alt="参考素材"></button>' :
        file.type.startsWith("video/") ? '<video src="' + previewURLs[index] + '" muted></video>' : '<div class="video-file-icon">声音</div>';
      const draggable = entry.field === "references";
      const entryKey = entry.field + ":" + (entry.referenceIndex == null ? 0 : entry.referenceIndex);
      const imageActions = file.type.startsWith("image/") && isSeedanceModel() ? '<button class="btn small" type="button" data-video-sketch="local" data-video-field="' + esc(entry.field) + '" data-video-entry-index="' + (entry.referenceIndex == null ? 0 : entry.referenceIndex) + '"' + (sketchProcessingKey ? ' disabled' : '') + '><i data-lucide="pencil" aria-hidden="true"></i><span>' + (sketchProcessingKey === entryKey ? '转换中' : '快速素描') + '</span></button>' +
        '<button class="btn small" type="button" data-video-sketch="ai" data-video-field="' + esc(entry.field) + '" data-video-entry-index="' + (entry.referenceIndex == null ? 0 : entry.referenceIndex) + '"' + (sketchProcessingKey ? ' disabled' : '') + '><i data-lucide="sparkles" aria-hidden="true"></i><span>AI 转素描</span></button>' +
        (sketchOriginals.has(file) ? '<button class="btn small" type="button" data-video-sketch="undo" data-video-field="' + esc(entry.field) + '" data-video-entry-index="' + (entry.referenceIndex == null ? 0 : entry.referenceIndex) + '"' + (sketchProcessingKey ? ' disabled' : '') + '><i data-lucide="undo-2" aria-hidden="true"></i><span>撤销</span></button>' : '') : '';
      return '<div class="video-preview-item" ' + (draggable ? 'draggable="true" data-video-reference-index="' + entry.referenceIndex + '"' : '') + '>' + preview +
        '<div class="video-preview-meta"><strong>' + esc(file.name) + '</strong><span>' + (entry.field === "video" ? "待编辑视频" : entry.field === "first_frame" ? "首帧" : entry.field === "last_frame" ? "尾帧" : "参考素材 " + (entry.referenceIndex + 1)) + '</span></div>' +
        '<div class="video-preview-actions">' + imageActions + '<button class="icon-btn" type="button" data-video-remove="' + esc(entry.field) + '" data-video-remove-index="' + (entry.referenceIndex == null ? "" : entry.referenceIndex) + '" aria-label="删除素材" title="删除"><i data-lucide="trash-2" aria-hidden="true"></i></button></div></div>';
    }).join("");
    if (window.lucide) window.lucide.createIcons();
  }
  function openDB() {
    if (!dbPromise) dbPromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(dbName, 1);
      request.onupgradeneeded = () => request.result.createObjectStore(storeName, { keyPath: "task_id" });
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return dbPromise;
  }
  async function historyTransaction(modeName, action) {
    const db = await openDB();
    return new Promise((resolve, reject) => { const tx = db.transaction(storeName, modeName); const result = action(tx.objectStore(storeName)); tx.oncomplete = () => resolve(result); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error); });
  }
  async function historyItems() {
    const db = await openDB();
    return new Promise((resolve, reject) => { const request = db.transaction(storeName).objectStore(storeName).getAll(); request.onsuccess = () => resolve(request.result.sort((a, b) => String(b.created_at || "").localeCompare(String(a.created_at || "")))); request.onerror = () => reject(request.error); });
  }
  async function saveTask(task, extra) {
    const id = String(task.task_id || extra.task_id || "");
    const existing = await new Promise(async (resolve, reject) => { const db = await openDB(); const request = db.transaction(storeName).objectStore(storeName).get(id); request.onsuccess = () => resolve(request.result || {}); request.onerror = () => reject(request.error); });
    const item = Object.assign({}, existing, extra || {}, task, { task_id: id });
    await historyTransaction("readwrite", (store) => store.put(item)); await renderHistory(); return item;
  }
  async function deleteTask(id) { await historyTransaction("readwrite", (store) => store.delete(id)); await renderHistory(); }
  async function migrateLegacyHistory() {
    let items = []; try { items = JSON.parse(localStorage.getItem(legacyHistoryKey) || "[]"); } catch (_) {}
    if (!Array.isArray(items) || !items.length) return;
    await historyTransaction("readwrite", (store) => items.forEach((item) => item && item.task_id && store.put(item))); localStorage.removeItem(legacyHistoryKey);
  }
  function savedFiles() {
    const fields = ["first_frame", "last_frame", "references", "video"];
    return fields.flatMap((field) => Array.from(form.elements[field].files || []).map((file) => ({ field, name: file.name, type: file.type, lastModified: file.lastModified, blob: file })));
  }
  function savedParams() { return { prompt: form.elements.prompt.value, mode: mode.value, model: form.elements.model.value, duration: form.elements.duration.value, size: form.elements.size.value, ratio: form.elements.ratio.value, audio: form.elements.audio.checked, watermark: form.elements.watermark.checked }; }
  function setFiles(input, records) { const transfer = new DataTransfer(); records.forEach((record) => transfer.items.add(new File([record.blob], record.name, { type: record.type, lastModified: record.lastModified }))); input.files = transfer.files; }
  function reuseTask(item) {
    const params = item.params || item;
    form.elements.prompt.value = params.prompt || ""; mode.value = params.mode || "references"; form.elements.model.value = params.model || ""; form.elements.duration.value = params.duration || ""; form.elements.size.value = params.size || ""; form.elements.ratio.value = params.ratio || ""; form.elements.audio.checked = params.audio !== false; form.elements.watermark.checked = params.watermark === true;
    ["first_frame", "last_frame", "video"].forEach((field) => setFiles(form.elements[field], (item.files || []).filter((file) => file.field === field)));
    setFiles(referencesInput, (item.files || []).filter((file) => file.field === "references")); referenceFiles = Array.from(referencesInput.files); syncMode(); syncModelOptions(); syncRatioPreview(); form.scrollIntoView({ behavior: "smooth", block: "start" }); setStatus("已复用历史参数和参考文件");
  }
  async function renderHistory() {
    const items = await historyItems();
    historyPreviewURLs.forEach(URL.revokeObjectURL); historyPreviewURLs = [];
    historyPanel.innerHTML = items.length ? items.map((item) => {
      const params = item.params || item; const fileCount = (item.files || []).length;
      const elapsed = elapsedLabel(Number(item.elapsed_ms));
      const imagePreviews = (item.files || []).filter((file) => String(file.type || "").startsWith("image/") && file.blob instanceof Blob).map((file) => {
        const url = URL.createObjectURL(file.blob); historyPreviewURLs.push(url);
        return '<button class="video-history-preview" type="button" data-video-history-image aria-label="预览并编辑 ' + esc(file.name || "参考图片") + '"><img src="' + url + '" alt="' + esc(file.name || "参考图片") + '"><span>' + esc(file.field === "first_frame" ? "首帧" : file.field === "last_frame" ? "尾帧" : "参考图") + '</span></button>';
      }).join("");
      return '<article class="video-history-item"><div class="video-history-head"><strong>' + esc(params.prompt || "视频任务") + '</strong><span class="video-status ' + esc(String(item.status || "queued").toLowerCase()) + '">' + esc(statusLabel(item.status)) + '</span></div>' +
        '<small>' + esc(item.created_at ? new Date(item.created_at).toLocaleString() : "") + '</small><small>' + esc([params.model, params.duration ? params.duration + " 秒" : "", params.ratio ? "比例 " + params.ratio : "自动比例", params.size, fileCount ? fileCount + " 个素材" : "", elapsed ? "耗时 " + elapsed : ""].filter(Boolean).join(" · ")) + '</small>' +
        (imagePreviews ? '<div class="video-history-previews">' + imagePreviews + '</div>' : '') +
        '<div class="video-history-actions"><button class="btn small" type="button" data-video-open="' + esc(item.task_id) + '">' + (item.url ? "查看" : "继续轮询") + '</button><button class="btn small" type="button" data-video-reuse="' + esc(item.task_id) + '">复用</button><button class="btn small danger" type="button" data-video-delete="' + esc(item.task_id) + '">删除</button></div></article>';
    }).join("") : '<div class="history-empty">暂无视频任务</div>';
  }
  function showResult(result, autoDownload) {
    if (!result.url) throw new Error("任务已完成，但没有返回视频地址");
    const player = document.querySelector("#videoPlayer"); const download = document.querySelector("#videoDownload"); player.src = result.url; download.href = "/api/video/download/" + encodeURIComponent(result.task_id);
    const meta = result.metadata || {}; const dimensions = meta.width && meta.height ? meta.width + "×" + meta.height : "";
    document.querySelector("#videoMetadata").textContent = [result.format, meta.duration ? meta.duration + " 秒" : "", dimensions].filter(Boolean).join(" · ");
    document.querySelector("#videoTiming").textContent = result.elapsed_ms ? "本次生成耗时 " + elapsedLabel(Number(result.elapsed_ms)) : "";
    document.querySelector("#videoResult").hidden = false; player.load();
    if (autoDownload) { const frame = document.createElement("iframe"); frame.hidden = true; frame.src = download.href; document.body.appendChild(frame); setTimeout(() => frame.remove(), 60000); }
  }
  async function waitForTask(taskID, startedAt, autoDownload) {
    if (!taskID) throw new Error("视频 API 未返回 task_id"); if (polling.has(taskID)) return; polling.add(taskID);
    let latestStatus = "queued";
    const updateTimer = () => { const elapsedMs = Date.now() - startedAt; setStatus((latestStatus === "in_progress" || latestStatus === "processing" ? "视频生成中，已用时 " : "任务排队中，已用时 ") + elapsedLabel(elapsedMs)); };
    const timer = setInterval(updateTimer, 1000); updateTimer();
    try { for (;;) {
      const task = await api("/api/video/status/" + encodeURIComponent(taskID));
      latestStatus = String(task.status || "queued").toLowerCase(); const elapsedMs = Date.now() - startedAt; task.elapsed_ms = elapsedMs; await saveTask(task, { elapsed_ms: elapsedMs });
      if (["completed", "succeeded"].includes(latestStatus)) { showResult(task, autoDownload); setStatus("视频生成完成，耗时 " + elapsedLabel(elapsedMs)); return task; }
      if (latestStatus === "failed") throw new Error(task.error && task.error.message || "视频生成失败");
      if (["cancelled", "canceled"].includes(latestStatus)) throw new Error("视频任务已取消");
      updateTimer(); await new Promise((resolve) => setTimeout(resolve, 10000));
    } } finally { clearInterval(timer); polling.delete(taskID); }
  }
  mode.addEventListener("change", syncMode);
  modelInput.addEventListener("change", syncModelOptions);
  faceReferenceWarningClose.addEventListener("click", () => {
    faceReferenceWarningDismissed = true;
    localStorage.setItem(faceReferenceWarningDismissedKey, "1");
    faceReferenceWarning.hidden = true;
  });
  imageModelInput.addEventListener("change", syncImageModelInfo);
  durationInput.addEventListener("input", syncDuration);
  resolutionInput.addEventListener("change", syncVideoModelInfo);
  ratioInput.addEventListener("change", syncRatioPreview);
  window.addEventListener("kuaima:account", syncResolutionAccess);
  ["#videoFirstFrame", "#videoLastFrame", "#videoEditInput"].forEach((selector) => document.querySelector(selector).addEventListener("change", renderPreviews));
  referencesInput.addEventListener("change", () => { referenceFiles = referenceFiles.concat(Array.from(referencesInput.files || [])); syncReferenceInput(); renderPreviews(); });
  previews.addEventListener("click", (event) => {
    const sketch = event.target.closest("[data-video-sketch]");
    if (sketch) { const field = sketch.dataset.videoField; const index = Number(sketch.dataset.videoEntryIndex || 0); if (sketch.dataset.videoSketch === "undo") undoEntrySketch(field, index); else transformEntry(field, index, sketch.dataset.videoSketch); return; }
    const imageButton = event.target.closest("[data-video-image-preview]"); if (imageButton) { openImagePreview(imageButton); return; }
    const button = event.target.closest("[data-video-remove]"); if (!button) return; const field = button.dataset.videoRemove; if (field === "references") { referenceFiles.splice(Number(button.dataset.videoRemoveIndex), 1); syncReferenceInput(); } else { form.elements[field].value = ""; } renderPreviews();
  });
  previews.addEventListener("dragstart", (event) => { const item = event.target.closest("[data-video-reference-index]"); if (!item) return; draggedReferenceIndex = Number(item.dataset.videoReferenceIndex); item.classList.add("dragging"); event.dataTransfer.effectAllowed = "move"; });
  previews.addEventListener("dragover", (event) => { if (draggedReferenceIndex >= 0 && event.target.closest("[data-video-reference-index]")) { event.preventDefault(); event.dataTransfer.dropEffect = "move"; } });
  previews.addEventListener("drop", (event) => { const item = event.target.closest("[data-video-reference-index]"); if (!item || draggedReferenceIndex < 0) return; event.preventDefault(); const target = Number(item.dataset.videoReferenceIndex); const moved = referenceFiles.splice(draggedReferenceIndex, 1)[0]; referenceFiles.splice(target, 0, moved); draggedReferenceIndex = -1; syncReferenceInput(); renderPreviews(); });
  previews.addEventListener("dragend", () => { draggedReferenceIndex = -1; renderPreviews(); });
  document.querySelector("#toggleVideoHistory").addEventListener("click", () => { const sidebar = document.querySelector("#videoHistorySidebar"); const collapsed = !sidebar.classList.contains("collapsed"); sidebar.classList.toggle("collapsed", collapsed); const button = document.querySelector("#toggleVideoHistory"); button.setAttribute("aria-expanded", String(!collapsed)); button.setAttribute("aria-label", collapsed ? "展开视频历史记录" : "收起视频历史记录"); button.innerHTML = iconHTML(collapsed ? "panel-left-open" : "panel-left-close"); });
  historyPanel.addEventListener("click", async (event) => {
    const imageButton = event.target.closest("[data-video-history-image]"); if (imageButton) { openImagePreview(imageButton); return; }
    const button = event.target.closest("[data-video-open],[data-video-reuse],[data-video-delete]"); if (!button) return;
    const id = button.dataset.videoOpen || button.dataset.videoReuse || button.dataset.videoDelete; const item = (await historyItems()).find((value) => value.task_id === id); if (!item) return;
    if (button.dataset.videoReuse) return reuseTask(item); if (button.dataset.videoDelete) { if (window.confirm("确定删除这条视频历史记录吗？")) await deleteTask(id); return; }
    if (item.url) { showResult(item, false); setStatus("已打开历史视频"); } else waitForTask(id, item.started_at || Date.parse(item.created_at) || Date.now(), false).catch((error) => setStatus(error.message, true));
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (mode.value === "frames" && !form.elements.first_frame.files.length && !form.elements.last_frame.files.length) { setStatus("首帧或尾帧至少选择一张", true); return; }
    if (mode.value === "edit" && !form.elements.video.files.length) { setStatus("请选择待编辑视频", true); return; }
    submit.disabled = true; setStatus("正在提交视频任务..."); document.querySelector("#videoResult").hidden = true; const startedAt = Date.now();
    try { const params = savedParams(); const files = savedFiles(); const data = new FormData(form); if (!form.elements.audio.checked) data.set("audio", "false"); if (!form.elements.watermark.checked) data.set("watermark", "false");
      const task = await api("/api/video/generate", { method: "POST", body: data }); task.elapsed_ms = Date.now() - startedAt;
      await saveTask(task, { prompt: params.prompt, mode: params.mode, params, files, created_at: new Date(startedAt).toISOString(), started_at: startedAt, elapsed_ms: task.elapsed_ms });
      if (["completed", "succeeded"].includes(String(task.status || "").toLowerCase())) { showResult(task, true); setStatus("视频生成完成，耗时 " + elapsedLabel(task.elapsed_ms)); } else await waitForTask(task.task_id, startedAt, true);
    } catch (error) { setStatus(error.message || "视频生成失败", true); } finally { submit.disabled = false; }
  });
  (async () => { try { await migrateLegacyHistory(); syncMode(); syncDuration(); syncRatioPreview(); syncResolutionAccess(); try { await loadPricingModels(); } catch (error) { setStatus("模型配置加载失败，正在使用内置选项：" + error.message, true); } await renderHistory(); (await historyItems()).filter((item) => item.task_id && !["completed", "succeeded", "failed"].includes(item.status)).forEach((item) => waitForTask(item.task_id, item.started_at || Date.parse(item.created_at) || Date.now(), false).catch((error) => setStatus(error.message, true))); } catch (error) { setStatus("视频历史记录加载失败：" + error.message, true); syncMode(); } })();
})();
