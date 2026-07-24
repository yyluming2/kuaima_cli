function openHistoryDB() {
  if (!("indexedDB" in window)) {
    return Promise.resolve(null);
  }
  if (!historyDBPromise) {
    historyDBPromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(HISTORY_DB_NAME, 1);
      request.onupgradeneeded = () => {
        const db = request.result;
        if (!db.objectStoreNames.contains(HISTORY_STORE)) {
          db.createObjectStore(HISTORY_STORE, { keyPath: "id" });
        }
      };
      request.onsuccess = () => {
        const db = request.result;
        db.onversionchange = () => resetHistoryDB(db);
        db.onclose = () => {
          if (historyDBPromise) {
            historyDBPromise = null;
          }
        };
        resolve(db);
      };
      request.onerror = () => {
        historyDBPromise = null;
        reject(request.error);
      };
    });
  }
  return historyDBPromise;
}

function resetHistoryDB(db) {
  if (historyDBPromise) {
    historyDBPromise = null;
  }
  if (db && typeof db.close === "function") {
    try {
      db.close();
    } catch {
      // Ignore close races; the next transaction will reopen the database.
    }
  }
}

function isHistoryDBConnectionError(error) {
  const message = String(error && error.message || "");
  return error && (
    error.name === "InvalidStateError" ||
    /database connection is (closing|closed)/i.test(message)
  );
}

async function requestPersistentHistoryStorage() {
  if (historyStoragePersistRequested || !navigator.storage || !navigator.storage.persist) {
    return false;
  }
  historyStoragePersistRequested = true;
  try {
    return await navigator.storage.persist();
  } catch {
    return false;
  }
}

async function withHistoryStore(mode, callback, retry = true) {
  const db = await openHistoryDB();
  if (!db) {
    return callback(null);
  }
  try {
    return await new Promise((resolve, reject) => {
      let tx;
      try {
        tx = db.transaction(HISTORY_STORE, mode);
      } catch (error) {
        resetHistoryDB(db);
        reject(error);
        return;
      }
      const store = tx.objectStore(HISTORY_STORE);
      let result;
      tx.oncomplete = () => resolve(result);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error || new Error("history transaction aborted"));
      try {
        result = callback(store);
      } catch (error) {
        try {
          tx.abort();
        } catch {
          // Transaction may already be inactive.
        }
        reject(error);
      }
    });
  } catch (error) {
    if (retry && isHistoryDBConnectionError(error)) {
      return withHistoryStore(mode, callback, false);
    }
    throw error;
  }
}

