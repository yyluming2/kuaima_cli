(function () {
  const state = { sourceURL: "", points: [], files: [] };
  const el = (id) => document.getElementById(id);

  function syncMarkerBounds() {
    const img = el("fineEditorImage");
    const markers = el("fineEditorMarkers");
    markers.style.left = img.offsetLeft + "px";
    markers.style.top = img.offsetTop + "px";
    markers.style.width = img.clientWidth + "px";
    markers.style.height = img.clientHeight + "px";
  }

  function renderPoints() {
    syncMarkerBounds();
    el("fineEditorEmpty").hidden = state.points.length > 0;
    el("fineEditorMarkers").innerHTML = state.points.map((point, index) =>
      '<button class="fine-marker" type="button" style="left:' + point.x + '%;top:' + point.y + '%" data-fine-index="' + index + '">' + (index + 1) + '</button>'
    ).join("");
    el("finePointList").innerHTML = state.points.length ? state.points.map((point, index) =>
      '<div class="fine-point-item"><span class="fine-point-number">' + (index + 1) + '</span>' +
      '<textarea data-fine-description="' + index + '" placeholder="描述此处要如何修改">' + esc(point.description) + '</textarea>' +
      '<button type="button" data-fine-remove="' + index + '" aria-label="删除修改点">×</button></div>'
    ).join("") : '<p class="fine-point-placeholder">还没有修改点</p>';
  }

  function renderFiles() {
    el("fineEditorFileList").innerHTML = state.files.map((file, index) =>
      '<span>' + esc(file.name) + '<button type="button" data-fine-file-remove="' + index + '">×</button></span>'
    ).join("");
  }

  function close() { el("fineEditor").hidden = true; }

  function open(url) {
    if (!url) return;
    state.sourceURL = url;
    state.points = [];
    state.files = [];
    el("fineEditorImage").src = url;
    el("fineEditorImage").onload = syncMarkerBounds;
    el("fineEditorNote").value = "";
    el("fineEditorFiles").value = "";
    el("fineEditorStatus").textContent = "";
    renderPoints();
    renderFiles();
    el("imageModal").hidden = true;
    el("fineEditor").hidden = false;
  }

  function pointPrompt() {
    const lines = state.points.map((point, index) => {
      const description = point.description.trim();
      return description ? (index + 1) + ". 修改点（横向 " + Math.round(point.x) + "%，纵向 " + Math.round(point.y) + "%）：" + description : "";
    }).filter(Boolean);
    const note = el("fineEditorNote").value.trim();
    return [
      "请以第1张图片为原图进行精细编辑。除明确要求的位置外，保持原图的主体身份、构图、比例、光影和其他细节不变。",
      lines.length ? "位置修改要求：\n" + lines.join("\n") : "",
      note ? "整体备注：\n" + note : "",
      state.files.length ? "后续图片是补充参考图，仅用于辅助理解修改要求。" : "",
    ].filter(Boolean).join("\n\n");
  }

  async function sourceFile() {
    const existing = selectedImages.find((file) => imagePreviewURL(file) === state.sourceURL || historySourceURLs.get(file) === state.sourceURL);
    return existing || fileFromHistoryURL(state.sourceURL, "fine-edit-source.png");
  }

  async function apply() {
    if (isGenerationRunning()) {
      el("fineEditorStatus").textContent = "已有任务正在生成，请等待完成或先取消";
      el("fineEditorStatus").className = "status error";
      return;
    }
    if (!state.points.some((point) => point.description.trim()) && !el("fineEditorNote").value.trim()) {
      el("fineEditorStatus").textContent = "请至少填写一个位置修改描述或整体备注";
      el("fineEditorStatus").className = "status error";
      return;
    }
    const button = el("applyFineEdit");
    button.disabled = true;
    button.innerHTML = '<span class="spinner small"></span><span>生成中...</span>';
    el("fineEditorStatus").textContent = "正在准备并提交...";
    let previousImages = null;
    let previousPrompt = "";
    try {
      const source = await sourceFile();
      const supplements = await dedupeReferenceFiles(state.files, [source]);
      const form = el("genForm");
      const generateTab = document.querySelector('[data-mode-tab="generate"][data-workflow-id="single"]');
      if (generateTab) generateTab.click();
      previousImages = selectedImages;
      previousPrompt = form.elements.prompt.value;
      selectedImages = [source].concat(supplements);
      form.elements.prompt.value = pointPrompt();
      activeGenerationController = new AbortController();
      close();
      el("cancelGenerate").hidden = false;
      el("genStatus").textContent = "正在编辑...";
      el("genStatus").className = "status gen-status loading";
      const result = await generateStream(form, activeGenerationController.signal);
      const historyError = await saveHistorySafely(result);
      el("genStatus").textContent = "编辑完成，已保存 " + result.saved.length + " 张" +
        (historyError ? "，历史保存失败：" + historyError : "");
      el("genStatus").className = "status gen-status";
      loadBalance();
    } catch (error) {
      const canceled = error && error.name === "AbortError";
      if (el("gallery").querySelector(".generation-placeholder")) {
        renderGalleryMessage(canceled ? "已取消生成" : "生成失败：" + error.message, canceled ? "status" : "status error");
      }
      el("genStatus").textContent = canceled ? "已取消编辑" : "编辑失败：" + (error.message || "未知错误");
      el("genStatus").className = canceled ? "status gen-status" : "status gen-status error";
      if (!canceled) await saveHistorySafely({ images: [] }, { status: "failed", error: error.message });
    } finally {
      activeGenerationController = null;
      el("cancelGenerate").hidden = true;
      if (previousImages) {
        selectedImages = previousImages;
        el("genForm").elements.prompt.value = previousPrompt;
        syncImageInput();
        renderImagePreviews();
      }
      button.disabled = false;
      button.innerHTML = iconHTML("wand-sparkles") + "<span>直接生成</span>";
    }
  }

  function init() {
    document.addEventListener("click", (event) => {
      if (event.target.closest("[data-fine-edit-tab]")) el("fineEditorSourceFiles").click();
    });
    el("fineEditorSourceFiles").addEventListener("change", (event) => {
      const file = Array.from(event.target.files || [])[0];
      event.target.value = "";
      if (file) open(imagePreviewURL(file));
    });
    el("openFineEditor").addEventListener("click", () => open(el("imageModal").dataset.previewUrl));
    el("closeFineEditor").addEventListener("click", close);
    el("fineEditor").addEventListener("click", (event) => { if (event.target === el("fineEditor")) close(); });
    el("fineEditorStage").addEventListener("click", (event) => {
      if (event.target.closest(".fine-marker")) return;
      const img = el("fineEditorImage");
      const rect = img.getBoundingClientRect();
      if (!rect.width || event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) return;
      state.points.push({ x: (event.clientX - rect.left) / rect.width * 100, y: (event.clientY - rect.top) / rect.height * 100, description: "" });
      renderPoints();
      const input = el("finePointList").querySelector('[data-fine-description="' + (state.points.length - 1) + '"]');
      if (input) input.focus();
    });
    el("finePointList").addEventListener("input", (event) => {
      if (event.target.dataset.fineDescription != null) state.points[Number(event.target.dataset.fineDescription)].description = event.target.value;
    });
    el("finePointList").addEventListener("click", (event) => {
      if (event.target.dataset.fineRemove != null) { state.points.splice(Number(event.target.dataset.fineRemove), 1); renderPoints(); }
    });
    el("fineEditorMarkers").addEventListener("click", (event) => {
      const marker = event.target.closest("[data-fine-index]");
      if (!marker) return;
      const input = el("finePointList").querySelector('[data-fine-description="' + marker.dataset.fineIndex + '"]');
      if (input) input.focus();
    });
    el("clearFinePoints").addEventListener("click", () => { state.points = []; renderPoints(); });
    el("fineEditorFiles").addEventListener("change", async (event) => {
      state.files = state.files.concat(await dedupeReferenceFiles(Array.from(event.target.files || []), state.files));
      renderFiles();
    });
    el("fineEditorFileList").addEventListener("click", (event) => {
      if (event.target.dataset.fineFileRemove != null) { state.files.splice(Number(event.target.dataset.fineFileRemove), 1); renderFiles(); }
    });
    el("applyFineEdit").addEventListener("click", apply);
    window.addEventListener("resize", syncMarkerBounds);
  }

  window.KuaimaFineEditor = { init, open, close };
  init();
})();
