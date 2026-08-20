(function () {
  const form = document.querySelector("#videoForm");
  if (!form) return;
  const mode = document.querySelector("#videoMode");
  const status = document.querySelector("#videoStatus");
  const previews = document.querySelector("#videoImagePreviews");
  const submit = document.querySelector("#videoSubmit");
  const historyPanel = document.querySelector("#videoHistory");
  const legacyHistoryKey = "kuaima.webui.video.history.v1";
  const dbName = "kuaima-webui-video-history";
  const storeName = "tasks";
  let previewURLs = [];
  let polling = new Set();
  let dbPromise;

  function setStatus(text, error) {
    status.textContent = text || "";
    status.className = "status" + (error ? " error" : "");
  }
  function inputs() {
    if (mode.value === "frames") return [document.querySelector("#videoFirstFrame"), document.querySelector("#videoLastFrame")];
    if (mode.value === "edit") return [document.querySelector("#videoEditInput")];
    return [document.querySelector("#videoReferences")];
  }
  function syncMode() {
    document.querySelector("#videoFramesFields").hidden = mode.value !== "frames";
    document.querySelector("#videoReferencesField").hidden = mode.value !== "references";
    document.querySelector("#videoEditField").hidden = mode.value !== "edit";
    renderPreviews();
  }
  function renderPreviews() {
    previewURLs.forEach(URL.revokeObjectURL);
    const files = inputs().flatMap((input) => Array.from(input.files || []));
    previewURLs = files.map(URL.createObjectURL);
    previews.innerHTML = files.map((file, index) => {
      const url = previewURLs[index];
      const preview = file.type.startsWith("image/") ? '<img src="' + url + '" alt="参考素材">' :
        file.type.startsWith("video/") ? '<video src="' + url + '" muted></video>' : '<div class="video-file-icon">声音</div>';
      return '<figure>' + preview + '<figcaption>' + esc(file.name) + '</figcaption></figure>';
    }).join("");
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
    return new Promise((resolve, reject) => {
      const tx = db.transaction(storeName, modeName);
      const result = action(tx.objectStore(storeName));
      tx.oncomplete = () => resolve(result);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }
  async function historyItems() {
    const db = await openDB();
    return new Promise((resolve, reject) => {
      const request = db.transaction(storeName).objectStore(storeName).getAll();
      request.onsuccess = () => resolve(request.result.sort((a, b) => String(b.created_at || "").localeCompare(String(a.created_at || ""))));
      request.onerror = () => reject(request.error);
    });
  }
  async function saveTask(task, extra) {
    const id = String(task.task_id || extra.task_id || "");
    const existing = await new Promise(async (resolve, reject) => {
      const db = await openDB();
      const request = db.transaction(storeName).objectStore(storeName).get(id);
      request.onsuccess = () => resolve(request.result || {});
      request.onerror = () => reject(request.error);
    });
    const item = Object.assign({}, existing, extra || {}, task, { task_id: id });
    await historyTransaction("readwrite", (store) => store.put(item));
    await renderHistory();
    return item;
  }
  async function deleteTask(id) {
    await historyTransaction("readwrite", (store) => store.delete(id));
    await renderHistory();
  }
  async function migrateLegacyHistory() {
    let items = [];
    try { items = JSON.parse(localStorage.getItem(legacyHistoryKey) || "[]"); } catch (_) {}
    if (!Array.isArray(items) || !items.length) return;
    await historyTransaction("readwrite", (store) => items.forEach((item) => item && item.task_id && store.put(item)));
    localStorage.removeItem(legacyHistoryKey);
  }
  function savedFiles() {
    const fields = ["first_frame", "last_frame", "references", "video"];
    return fields.flatMap((field) => Array.from(form.elements[field].files || []).map((file) => ({
      field, name: file.name, type: file.type, lastModified: file.lastModified, blob: file
    })));
  }
  function savedParams() {
    return { prompt: form.elements.prompt.value, mode: mode.value, model: form.elements.model.value,
      duration: form.elements.duration.value, size: form.elements.size.value,
      audio: form.elements.audio.checked, watermark: form.elements.watermark.checked };
  }
  function setFiles(input, records) {
    const transfer = new DataTransfer();
    records.forEach((record) => transfer.items.add(new File([record.blob], record.name, { type: record.type, lastModified: record.lastModified })));
    input.files = transfer.files;
  }
  function reuseTask(item) {
    const params = item.params || item;
    form.elements.prompt.value = params.prompt || "";
    mode.value = params.mode || "frames";
    form.elements.model.value = params.model || "";
    form.elements.duration.value = params.duration || "";
    form.elements.size.value = params.size || "";
    form.elements.audio.checked = params.audio !== false;
    form.elements.watermark.checked = params.watermark === true;
    ["first_frame", "last_frame", "references", "video"].forEach((field) =>
      setFiles(form.elements[field], (item.files || []).filter((file) => file.field === field)));
    syncMode();
    form.scrollIntoView({ behavior: "smooth", block: "start" });
    setStatus("已复用历史参数和参考文件");
  }
  async function renderHistory() {
    const items = await historyItems();
    historyPanel.innerHTML = items.length ? items.map((item) => {
      const params = item.params || item;
      const fileCount = (item.files || []).length;
      return '<article class="video-history-item"><strong>' + esc(params.prompt || "视频任务") + '</strong>' +
        '<small>' + esc(item.created_at ? new Date(item.created_at).toLocaleString() : "") + '</small>' +
        '<small>' + esc([params.model, params.duration ? params.duration + " 秒" : "", params.size, fileCount ? fileCount + " 个参考文件" : ""].filter(Boolean).join(" · ")) + '</small>' +
        '<code>' + esc(item.task_id) + '</code><span>' + esc(item.status || "queued") + '</span>' +
        '<div class="video-history-actions"><button class="btn small" type="button" data-video-open="' + esc(item.task_id) + '">' + (item.url ? "查看" : "继续轮询") + '</button>' +
        '<button class="btn small" type="button" data-video-reuse="' + esc(item.task_id) + '">复用</button>' +
        '<button class="btn small" type="button" data-video-delete="' + esc(item.task_id) + '">删除</button></div></article>';
    }).join("") : '<div class="history-empty">暂无视频任务</div>';
  }
  function showResult(result) {
    if (!result.url) throw new Error("任务已完成，但没有返回视频地址");
    const player = document.querySelector("#videoPlayer");
    player.src = result.url;
    document.querySelector("#videoDownload").href = result.url;
    const meta = result.metadata || {};
    const dimensions = meta.width && meta.height ? meta.width + "×" + meta.height : "";
    document.querySelector("#videoMetadata").textContent = [result.format, meta.duration ? meta.duration + " 秒" : "", dimensions].filter(Boolean).join(" · ");
    document.querySelector("#videoResult").hidden = false;
    player.load();
  }
  async function waitForTask(taskID) {
    if (!taskID) throw new Error("视频 API 未返回 task_id");
    if (polling.has(taskID)) return;
    polling.add(taskID);
    try {
      for (;;) {
        const task = await api("/api/video/status/" + encodeURIComponent(taskID));
        await saveTask(task, {});
        if (task.status === "completed") { showResult(task); setStatus("视频生成完成"); return task; }
        if (task.status === "failed") throw new Error(task.error && task.error.message || "视频生成失败");
        setStatus(task.status === "in_progress" ? "视频生成中..." : "任务排队中...");
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    } finally { polling.delete(taskID); }
  }
  mode.addEventListener("change", syncMode);
  ["#videoFirstFrame", "#videoLastFrame", "#videoReferences", "#videoEditInput"].forEach((selector) =>
    document.querySelector(selector).addEventListener("change", renderPreviews));
  historyPanel.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-video-open],[data-video-reuse],[data-video-delete]");
    if (!button) return;
    const id = button.dataset.videoOpen || button.dataset.videoReuse || button.dataset.videoDelete;
    const item = (await historyItems()).find((value) => value.task_id === id);
    if (!item) return;
    if (button.dataset.videoReuse) return reuseTask(item);
    if (button.dataset.videoDelete) {
      if (window.confirm("确定删除这条视频历史记录吗？")) await deleteTask(id);
      return;
    }
    if (item.url) { showResult(item); setStatus("已打开历史视频"); }
    else waitForTask(id).catch((error) => setStatus(error.message, true));
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (mode.value === "frames" && inputs().every((input) => !input.files.length)) { setStatus("首帧或尾帧至少选择一张", true); return; }
    if (mode.value === "edit" && !inputs()[0].files.length) { setStatus("请选择待编辑视频", true); return; }
    submit.disabled = true; setStatus("正在提交视频任务...");
    document.querySelector("#videoResult").hidden = true;
    try {
      const params = savedParams();
      const files = savedFiles();
      const data = new FormData(form);
      if (!form.elements.audio.checked) data.set("audio", "false");
      if (!form.elements.watermark.checked) data.set("watermark", "false");
      const task = await api("/api/video/generate", { method: "POST", body: data });
      await saveTask(task, { prompt: params.prompt, mode: params.mode, params, files, created_at: new Date().toISOString() });
      if (task.status === "completed") { showResult(task); setStatus("视频生成完成"); }
      else await waitForTask(task.task_id);
    } catch (error) { setStatus(error.message || "视频生成失败", true); }
    finally { submit.disabled = false; }
  });
  (async () => {
    try {
      await migrateLegacyHistory();
      syncMode(); await renderHistory();
      (await historyItems()).filter((item) => item.task_id && !["completed", "failed"].includes(item.status)).forEach((item) =>
        waitForTask(item.task_id).catch((error) => setStatus(error.message, true)));
    } catch (error) { setStatus("视频历史记录加载失败：" + error.message, true); syncMode(); }
  })();
})();
