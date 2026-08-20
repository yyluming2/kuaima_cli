(function () {
  const form = document.querySelector("#videoForm");
  if (!form) return;
  const mode = document.querySelector("#videoMode");
  const status = document.querySelector("#videoStatus");
  const previews = document.querySelector("#videoImagePreviews");
  const submit = document.querySelector("#videoSubmit");
  const historyPanel = document.querySelector("#videoHistory");
  const historyKey = "kuaima.webui.video.history.v1";
  let previewURLs = [];
  let polling = new Set();

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
  function historyItems() {
    try { return JSON.parse(localStorage.getItem(historyKey) || "[]"); } catch { return []; }
  }
  function saveTask(task, extra) {
    const items = historyItems();
    const id = String(task.task_id || extra.task_id || "");
    const item = Object.assign({}, items.find((value) => value.task_id === id) || {}, extra || {}, task, { task_id: id });
    const next = [item].concat(items.filter((value) => value.task_id !== id)).slice(0, 100);
    localStorage.setItem(historyKey, JSON.stringify(next));
    renderHistory();
    return item;
  }
  function renderHistory() {
    const items = historyItems();
    historyPanel.innerHTML = items.length ? items.map((item) =>
      '<article class="video-history-item"><strong>' + esc(item.prompt || "视频任务") + '</strong>' +
      '<small>' + esc(item.created_at ? new Date(item.created_at).toLocaleString() : "") + '</small>' +
      '<code>' + esc(item.task_id) + '</code><span>' + esc(item.status || "queued") + '</span>' +
      (item.url ? '<button class="btn small" type="button" data-video-task="' + esc(item.task_id) + '">查看</button>' :
        '<button class="btn small" type="button" data-video-task="' + esc(item.task_id) + '">继续轮询</button>') + '</article>'
    ).join("") : '<div class="history-empty">暂无视频任务</div>';
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
        saveTask(task, {});
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
  historyPanel.addEventListener("click", (event) => {
    const button = event.target.closest("[data-video-task]");
    if (!button) return;
    const item = historyItems().find((value) => value.task_id === button.dataset.videoTask);
    if (item && item.url) { showResult(item); setStatus("已打开历史视频"); }
    else waitForTask(button.dataset.videoTask).catch((error) => setStatus(error.message, true));
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (mode.value === "frames" && inputs().every((input) => !input.files.length)) { setStatus("首帧或尾帧至少选择一张", true); return; }
    if (mode.value === "edit" && !inputs()[0].files.length) { setStatus("请选择待编辑视频", true); return; }
    submit.disabled = true; setStatus("正在提交视频任务...");
    document.querySelector("#videoResult").hidden = true;
    try {
      const data = new FormData(form);
      if (!form.elements.audio.checked) data.set("audio", "false");
      if (!form.elements.watermark.checked) data.set("watermark", "false");
      const task = await api("/api/video/generate", { method: "POST", body: data });
      saveTask(task, { prompt: form.elements.prompt.value, mode: mode.value, created_at: new Date().toISOString() });
      if (task.status === "completed") { showResult(task); setStatus("视频生成完成"); }
      else await waitForTask(task.task_id);
    } catch (error) { setStatus(error.message || "视频生成失败", true); }
    finally { submit.disabled = false; }
  });
  syncMode(); renderHistory();
  historyItems().filter((item) => item.task_id && !["completed", "failed"].includes(item.status)).forEach((item) =>
    waitForTask(item.task_id).catch((error) => setStatus(error.message, true)));
})();