function requestResult(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function readHistory() {
  const items = await withHistoryStore("readonly", (store) => {
    if (!store) {
      return [];
    }
    return requestResult(store.getAll());
  });
  return (Array.isArray(items) ? items : []).sort((a, b) => String(b.created_at || "").localeCompare(String(a.created_at || "")));
}

async function cleanupHistoryImageData() {
  const items = await readHistory();
  const dirty = items.filter((item) => (item.references || []).some((ref) => ref && "blob" in ref));
  if (dirty.length === 0) {
    return;
  }
  await withHistoryStore("readwrite", (store) => {
    dirty.forEach((item) => {
      item.references = (item.references || []).map((ref) => ({
        name: ref && ref.name || "reference-image.png",
        type: ref && ref.type || "image/png",
        size: ref && ref.size || 0,
        url: ref && ref.url || "",
      }));
      store.put(item);
    });
  });
}

async function trimHistory() {
  const items = await readHistory();
  const stale = items.slice(HISTORY_LIMIT);
  if (stale.length === 0) {
    return;
  }
  await withHistoryStore("readwrite", (store) => {
    stale.forEach((item) => store.delete(item.id));
  });
}

async function putHistoryItem(item) {
  const db = await openHistoryDB();
  if (!db) {
    return;
  }
  const existing = await readHistory();
  const duplicate = existing.find((entry) => historyKey(entry) === historyKey(item));
  const previous = existing[0];
  const replacePreviousFailure = previous &&
    previous.status === "failed" &&
    item.status === "failed" &&
    historyFailureKey(previous) === historyFailureKey(item);
  await withHistoryStore("readwrite", (store) => {
    if (replacePreviousFailure) {
      store.delete(previous.id);
    } else if (duplicate) {
      store.delete(duplicate.id);
    }
    store.put(item);
  });
  await trimHistory();
}

async function deleteHistoryItem(id) {
  const db = await openHistoryDB();
  if (!db) {
    return;
  }
  await withHistoryStore("readwrite", (store) => {
    store.delete(id);
  });
}

function historyKey(item) {
  return [
    item.status || "success",
    item.workflow_id || "single",
    item.prompt || "",
    item.image_model || "",
    item.image_endpoint || "",
    item.image_size || "",
    item.image_quality || "",
    item.image_count || "",
    item.reference_count || 0,
    JSON.stringify(item.workflow_params || {}),
    (item.images || []).join(","),
    item.error || "",
  ].join("\n");
}

function historyFailureKey(item) {
  const references = (item.references || []).map((ref) => ({
    name: ref && ref.name || "",
    type: ref && ref.type || "",
    size: ref && ref.size || 0,
    url: ref && ref.url || "",
  }));
  return [
    item.workflow_id || "single",
    item.prompt || "",
    item.image_model || "",
    item.image_endpoint || "",
    item.image_size || "",
    item.image_quality || "",
    item.image_count || "",
    item.ratio || "",
    item.resolution || "",
    item.reference_count || 0,
    JSON.stringify(item.workflow_params || {}),
    JSON.stringify(references),
  ].join("\n");
}

function historyReferences() {
  return selectedImages.map((file) => ({
    name: file.name || "reference-image.png",
    type: file.type || "image/png",
    size: file.size || 0,
    url: historySourceURLs.get(file) || "",
  }));
}

async function uploadHistoryReferences(files) {
  const pending = Array.from(files || []).filter((file) => file && !historySourceURLs.get(file));
  if (pending.length === 0) {
    return [];
  }
  const form = new FormData();
  pending.forEach((file) => {
    form.append("images", file, file.name || "reference-image.png");
  });
  const result = await api("/api/history/upload", {
    method: "POST",
    body: form,
  });
  return Array.isArray(result.urls) ? result.urls : [];
}

async function ensureHistoryReferenceURLs() {
  const refs = historyReferences();
  const missing = [];
  const missingIndexes = [];
  selectedImages.forEach((file, index) => {
    if (!historySourceURLs.get(file)) {
      missing.push(file);
      missingIndexes.push(index);
    }
  });
  if (missing.length === 0) {
    return refs;
  }
  try {
    const urls = await uploadHistoryReferences(missing);
    missingIndexes.forEach((index, i) => {
      const url = urls[i] || "";
      if (url) {
        refs[index].url = url;
        historySourceURLs.set(selectedImages[index], url);
      }
    });
  } catch (error) {
    console.warn("History reference upload failed", error);
  }
  return refs;
}

async function saveHistory(result, extra = {}) {
  const form = $("#genForm");
  const references = await ensureHistoryReferenceURLs();
  const timing = result && result.timing ? result.timing : null;
  const workflowId = form.elements.workflow_id ? form.elements.workflow_id.value : "single";
  const workflowParams = typeof window.KuaimaWorkflow !== "undefined" &&
    window.KuaimaWorkflow && typeof window.KuaimaWorkflow.currentValues === "function"
    ? window.KuaimaWorkflow.currentValues()
    : {};
  const resultParams = result && result.params && typeof result.params === "object" ? result.params : {};
  const mergedWorkflowParams = Object.assign({}, workflowParams, resultParams);
  const historyId = String(extra.history_id || (result && result.history_id) || "").trim() || String(Date.now());
  const item = {
    id: historyId,
    created_at: new Date().toISOString(),
    status: extra.status || "success",
    prompt: form.elements.prompt.value,
    workflow_id: result.workflow_id || workflowId || "single",
    workflow_name: result.workflow_name || (typeof window.KuaimaWorkflow !== "undefined" && window.KuaimaWorkflow &&
      typeof window.KuaimaWorkflow.currentWorkflowLabel === "function"
      ? window.KuaimaWorkflow.currentWorkflowLabel(workflowId)
      : workflowId),
    workflow_params: mergedWorkflowParams,
    image_model: form.elements.image_model.value,
    image_count: form.elements.image_count.value,
    image_quality: form.elements.image_quality.value,
    image_endpoint: form.elements.image_endpoint.value,
    ratio: $("#ratioSelect").value,
    resolution: $("#resolutionSelect").value,
    image_size: $('[name="image_size"]').value,
    reference_count: selectedImages.length,
    references,
    images: result.images || [],
    response_ms: timing && Number.isFinite(Number(timing.response_ms)) ? Math.round(Number(timing.response_ms)) : null,
    total_ms: timing && Number.isFinite(Number(timing.total_ms)) ? Math.round(Number(timing.total_ms)) : null,
    error: extra.error || "",
  };
  if (result && typeof result === "object") {
    result.history_id = item.id;
  }
  await putHistoryItem(item);
  await renderHistory();
}

async function saveHistorySafely(result, extra = {}) {
  try {
    await saveHistory(result, extra);
    return "";
  } catch (error) {
    console.warn("History save failed", error);
    return error && error.message ? error.message : "unknown history error";
  }
}

function formatHistoryTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatDuration(value) {
  const duration = Number(value);
  if (!Number.isFinite(duration) || duration < 0) {
    return "-";
  }
  if (duration < 1000) {
    return Math.max(1, Math.round(duration)) + "ms";
  }
  return (duration / 1000).toFixed(duration >= 10000 ? 0 : 1).replace(/\.0$/, "") + "s";
}

function formatTimingSummary(item) {
  const parts = [];
  if (Number.isFinite(Number(item && item.response_ms))) {
    parts.push("响应 " + formatDuration(item.response_ms));
  }
  if (Number.isFinite(Number(item && item.total_ms))) {
    parts.push("总计 " + formatDuration(item.total_ms));
  }
  return parts.join(" · ");
}

function formatElapsedSeconds(ms) {
  return Math.max(0, Math.ceil(Number(ms || 0) / 1000)) + " 秒";
}

function historyStatusLabel(status) {
  const value = String(status || "").toLowerCase();
  if (value === "partial") {
    return "部分成功";
  }
  if (value === "failed") {
    return "失败";
  }
  return "";
}

function workflowParamSummary(params) {
  const entries = Object.entries(params && typeof params === "object" ? params : {})
    .filter((entry) => String(entry[1] == null ? "" : entry[1]).trim() !== "");
  if (entries.length === 0) {
    return "";
  }
  const labels = {
    product_brief: "商品",
    platform: "平台",
    copy_language: "语言",
    audience: "受众",
    visual_style: "风格",
    image_resolution: "清晰度",
    templates: "出图",
    workflow_templates: "出图",
  };
  return entries.slice(0, 3).map(([key, value]) => {
    const text = String(value == null ? "" : value).trim();
    const display = text.length > 34 ? text.slice(0, 34) + "..." : text;
    return '<span class="history-param-chip">' + esc(display) + '</span>';
  }).join("");
}

function renderResultTiming(timing) {
  const label = $("#resultTiming");
  if (!label) {
    return;
  }
  const summary = timing ? formatTimingSummary(timing) : "";
  label.textContent = summary;
  label.hidden = !summary;
}

let generationPlaceholderTimer = null;
let generationPlaceholderState = null;

function stopGenerationPlaceholderTimer() {
  if (generationPlaceholderTimer) {
    clearInterval(generationPlaceholderTimer);
    generationPlaceholderTimer = null;
  }
}

function updateGenerationPlaceholderTimer(startedAt) {
  $("#gallery").querySelectorAll("[data-generation-timer]").forEach((label) => {
    label.textContent = "已耗时 " + formatElapsedSeconds(performance.now() - startedAt);
  });
}

function renderGenerationPlaceholder(message, startedAt, count = 1) {
  stopGenerationPlaceholderTimer();
  generationPlaceholderState = {
    count: Math.max(1, Number(count) || 1),
    startedAt,
    message: message || "",
  };
  setResultsVisible(true);
  $("#gallery").classList.remove("empty");
  const total = generationPlaceholderState.count;
  $("#gallery").innerHTML =
    '<div class="generation-placeholder">' +
    Array.from({ length: total }, (_, index) => (
      '<div class="gallery-item generation-placeholder-item">' +
      '<div class="generation-placeholder-frame">' +
      '<div class="generation-placeholder-image" aria-hidden="true">' +
      '<span class="generation-placeholder-image-mark"></span>' +
      '</div>' +
      '<span class="generation-placeholder-timer" data-generation-timer>已耗时 0 秒</span>' +
      '</div>' +
      '<div class="generation-placeholder-copy">' +
      '<span class="spinner"></span>' +
      '<span>' + esc(total > 1 ? (message || "正在等待图片...") : (message || "正在等待首张图片...")) + '</span>' +
      '</div>' +
      '</div>'
    )).join("") +
    '</div>';
  updateGenerationPlaceholderTimer(generationPlaceholderState.startedAt);
  generationPlaceholderTimer = setInterval(() => updateGenerationPlaceholderTimer(startedAt), 1000);
}

function historyReferencePreview(ref) {
  return ref && ref.url ? ref.url : "";
}

async function renderHistory() {
  const panel = $("#historyPanel");
  const history = await readHistory();
  if (history.length === 0) {
    panel.innerHTML = '<div class="history-empty">暂无历史记录</div>';
    return;
  }
  panel.innerHTML = history.map((item) => (
    '<article class="history-item ' + esc(String(item.status || "").toLowerCase()) + '" data-history-id="' + esc(item.id) + '">' +
    '<div class="history-head"><div><strong>' + esc(formatHistoryTime(item.created_at)) +
    (historyStatusLabel(item.status) ? ' · ' + esc(historyStatusLabel(item.status)) : '') +
    '</strong><p>' + esc(item.prompt || "") + '</p></div>' +
    '<div class="history-actions">' +
    '<button class="btn small" type="button" data-history-action="restore" data-history-id="' + esc(item.id) +
    '">' + iconHTML("rotate-ccw") + '<span>复用</span></button>' +
    '<button class="btn small danger" type="button" data-history-action="delete" data-history-id="' + esc(item.id) +
    '">' + iconHTML("trash-2") + '<span>删除</span></button></div></div>' +
    '<div class="history-meta">' + esc(item.workflow_name || item.workflow_id || "图片生成") + ' · ' +
    esc(item.image_model || "-") + ' · ' +
    esc(item.image_size || "-") + ' · ' + esc(qualityLabel(item.image_quality)) +
    ' · 参考图 ' + esc(item.reference_count || 0) + ' 张' +
    (formatTimingSummary(item) ? ' · ' + esc(formatTimingSummary(item)) : '') + '</div>' +
    (workflowParamSummary(item.workflow_params) ? '<div class="history-params">' + workflowParamSummary(item.workflow_params) + '</div>' : '') +
    (item.error ? '<div class="history-error">' + esc(item.error) + '</div>' : '') +
    '<div class="history-images">' + (item.references || []).map((ref, index) => ({ ref, index })).filter((entry) => entry.ref && entry.ref.url).map((entry) => {
      const ref = entry.ref;
      const preview = historyReferencePreview(ref);
      return '<div class="history-image-card">' +
        '<button type="button" class="history-image-preview" draggable="true" data-preview-image="' + esc(preview) + '" data-history-drag-url="' + esc(preview) + '" title="点击放大预览，拖到参考区添加">' +
        '<img src="' + esc(preview) + '" alt="历史参考图预览">' +
        '<span>点击放大预览</span>' +
        '</button>' +
        '</div>';
    }).join("") + (item.images || []).map((url) => (
      '<div class="history-image-card">' +
      '<button type="button" class="history-image-preview" draggable="true" data-preview-image="' + esc(url) + '" data-history-drag-url="' + esc(url) + '" title="点击放大预览，拖到参考区添加">' +
      '<img src="' + esc(url) + '" alt="历史图片预览">' +
      '<span>点击放大预览</span>' +
      '</button>' +
      '</div>'
    )).join("") + '</div>' +
    '</article>'
  )).join("");
}

async function restoreHistory(id) {
  const item = (await readHistory()).find((entry) => entry.id === id);
  if (!item) {
    return;
  }
  const form = $("#genForm");
  if (form.elements.workflow_id) {
    form.elements.workflow_id.value = item.workflow_id || "single";
  }
  if (window.KuaimaWorkflow && typeof window.KuaimaWorkflow.restore === "function") {
    window.KuaimaWorkflow.restore(item.workflow_id || "single", item.workflow_params || {});
  }
  form.elements.prompt.value = item.prompt || "";
  form.elements.image_model.value = item.image_model || form.elements.image_model.value;
  form.elements.image_count.value = item.image_count || form.elements.image_count.value;
  form.elements.image_quality.value = item.image_quality || form.elements.image_quality.value;
  form.elements.image_endpoint.value = item.image_endpoint || form.elements.image_endpoint.value;
  if (item.ratio) {
    $("#ratioSelect").value = item.ratio;
  }
  if (item.resolution) {
    $("#resolutionSelect").value = item.resolution;
  }
  if (window.KuaimaPromptHelper && typeof window.KuaimaPromptHelper.sync === "function") {
    window.KuaimaPromptHelper.sync();
  }
  selectedImages = await historyReferenceFiles(item);
  syncImageInput();
  syncMaskAfterImageChange();
  renderImagePreviews();
  updateSize();
  $("#genStatus").textContent = selectedImages.length > 0 ? "已恢复参数和参考图" : "已恢复参数";
  $("#genStatus").className = "status gen-status";
}

async function historyReferenceFiles(item) {
  const refs = (item.references || []).filter((ref) => ref && ref.url);
  const files = [];
  for (const ref of refs) {
    files.push(await fileFromHistoryURL(ref.url, ref.name, ref.type));
  }
  return files;
}

async function fileFromHistoryURL(url, name, type) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error("历史图片读取失败");
  }
  const blob = await response.blob();
  const file = new File([blob], name || decodeURIComponent(String(url).split("/").pop() || "history-image.png"), {
    type: type || blob.type || "image/png",
  });
  historySourceURLs.set(file, url);
  return file;
}

