(function () {
  const form = document.querySelector("#videoForm");
  if (!form) return;
  const mode = document.querySelector("#videoMode");
  const input = document.querySelector("#videoImages");
  const status = document.querySelector("#videoStatus");
  const previews = document.querySelector("#videoImagePreviews");
  const submit = document.querySelector("#videoSubmit");
  let previewURLs = [];

  function setStatus(text, error) {
    status.textContent = text || "";
    status.className = "status" + (error ? " error" : "");
  }
  function syncMode() {
    const frames = mode.value === "frames";
    document.querySelector("#videoImagesLabel").textContent = frames ? "首帧与尾帧" : "参考图片";
    document.querySelector("#videoImagesHint").textContent = frames ? "第 1 张作为首帧（必选），第 2 张作为尾帧（可选）。" : "可选择多张参考图片。";
  }
  function renderPreviews() {
    previewURLs.forEach(URL.revokeObjectURL);
    previewURLs = Array.from(input.files || []).map(URL.createObjectURL);
    previews.innerHTML = previewURLs.map((url, index) => '<figure><img src="' + url + '" alt="参考图"><figcaption>' +
      (mode.value === "frames" ? (index === 0 ? "首帧" : "尾帧") : "参考图 " + (index + 1)) + '</figcaption></figure>').join("");
  }
  async function waitForTask(taskID) {
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      const task = await api("/api/video/status/" + encodeURIComponent(taskID));
      if (task.status === "completed") return task;
      if (task.status === "failed") {
        const detail = task.error && task.error.message ? task.error.message : "视频生成失败";
        throw new Error(detail);
      }
      setStatus(task.status === "in_progress" ? "视频生成中..." : "任务排队中...");
    }
  }
  mode.addEventListener("change", () => { syncMode(); renderPreviews(); });
  input.addEventListener("change", () => {
    if (mode.value === "frames" && input.files.length > 2) {
      input.value = "";
      setStatus("首尾帧模式最多选择 2 张图片", true);
    }
    renderPreviews();
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (mode.value === "frames" && input.files.length === 0) {
      setStatus("请选择首帧图片", true);
      return;
    }
    submit.disabled = true;
    setStatus("正在提交视频任务...");
    document.querySelector("#videoResult").hidden = true;
    try {
      const task = await api("/api/video/generate", { method: "POST", body: new FormData(form) });
      setStatus(task.status === "in_progress" ? "视频生成中..." : "任务排队中...");
      const result = task.status === "completed" ? task : await waitForTask(task.task_id);
      if (!result.url) throw new Error("任务已完成，但没有返回视频地址");
      const player = document.querySelector("#videoPlayer");
      player.src = result.url;
      document.querySelector("#videoDownload").href = result.url;
      const meta = result.metadata || {};
      const dimensions = meta.width && meta.height ? meta.width + "×" + meta.height : "";
      document.querySelector("#videoMetadata").textContent = [result.format, meta.duration ? meta.duration + " 秒" : "", dimensions].filter(Boolean).join(" · ");
      document.querySelector("#videoResult").hidden = false;
      setStatus("视频生成完成");
      player.load();
    } catch (error) {
      setStatus(error.message || "视频生成失败", true);
    } finally {
      submit.disabled = false;
    }
  });
  syncMode();
})();
