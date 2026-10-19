(function () {
  const state = { sourceURL: "", points: [], files: [], selectedIndex: -1, dragging: null, onApply: null };
  const el = (id) => document.getElementById(id);
  async function loadImageModels() {
    const select = el("fineEditorImageModel");
    if (!select) return;
    try {
      const data = await fetch("/api/ipricing").then(r => r.json());
      const models = (data?.data?.img || data?.data?.image || []).filter(x => x?.model_name);
      if (models.length) select.innerHTML = models.map(x => '<option value="' + esc(x.model_name) + '">' + esc(x.model_name) + '</option>').join("");
    } catch (_) { /* keep the built-in model */ }
  }

  function syncMarkerBounds() {
    const img = el("fineEditorImage");
    const markers = el("fineEditorMarkers");
    markers.style.left = img.offsetLeft + "px";
    markers.style.top = img.offsetTop + "px";
    markers.style.width = img.clientWidth + "px";
    markers.style.height = img.clientHeight + "px";
    syncQuickEditor();
  }

  function syncSourceEmpty() {
    const empty = !state.sourceURL;
    el("fineEditorSourceEmpty").hidden = !empty;
    el("fineEditorImage").hidden = empty;
    el("fineEditorEmpty").hidden = empty || state.points.length > 0;
    el("fineEditorMarkers").hidden = empty;
  }

  function renderPoints() {
    syncMarkerBounds();
    el("fineEditorEmpty").hidden = !state.sourceURL || state.points.length > 0;
    el("fineEditorMarkers").innerHTML = state.points.map((point, index) =>
      '<button class="fine-marker' + (index === state.selectedIndex ? ' active' : '') + '" type="button" style="left:' + point.x + '%;top:' + point.y + '%" data-fine-index="' + index + '" aria-label="修改点 ' + (index + 1) + '，可拖动或用方向键微调">' + (index + 1) + '</button>'
    ).join("");
    el("finePointList").innerHTML = state.points.length ? state.points.map((point, index) =>
      '<div class="fine-point-item' + (index === state.selectedIndex ? ' active' : '') + '"><span class="fine-point-number">' + (index + 1) + '</span>' +
      '<textarea data-fine-description="' + index + '" placeholder="描述此处要如何修改">' + esc(point.description) + '</textarea>' +
      '<button type="button" data-fine-remove="' + index + '" aria-label="删除修改点">×</button></div>'
    ).join("") : '<p class="fine-point-placeholder">还没有修改点</p>';
    syncQuickEditor();
  }

  function syncQuickEditor() {
    const editor = el("finePointQuickEditor");
    const input = el("finePointQuickInput");
    const point = state.points[state.selectedIndex];
    if (!point) { editor.hidden = true; return; }
    editor.hidden = false;
    const img = el("fineEditorImage");
    editor.style.left = (img.offsetLeft + img.clientWidth * point.x / 100) + "px";
    editor.style.top = (img.offsetTop + img.clientHeight * point.y / 100) + "px";
    if (input.value !== point.description) input.value = point.description;
  }

  function selectPoint(index, focusMarker) {
    state.selectedIndex = index >= 0 && index < state.points.length ? index : -1;
    renderPoints();
    if (focusMarker && state.selectedIndex >= 0) {
      const marker = el("fineEditorMarkers").querySelector('[data-fine-index="' + state.selectedIndex + '"]');
      if (marker) marker.focus();
    }
  }

  function activatePointView(index) {
    state.selectedIndex = index;
    el("fineEditorMarkers").querySelectorAll(".fine-marker").forEach((marker, markerIndex) => marker.classList.toggle("active", markerIndex === index));
    el("finePointList").querySelectorAll(".fine-point-item").forEach((item, itemIndex) => item.classList.toggle("active", itemIndex === index));
    syncQuickEditor();
  }

  function removePoint(index) {
    if (index < 0 || index >= state.points.length) return;
    state.points.splice(index, 1);
    if (!state.points.length) state.selectedIndex = -1;
    else if (state.selectedIndex >= state.points.length) state.selectedIndex = state.points.length - 1;
    else if (index < state.selectedIndex) state.selectedIndex--;
    renderPoints();
  }

  function renderFiles() {
    el("fineEditorFileList").innerHTML = state.files.map((file, index) =>
      '<span>' + esc(file.name) + '<button type="button" data-fine-file-remove="' + index + '">×</button></span>'
    ).join("");
  }

  function close() { el("fineEditor").hidden = true; }

  function showPointTab() {
    el("finePointEditorView").hidden = false;
    el("retouchEditor").hidden = true;
    el("fineEditorHint").textContent = state.sourceURL ? "直接点击图片添加修改点，再描述这个位置要如何更改" : "点击或拖入图片后，即可指哪改哪";
    document.querySelectorAll("[data-editor-tab]").forEach((button) => {
      const active = button.dataset.editorTab === "point";
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", String(active));
    });
    syncSourceEmpty(); syncMarkerBounds();
  }


  function openEmpty() {
    state.sourceURL = ""; state.points = []; state.selectedIndex = -1; state.files = [];
    el("fineEditorImage").removeAttribute("src"); el("fineEditorNote").value = ""; el("fineEditorStatus").textContent = "";
    renderPoints(); renderFiles(); showPointTab(); el("imageModal") && (el("imageModal").hidden = true); el("fineEditor").hidden = false;
  }

  function open(url, options) {
    if (!url) return;
    state.onApply = options?.onApply || null;    state.sourceURL = url;
    state.points = [];
    state.selectedIndex = -1;
    state.files = [];
    el("fineEditorImage").src = url;
    el("fineEditorImage").onload = syncMarkerBounds;
    el("fineEditorNote").value = "";
    el("fineEditorFiles").value = "";
    el("fineEditorStatus").textContent = "";
    syncSourceEmpty(); renderPoints();
    renderFiles();
    // PPT image editing starts in the full manual editor.
    if ((options?.initialTab === "manual" || /(?:^|\/)ppt(?:\.html)?$/.test(location.pathname)) && window.KuaimaRetouchEditor) {
      window.KuaimaRetouchEditor.open(url, "pan");
      return;
    }
    showPointTab();
    el("imageModal") && (el("imageModal").hidden = true);
    el("fineEditor").hidden = false;
  }

  function pointPrompt() {
    const lines = state.points.map((point, index) => {
      const description = point.description.trim();
      return description ? (index + 1) + '. 修改点（横向 ' + Math.round(point.x) + '%，纵向 ' + Math.round(point.y) + '%）：' + description : '';
    }).filter(Boolean);
    const note = el('fineEditorNote').value.trim();
    return ['请以第1张图片为原图进行精细编辑。除明确要求的位置外，保持原图的主体身份、构图、比例、光影和其他细节不变。', lines.length ? '位置修改要求：\\n' + lines.join('\\n') : '', note ? '整体备注：\\n' + note : '', state.files.length ? '后续图片是补充参考图，仅用于辅助理解修改要求。' : ''].filter(Boolean).join('\\n');
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
    if (state.onApply) {
      try {
        const sourceResponse = await fetch(state.sourceURL); const sourceBlob = await sourceResponse.blob();
        const formData = new FormData(); formData.append("prompt", pointPrompt()); formData.append("model", el("fineEditorImageModel")?.value || "gpt-image-2"); formData.append("images", sourceBlob, "ppt-image.png");
        const response = await fetch("/api/generate", { method: "POST", body: formData }); const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error || "图片编辑失败");
        const result = payload.images?.[0] || payload.data?.[0]?.url || payload.url; if (!result) throw new Error("编辑接口未返回图片");
        await state.onApply(result); close(); button.disabled = false; button.innerHTML = iconHTML("wand-sparkles") + "<span>确定</span>"; return;
      } catch (error) { el("fineEditorStatus").textContent = "编辑失败：" + error.message; el("fineEditorStatus").className = "status error"; button.disabled = false; button.innerHTML = iconHTML("wand-sparkles") + "<span>确定</span>"; return; }
    }
    let previousImages = null;
    let previousPrompt = "";
    let previousModel = "";
    try {
      const source = await sourceFile();
      const supplements = await dedupeReferenceFiles(state.files, [source]);
      const form = el("genForm");
      const generateTab = document.querySelector('[data-mode-tab="generate"][data-workflow-id="single"]');
      if (generateTab) generateTab.click();
      previousImages = selectedImages;
      previousPrompt = form.elements.prompt.value;
      previousModel = form.elements.image_model?.value || "";
      if (form.elements.image_model && el("fineEditorImageModel")?.value) form.elements.image_model.value = el("fineEditorImageModel").value;
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
        if (el("genForm").elements.image_model && previousModel) el("genForm").elements.image_model.value = previousModel;
        syncImageInput();
        renderImagePreviews();
      }
      button.disabled = false;
      button.innerHTML = iconHTML("wand-sparkles") + "<span>确定</span>";
    }
  }

  function init() {
    loadImageModels();
    document.addEventListener("click", (event) => {
      if (event.target.closest("[data-fine-edit-tab]")) el("fineEditorSourceFiles").click();
    });
    el("fineEditorSourceEmpty").addEventListener("click", () => el("fineEditorSourceFiles").click());
    el("fineEditorSourceFiles").addEventListener("change", (event) => {
      const file = Array.from(event.target.files || [])[0];
      event.target.value = "";
      if (file) open(imagePreviewURL(file));
    });
    el("openFineEditor")?.addEventListener("click", () => open(el("imageModal")?.dataset.previewUrl));
    document.querySelector('[data-editor-tab="point"]').addEventListener("click", showPointTab);
    document.querySelector('[data-editor-tab="manual"]').addEventListener("click", () => state.sourceURL ? window.KuaimaRetouchEditor.open(state.sourceURL, "pan") : window.KuaimaRetouchEditor.openEmpty("pan"));
    el("closeFineEditor").addEventListener("click", close);
    el("fineEditor").addEventListener("click", (event) => { if (event.target === el("fineEditor")) close(); });
    el("fineEditorStage").addEventListener("click", (event) => {
      if (!state.sourceURL) return;
      if (event.target.closest(".fine-marker, .fine-point-quick-editor")) return;
      const img = el("fineEditorImage");
      const rect = img.getBoundingClientRect();
      if (!rect.width || event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) return;
      state.points.push({ x: (event.clientX - rect.left) / rect.width * 100, y: (event.clientY - rect.top) / rect.height * 100, description: "" });
      selectPoint(state.points.length - 1);
      el("finePointQuickInput").focus();
    });
    ["dragenter", "dragover"].forEach((name) => el("fineEditorStage").addEventListener(name, (event) => {
      if (state.sourceURL) return; event.preventDefault(); el("fineEditorStage").classList.add("drag-over");
    }));
    ["dragleave", "drop"].forEach((name) => el("fineEditorStage").addEventListener(name, (event) => {
      if (state.sourceURL) return; event.preventDefault(); el("fineEditorStage").classList.remove("drag-over");
    }));
    el("fineEditorStage").addEventListener("drop", (event) => {
      if (state.sourceURL) return; const file = Array.from(event.dataTransfer?.files || []).find((item) => String(item.type || "").startsWith("image/"));
      if (file) open(imagePreviewURL(file));
    });
    el("finePointList").addEventListener("input", (event) => {
      if (event.target.dataset.fineDescription != null) {
        const index = Number(event.target.dataset.fineDescription);
        state.points[index].description = event.target.value;
        if (index === state.selectedIndex) el("finePointQuickInput").value = event.target.value;
      }
    });
    el("finePointList").addEventListener("click", (event) => {
      if (event.target.dataset.fineRemove != null) removePoint(Number(event.target.dataset.fineRemove));
    });
    el("finePointList").addEventListener("focusin", (event) => {
      if (event.target.dataset.fineDescription != null) activatePointView(Number(event.target.dataset.fineDescription));
    });
    el("fineEditorMarkers").addEventListener("pointerdown", (event) => {
      const marker = event.target.closest("[data-fine-index]");
      if (!marker) return;
      event.preventDefault();
      selectPoint(Number(marker.dataset.fineIndex), true);
      state.dragging = { pointerId: event.pointerId };
      el("fineEditorMarkers").setPointerCapture(event.pointerId);
    });
    el("fineEditorMarkers").addEventListener("pointermove", (event) => {
      if (!state.dragging || state.dragging.pointerId !== event.pointerId) return;
      const rect = el("fineEditorImage").getBoundingClientRect();
      const point = state.points[state.selectedIndex];
      point.x = Math.max(0, Math.min(100, (event.clientX - rect.left) / rect.width * 100));
      point.y = Math.max(0, Math.min(100, (event.clientY - rect.top) / rect.height * 100));
      renderPoints();
    });
    el("fineEditorMarkers").addEventListener("pointerup", () => {
      const index = state.selectedIndex;
      state.dragging = null;
      selectPoint(index, true);
    });
    el("fineEditorMarkers").addEventListener("pointercancel", () => { state.dragging = null; });
    el("fineEditorMarkers").addEventListener("keydown", (event) => {
      const marker = event.target.closest("[data-fine-index]");
      if (!marker) return;
      const index = Number(marker.dataset.fineIndex);
      if (event.key === "Delete" || event.key === "Backspace") { event.preventDefault(); removePoint(index); return; }
      const moves = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
      if (!moves[event.key]) return;
      event.preventDefault();
      const step = event.shiftKey ? 2 : 0.5;
      state.points[index].x = Math.max(0, Math.min(100, state.points[index].x + moves[event.key][0] * step));
      state.points[index].y = Math.max(0, Math.min(100, state.points[index].y + moves[event.key][1] * step));
      selectPoint(index, true);
    });
    el("finePointQuickInput").addEventListener("input", (event) => {
      const point = state.points[state.selectedIndex];
      if (!point) return;
      point.description = event.target.value;
      const input = el("finePointList").querySelector('[data-fine-description="' + state.selectedIndex + '"]');
      if (input) input.value = event.target.value;
    });
    el("clearFinePoints").addEventListener("click", () => { state.points = []; state.selectedIndex = -1; renderPoints(); });
    el("fineEditorFiles").addEventListener("change", async (event) => {
      state.files = state.files.concat(await dedupeReferenceFiles(Array.from(event.target.files || []), state.files));
      renderFiles();
    });
    el("fineEditorFileList").addEventListener("click", (event) => {
      if (event.target.dataset.fineFileRemove != null) { state.files.splice(Number(event.target.dataset.fineFileRemove), 1); renderFiles(); }
    });
    el("applyFineEdit").addEventListener("click", apply);
    el("applyFineEditHeader")?.addEventListener("click", () => {
      if (!el("retouchEditor").hidden && state.onApply && window.KuaimaRetouchEditor) {
        const result = window.KuaimaRetouchEditor.exportDataURL();
        if (result) { state.onApply(result); close(); return; }
      }
      apply();
    });
    window.addEventListener("resize", syncMarkerBounds);
  }

  window.KuaimaFineEditor = { init, open, openEmpty, close };
  init();
})();