function renderGalleryImages(images) {
  const unique = Array.from(new Set(images || []));
  const placeholderCount = generationPlaceholderState ? Math.max(0, generationPlaceholderState.count - unique.length) : 0;
  const showingPlaceholders = placeholderCount > 0;
  if (!showingPlaceholders) {
    generationPlaceholderState = null;
    stopGenerationPlaceholderTimer();
  }
  setResultsVisible(true);
  if (unique.length === 0 && !showingPlaceholders) {
    $("#gallery").classList.add("empty");
    $("#gallery").innerHTML = '<div class="status">没有返回图片</div>';
    return;
  }
  $("#gallery").classList.remove("empty");
  const galleryItems = unique.map((url) => (
    '<div class="gallery-item"><button class="gallery-preview" type="button" data-preview-image="' + esc(url) +
    '"><img src="' + esc(url) +
    '" alt="生成结果"><span class="image-size-label" data-image-size>读取尺寸...</span></button>' +
    '</div>'
  ));
  if (showingPlaceholders) {
    galleryItems.push(...Array.from({ length: placeholderCount }, () => (
      '<div class="gallery-item generation-placeholder-item">' +
      '<div class="generation-placeholder-frame">' +
      '<div class="generation-placeholder-image" aria-hidden="true">' +
      '<span class="generation-placeholder-image-mark"></span>' +
      '</div>' +
      '<span class="generation-placeholder-timer" data-generation-timer>已耗时 0 秒</span>' +
      '</div>' +
      '<div class="generation-placeholder-copy">' +
      '<span class="spinner"></span>' +
      '<span>' + esc((generationPlaceholderState && generationPlaceholderState.message) || "正在等待图片...") + '</span>' +
      '</div>' +
      '</div>'
    )));
    if (!generationPlaceholderTimer && generationPlaceholderState) {
      generationPlaceholderTimer = setInterval(() => updateGenerationPlaceholderTimer(generationPlaceholderState.startedAt), 1000);
    }
  }
  $("#gallery").innerHTML = galleryItems.join("");
  if (showingPlaceholders && generationPlaceholderState) {
    updateGenerationPlaceholderTimer(generationPlaceholderState.startedAt);
  }
}

