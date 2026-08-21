(function () {
  const form = document.querySelector("#videoForm");
  if (!form) return;
  const mode = document.querySelector("#videoMode");
  const status = document.querySelector("#videoStatus");
  const previews = document.querySelector("#videoImagePreviews");
  const referencesInput = document.querySelector("#videoReferences");
  const submit = document.querySelector("#videoSubmit");
  const historyPanel = document.querySelector("#videoHistory");
  const legacyHistoryKey = "kuaima.webui.video.history.v1";
  const dbName = "kuaima-webui-video-history";
  const storeName = "tasks";
  let referenceFiles = [];
  let previewURLs = [];
  let draggedReferenceIndex = -1;
  let polling = new Set();
  let dbPromise;

  function setStatus(text, error) {
    status.textContent = text || "";
    status.className = "status" + (error ? " error" : "");
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
    renderPreviews();
  }
  function renderPreviews() {
    previewURLs.forEach(URL.revokeObjectURL);
    const entries = activeFiles();
    previewURLs = entries.map((entry) => URL.createObjectURL(entry.file));
    previews.classList.toggle("empty", entries.length === 0);
    if (!entries.length) { previews.textContent = "上传素材后可预览、删除和调整顺序"; return; }
    previews.innerHTML = entries.map((entry, index) => {
      const file = entry.file;
      const preview = file.type.startsWith("image/") ? '<img src="' + previewURLs[index] + '" alt="参考素材">' :
        file.type.startsWith("video/") ? '<video src="' + previewURLs[index] + '" muted></video>' : '<div class="video-file-icon">声音</div>';
      const draggable = entry.field === "references";
      return '<div class="video-preview-item" ' + (draggable ? 'draggable="true" data-video-reference-index="' + entry.referenceIndex + '"' : '') + '>' + preview +
        '<div class="video-preview-meta"><strong>' + esc(file.name) + '</strong><span>' + (entry.field === "video" ? "待编辑视频" : entry.field === "first_frame" ? "首帧" : entry.field === "last_frame" ? "尾帧" : "参考素材 " + (entry.referenceIndex + 1)) + '</span></div>' +
        '<button class="icon-btn" type="button" data-video-remove="' + esc(entry.field) + '" data-video-remove-index="' + (entry.referenceIndex == null ? "" : entry.referenceIndex) + '" aria-label="删除素材" title="删除"><i data-lucide="trash-2" aria-hidden="true"></i></button></div>';
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
    form.elements.prompt.value = params.prompt || ""; mode.value = params.mode || "frames"; form.elements.model.value = params.model || ""; form.elements.duration.value = params.duration || ""; form.elements.size.value = params.size || ""; form.elements.ratio.value = params.ratio || ""; form.elements.audio.checked = params.audio !== false; form.elements.watermark.checked = params.watermark === true;
    ["first_frame", "last_frame", "video"].forEach((field) => setFiles(form.elements[field], (item.files || []).filter((file) => file.field === field)));
    setFiles(referencesInput, (item.files || []).filter((file) => file.field === "references")); referenceFiles = Array.from(referencesInput.files); syncMode(); form.scrollIntoView({ behavior: "smooth", block: "start" }); setStatus("已复用历史参数和参考文件");
  }
  async function renderHistory() {
    const items = await historyItems();
    historyPanel.innerHTML = items.length ? items.map((item) => {
      const params = item.params || item; const fileCount = (item.files || []).length;
      const elapsed = elapsedLabel(Number(item.elapsed_ms));
      return '<article class="video-history-item"><div class="video-history-head"><strong>' + esc(params.prompt || "视频任务") + '</strong><span class="video-status ' + esc(String(item.status || "queued").toLowerCase()) + '">' + esc(statusLabel(item.status)) + '</span></div>' +
        '<small>' + esc(item.created_at ? new Date(item.created_at).toLocaleString() : "") + '</small><small>' + esc([params.model, params.duration ? params.duration + " 秒" : "", params.ratio ? "比例 " + params.ratio : "自动比例", params.size, fileCount ? fileCount + " 个素材" : "", elapsed ? "耗时 " + elapsed : ""].filter(Boolean).join(" · ")) + '</small>' +
        '<div class="video-history-actions"><button class="btn small" type="button" data-video-open="' + esc(item.task_id) + '">' + (item.url ? "查看" : "继续轮询") + '</button><button class="btn small" type="button" data-video-reuse="' + esc(item.task_id) + '">复用</button><button class="btn small danger" type="button" data-video-delete="' + esc(item.task_id) + '">删除</button></div></article>';
    }).join("") : '<div class="history-empty">暂无视频任务</div>';
  }
  function showResult(result, autoDownload) {
    if (!result.url) throw new Error("任务已完成，但没有返回视频地址");
    const player = document.querySelector("#videoPlayer"); const download = document.querySelector("#videoDownload"); player.src = result.url; download.href = result.url;
    const meta = result.metadata || {}; const dimensions = meta.width && meta.height ? meta.width + "×" + meta.height : "";
    document.querySelector("#videoMetadata").textContent = [result.format, meta.duration ? meta.duration + " 秒" : "", dimensions].filter(Boolean).join(" · ");
    document.querySelector("#videoTiming").textContent = result.elapsed_ms ? "本次生成耗时 " + elapsedLabel(Number(result.elapsed_ms)) : "";
    document.querySelector("#videoResult").hidden = false; player.load();
    if (autoDownload) { const link = document.createElement("a"); link.href = result.url; link.download = ""; link.target = "_blank"; link.rel = "noopener"; document.body.appendChild(link); link.click(); link.remove(); }
  }
  async function waitForTask(taskID, startedAt, autoDownload) {
    if (!taskID) throw new Error("视频 API 未返回 task_id"); if (polling.has(taskID)) return; polling.add(taskID);
    try { for (;;) {
      const task = await api("/api/video/status/" + encodeURIComponent(taskID));
      const elapsedMs = Date.now() - startedAt; task.elapsed_ms = elapsedMs; await saveTask(task, { elapsed_ms: elapsedMs });
      if (task.status === "completed") { showResult(task, autoDownload); setStatus("视频生成完成，耗时 " + elapsedLabel(elapsedMs)); return task; }
      if (task.status === "failed") throw new Error(task.error && task.error.message || "视频生成失败");
      setStatus((task.status === "in_progress" ? "视频生成中，已用时 " : "任务排队中，已用时 ") + elapsedLabel(elapsedMs)); await new Promise((resolve) => setTimeout(resolve, 2000));
    } } finally { polling.delete(taskID); }
  }
  mode.addEventListener("change", syncMode);
  ["#videoFirstFrame", "#videoLastFrame", "#videoEditInput"].forEach((selector) => document.querySelector(selector).addEventListener("change", renderPreviews));
  referencesInput.addEventListener("change", () => { referenceFiles = referenceFiles.concat(Array.from(referencesInput.files || [])); syncReferenceInput(); renderPreviews(); });
  previews.addEventListener("click", (event) => { const button = event.target.closest("[data-video-remove]"); if (!button) return; const field = button.dataset.videoRemove; if (field === "references") { referenceFiles.splice(Number(button.dataset.videoRemoveIndex), 1); syncReferenceInput(); } else { form.elements[field].value = ""; } renderPreviews(); });
  previews.addEventListener("dragstart", (event) => { const item = event.target.closest("[data-video-reference-index]"); if (!item) return; draggedReferenceIndex = Number(item.dataset.videoReferenceIndex); item.classList.add("dragging"); event.dataTransfer.effectAllowed = "move"; });
  previews.addEventListener("dragover", (event) => { if (draggedReferenceIndex >= 0 && event.target.closest("[data-video-reference-index]")) { event.preventDefault(); event.dataTransfer.dropEffect = "move"; } });
  previews.addEventListener("drop", (event) => { const item = event.target.closest("[data-video-reference-index]"); if (!item || draggedReferenceIndex < 0) return; event.preventDefault(); const target = Number(item.dataset.videoReferenceIndex); const moved = referenceFiles.splice(draggedReferenceIndex, 1)[0]; referenceFiles.splice(target, 0, moved); draggedReferenceIndex = -1; syncReferenceInput(); renderPreviews(); });
  previews.addEventListener("dragend", () => { draggedReferenceIndex = -1; renderPreviews(); });
  document.querySelector("#toggleVideoHistory").addEventListener("click", () => { const sidebar = document.querySelector("#videoHistorySidebar"); const collapsed = !sidebar.classList.contains("collapsed"); sidebar.classList.toggle("collapsed", collapsed); const button = document.querySelector("#toggleVideoHistory"); button.setAttribute("aria-expanded", String(!collapsed)); button.setAttribute("aria-label", collapsed ? "展开视频历史记录" : "收起视频历史记录"); button.innerHTML = iconHTML(collapsed ? "panel-left-open" : "panel-left-close"); });
  historyPanel.addEventListener("click", async (event) => {
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
      if (task.status === "completed") { showResult(task, true); setStatus("视频生成完成，耗时 " + elapsedLabel(task.elapsed_ms)); } else await waitForTask(task.task_id, startedAt, true);
    } catch (error) { setStatus(error.message || "视频生成失败", true); } finally { submit.disabled = false; }
  });
  (async () => { try { await migrateLegacyHistory(); syncMode(); await renderHistory(); (await historyItems()).filter((item) => item.task_id && !["completed", "failed"].includes(item.status)).forEach((item) => waitForTask(item.task_id, item.started_at || Date.parse(item.created_at) || Date.now(), false).catch((error) => setStatus(error.message, true))); } catch (error) { setStatus("视频历史记录加载失败：" + error.message, true); syncMode(); } })();
})();