function renderGalleryMessage(message, className = "status") {
  stopGenerationPlaceholderTimer();
  generationPlaceholderState = null;
  setResultsVisible(Boolean(message));
  $("#gallery").classList.add("empty");
  $("#gallery").innerHTML = '<div class="' + esc(className) + '">' + esc(message) + '</div>';
}

function imageDimensionsText(img) {
  if (!img || !img.naturalWidth || !img.naturalHeight) {
    return "尺寸读取中";
  }
  return img.naturalWidth + " × " + img.naturalHeight + " px";
}

function updateLoadedImageSize(img) {
  const item = img.closest(".gallery-item");
  const label = item && item.querySelector("[data-image-size]");
  if (label) {
    label.textContent = imageDimensionsText(img);
  }
}

function openImagePreview(button) {
  const img = button.querySelector("img");
  const modalImg = $("#imageModalImg");
  const previewURL = button.dataset.previewImage || (img && img.src) || "";
  $("#imagePreviewTitle").textContent = "图片预览";
  $("#imageModalSize").textContent = "尺寸读取中";
  modalImg.onload = () => {
    $("#imageModalSize").textContent = imageDimensionsText(modalImg);
  };
  modalImg.onerror = () => {
    $("#imageModalSize").textContent = "尺寸读取失败";
  };
  modalImg.alt = img && img.alt ? img.alt : "图片预览";
  modalImg.src = previewURL;
  if (modalImg.complete && modalImg.naturalWidth) {
    $("#imageModalSize").textContent = imageDimensionsText(modalImg);
  }
  $("#imageModal").hidden = false;
}

function updateSaveDirStatus(message, isError = false) {
  const status = $("#saveDirStatus");
  status.textContent = message;
  status.className = isError ? "status error" : "status";
}

function currentOutputDir() {
  return String($("#genForm").elements.save_dir.value || "").trim();
}

async function chooseOutputDir() {
  const saveDir = currentOutputDir();
  if (!saveDir) {
    updateSaveDirStatus("请输入图片生成结果目录", true);
    return;
  }
  try {
    const result = await api("/api/output/set", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ save_dir: saveDir }),
    });
    $("#genForm").elements.save_dir.value = result.save_dir || saveDir;
    updateSaveDirStatus("已应用：" + (result.save_dir || saveDir));
  } catch (error) {
    updateSaveDirStatus(error.message, true);
  }
}

async function openDefaultOutputDir() {
  return openOutputDir(currentOutputDir());
}

async function openOutputDir(saveDir) {
  saveDir = String(saveDir || "").trim();
  if (!saveDir) {
    updateSaveDirStatus("请输入图片生成结果目录", true);
    return;
  }
  updateSaveDirStatus("正在打开输出目录...");
  try {
    const result = await api("/api/output/open", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ save_dir: saveDir }),
    });
    $("#genForm").elements.save_dir.value = result.save_dir || saveDir;
    updateSaveDirStatus("已打开：" + (result.save_dir || saveDir));
  } catch (error) {
    updateSaveDirStatus(error.message, true);
  }
}

window.KuaimaOutputDir = {
  open: openOutputDir,
};
