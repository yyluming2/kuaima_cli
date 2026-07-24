const $ = (selector) => document.querySelector(selector);

const SIZE_MATRIX = {
  "1k": {
    "1:1": "1024x1024",
    "2:3": "1024x1536",
    "3:2": "1536x1024",
  },
  "2k": {
    "1:1": "2048x2048",
    "2:3": "1440x2560",
    "3:2": "2560x1440",
  },
  "4k": {
    "1:1": "2880x2880",
    "2:3": "2160x3840",
    "3:2": "3840x2160",
  },
};

const SIZE_LOOKUP = Object.entries(SIZE_MATRIX).reduce((lookup, [resolution, ratios]) => {
  Object.entries(ratios).forEach(([ratio, size]) => {
    lookup[size] = { resolution, ratio };
  });
  return lookup;
}, {});

const HISTORY_DB_NAME = "kuaima.webui.history.v1";
const HISTORY_STORE = "items";
const HISTORY_LIMIT = 100;
const SETTINGS_KEY = "kuaima.webui.settings.v1";
const REFERENCE_IMAGE_MAX_SIDE = 1536;
const REFERENCE_IMAGE_JPEG_QUALITY = 0.82;

let selectedPayment = null;
let selectedImages = [];
let useOriginalImages = false;
let draggedImageIndex = -1;
let activeRechargeTab = "amount";
let historyDBPromise = null;
let historyStoragePersistRequested = false;
let activeGenerationController = null;
const imagePreviewURLs = new WeakMap();
const historySourceURLs = new WeakMap();
const imageFingerprintCache = new WeakMap();
let draggedHistoryImageURL = "";

function isGenerationRunning() {
  return activeGenerationController !== null;
}

window.addEventListener("beforeunload", (event) => {
  if (!isGenerationRunning()) {
    return;
  }

  const message = "图片正在生成中，关闭页面会中断当前任务。确定要离开吗？";
  event.preventDefault();
  event.returnValue = message;
  return message;
});

function setResultsVisible(visible) {
  $("#resultsPanel").hidden = !visible;
}

function esc(value) {
  return String(value == null ? "" : value).replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;",
  }[char]));
}

function iconHTML(name) {
  return '<i data-lucide="' + esc(name) + '" aria-hidden="true"></i>';
}

function unit(value) {
  if (value == "month") return "月";
  else if (value == "year") return "年";
  else if (value == "day") return "天";
  else if (value == "week") return "周";
  return value;
}

async function api(url, options) {
  const response = await fetch(url, options);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

async function apiStream(url, options, handlers) {
  const response = await fetch(url, options);
  if (!response.ok || !response.body) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || response.statusText);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const chunks = buffer.split(/\r?\n\r?\n/);
    buffer = chunks.pop() || "";
    for (const chunk of chunks) {
      const lines = chunk.split(/\r?\n/);
      let event = "message";
      const data = [];
      lines.forEach((line) => {
        if (line.startsWith("event:")) {
          event = line.slice(6).trim();
        } else if (line.startsWith("data:")) {
          data.push(line.slice(5).trimStart());
        }
      });
      if (data.length === 0) {
        continue;
      }
      const payload = JSON.parse(data.join("\n"));
      if (handlers[event]) {
        handlers[event](payload);
      }
    }
  }
}

function updateSize() {
  const ratio = $("#ratioSelect").value;
  const resolution = $("#resolutionSelect").value;
  const size = SIZE_MATRIX[resolution][ratio];
  $('[name="image_size"]').value = size;
  $("#sizePreview").textContent = size;
  document.querySelectorAll("[data-ratio]").forEach((button) => {
    button.classList.toggle("active", button.dataset.ratio === ratio);
  });
}

function resolutionCanUse4k(group) {
  const value = String(group || "default").toLowerCase();
  return value === "vip" || value === "svip";
}

function resolutionUpgradeMessage() {
  return "4k 分辨率仅限 VIP 及以上用户才能选择。";
}

function applySize(size) {
  const match = SIZE_LOOKUP[String(size || "").trim()];
  if (!match) {
    updateSize();
    return;
  }
  $("#ratioSelect").value = match.ratio;
  $("#resolutionSelect").value = match.resolution;
  updateSize();
}

function syncImageInput() {
  const input = $('[name="images"]');
  if (typeof DataTransfer === "undefined") {
    return;
  }
  const transfer = new DataTransfer();
  selectedImages.forEach((file) => transfer.items.add(file));
  input.files = transfer.files;
}

function clipboardImageFiles(event) {
  const clipboard = event && event.clipboardData;
  if (!clipboard || !clipboard.items) {
    return [];
  }
  return Array.from(clipboard.items)
    .filter((item) => item.kind === "file")
    .map((item) => item.getAsFile())
    .filter((file) => file && String(file.type || "").startsWith("image/"));
}

async function addReferenceFiles(files, sourceText) {
  const images = await dedupeReferenceFiles(files);
  if (images.length === 0) {
    $("#genStatus").textContent = "参考图已存在";
    $("#genStatus").className = "status gen-status";
    return false;
  }
  selectedImages = selectedImages.concat(images);
  syncImageInput();
  renderImagePreviews();
  $("#genStatus").textContent = "已" + (sourceText || "添加") + images.length + "张参考图";
  $("#genStatus").className = "status gen-status";
  return true;
}

function setRechargeTab(tab) {
  activeRechargeTab = tab === "plan" || tab === "code" ? tab : "amount";
  const amountTab = $("#billingTabAmount");
  const planTab = $("#billingTabPlan");
  const codeTab = $("#billingTabCode");
  const amountPanel = $("#amountOptions") && $("#amountOptions").closest(".field");
  const codePanel = $("#exchangeCodePanel");
  const planTitle = $("#rechargeStatus") && $("#rechargeStatus").closest(".section-title");
  const plansPanel = $("#plans");
  if (amountTab) {
    amountTab.classList.toggle("active", activeRechargeTab === "amount");
    amountTab.setAttribute("aria-selected", String(activeRechargeTab === "amount"));
  }
  if (planTab) {
    planTab.classList.toggle("active", activeRechargeTab === "plan");
    planTab.setAttribute("aria-selected", String(activeRechargeTab === "plan"));
  }
  if (codeTab) {
    codeTab.classList.toggle("active", activeRechargeTab === "code");
    codeTab.setAttribute("aria-selected", String(activeRechargeTab === "code"));
  }
  if (amountPanel) {
    amountPanel.hidden = activeRechargeTab !== "amount";
  }
  if (codePanel) {
    codePanel.hidden = activeRechargeTab !== "code";
  }
  if (planTitle) {
    planTitle.hidden = activeRechargeTab !== "plan";
  }
  if (plansPanel) {
    plansPanel.hidden = activeRechargeTab !== "plan";
  }
}

function imagePreviewURL(file) {
  if (!imagePreviewURLs.has(file)) {
    imagePreviewURLs.set(file, URL.createObjectURL(file));
  }
  return imagePreviewURLs.get(file);
}

function bytesToHex(bytes) {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function imageFingerprint(file) {
  if (!file) {
    return "";
  }
  const sourceURL = historySourceURLs.get(file);
  if (sourceURL) {
    return "url:" + sourceURL;
  }
  if (!imageFingerprintCache.has(file)) {
    imageFingerprintCache.set(file, (async () => {
      try {
        if (file.arrayBuffer && crypto && crypto.subtle && crypto.subtle.digest) {
          const buffer = await file.arrayBuffer();
          const digest = await crypto.subtle.digest("SHA-256", buffer);
          return "sha256:" + bytesToHex(new Uint8Array(digest));
        }
      } catch {
        // Fallback below.
      }
      return [
        "meta",
        file.name || "",
        file.size || 0,
        file.type || "",
        file.lastModified || 0,
      ].join(":");
    })());
  }
  return imageFingerprintCache.get(file);
}

async function dedupeReferenceFiles(files, existingFiles = selectedImages) {
  const unique = [];
  const seen = new Set();
  for (const file of existingFiles || []) {
    seen.add(await imageFingerprint(file));
  }
  for (const file of files || []) {
    if (!file || !String(file.type || "").startsWith("image/")) {
      continue;
    }
    const fingerprint = await imageFingerprint(file);
    if (seen.has(fingerprint)) {
      continue;
    }
    seen.add(fingerprint);
    unique.push(file);
  }
  return unique;
}

function formatFileSize(bytes) {
  const size = Number(bytes);
  if (!Number.isFinite(size)) {
    return "";
  }
  if (size < 1024 * 1024) {
    return Math.max(1, Math.round(size / 1024)) + " KB";
  }
  return (size / 1024 / 1024).toFixed(1) + " MB";
}

function useOriginalReferenceImages(form) {
  return useOriginalImages;
}

function referenceCompressionDisabled(form) {
  const hasMask = window.KuaimaMaskEditor && window.KuaimaMaskEditor.hasMask();
  return useOriginalReferenceImages(form) || hasMask;
}

function loadImageForCompression(file) {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => {
      URL.revokeObjectURL(url);
      resolve(img);
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("参考图读取失败"));
    };
    img.src = url;
  });
}

function canvasToBlob(canvas, type, quality) {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error("参考图压缩失败"));
        return;
      }
      resolve(blob);
    }, type, quality);
  });
}

function compressedReferenceName(file) {
  const name = String(file && file.name || "reference-image").replace(/\.[^.]*$/, "");
  return name + ".jpg";
}

async function compressReferenceImage(file) {
  if (!file || !String(file.type || "").startsWith("image/")) {
    return file;
  }
  if (String(file.type || "").toLowerCase() === "image/gif") {
    return file;
  }
  const img = await loadImageForCompression(file);
  const width = img.naturalWidth || img.width;
  const height = img.naturalHeight || img.height;
  if (!width || !height) {
    return file;
  }
  const scale = Math.min(1, REFERENCE_IMAGE_MAX_SIDE / Math.max(width, height));
  const targetWidth = Math.max(1, Math.round(width * scale));
  const targetHeight = Math.max(1, Math.round(height * scale));
  const canvas = document.createElement("canvas");
  canvas.width = targetWidth;
  canvas.height = targetHeight;
  const ctx = canvas.getContext("2d");
  ctx.fillStyle = "#fff";
  ctx.fillRect(0, 0, targetWidth, targetHeight);
  ctx.drawImage(img, 0, 0, targetWidth, targetHeight);
  const blob = await canvasToBlob(canvas, "image/jpeg", REFERENCE_IMAGE_JPEG_QUALITY);
  if (blob.size >= file.size && scale === 1) {
    return file;
  }
  return new File([blob], compressedReferenceName(file), {
    type: "image/jpeg",
    lastModified: file.lastModified || Date.now(),
  });
}

async function referenceFilesForSubmit(form) {
  if (referenceCompressionDisabled(form)) {
    return selectedImages;
  }
  const compressed = [];
  for (const file of selectedImages) {
    try {
      compressed.push(await compressReferenceImage(file));
    } catch (error) {
      console.warn("Reference image compression failed", error);
      compressed.push(file);
    }
  }
  return compressed;
}

async function buildGenerateFormData(form) {
  const formData = new FormData(form);
  formData.delete("images");
  const files = await referenceFilesForSubmit(form);
  files.forEach((file) => {
    formData.append("images", file, file.name || "reference-image.png");
  });
  return formData;
}

function renderImagePreviews() {
  const list = $("#imagePreviewList");
  if (selectedImages.length === 0) {
    list.className = "image-preview-list empty";
    list.textContent = "可以拖放参考图片到这里";
    return;
  }

  list.className = "image-preview-list";
  list.innerHTML = (
    '<div class="reference-options">' +
    '<label class="original-image-toggle" title="开启后参考图按原始文件提交">' +
    '<input id="useOriginalImages" type="checkbox" value="1"' + (useOriginalImages ? ' checked' : '') + '>' +
    '<span>使用原图</span>' +
    '</label>' +
    '</div>'
  ) + selectedImages.map((file, index) => (
    (() => {
      const previewURL = imagePreviewURL(file);
      return (
        '<div class="image-preview-item" draggable="true" data-image-index="' + index + '">' +
        '<img src="' + esc(previewURL) + '" alt="' + esc(file.name) + '">' +
        '<div class="image-preview-meta"><strong>' + esc(file.name) + '</strong><span>' +
        esc(formatFileSize(file.size)) + ' · 拖动可调整顺序</span></div>' +
        '<div class="image-preview-actions">' +
        '<button type="button" title="预览查看" data-preview-image="' + esc(previewURL) + '">' +
        iconHTML("eye") + '<span>预览查看</span></button>' +
        '<button type="button" title="删除" data-image-action="remove" data-image-index="' + index + '">' +
        iconHTML("trash-2") + '<span class="sr-only">删除</span></button>' +
        '</div>' +
        '</div>'
      );
    })()
  )).join("");
}

function syncMaskAfterImageChange() {
  if (!window.KuaimaMaskEditor) {
    return;
  }
  const first = selectedImages[0] || null;
  if (first && window.KuaimaMaskEditor.usesFile(first)) {
    return;
  }
  if (window.KuaimaMaskEditor.hasMask()) {
    window.KuaimaMaskEditor.disable();
  } else {
    window.KuaimaMaskEditor.loadReference(null);
  }
}

function moveImage(from, to) {
  if (from === to || to < 0 || to >= selectedImages.length) {
    return;
  }
  const [file] = selectedImages.splice(from, 1);
  selectedImages.splice(to, 0, file);
  syncImageInput();
  syncMaskAfterImageChange();
  renderImagePreviews();
}

function formatAmount(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return String(value || "");
  }
  return Number.isInteger(number) ? String(number) : number.toFixed(2).replace(/0+$/, "").replace(/\.$/, "");
}

function discountText(discount) {
  const value = Number(discount);
  if (!value || value >= 1) {
    return "";
  }
  return (value * 10).toFixed(1).replace(/\.0$/, "") + " 折";
}

function formatQuota(value) {
  const quota = Number(value);
  if (!Number.isFinite(quota) || quota <= 0) {
    return "-";
  }
  return Math.round(quota) + " (￥" + (quota / 500000).toFixed(2) + ")";
}

function qualityLabel(value) {
  return ({
    auto: "自动",
    low: "快速",
    medium: "中等",
    high: "高清",
  })[value] || value || "-";
}

function endpointLabel(value) {
  return value === "response" ? "Response 接口" : "Image 接口";
}

function readSettings() {
  try {
    const value = JSON.parse(localStorage.getItem(SETTINGS_KEY) || "{}");
    return value && typeof value === "object" ? value : {};
  } catch {
    return {};
  }
}

function saveSettings() {
  syncSingleGenerationControls();
  const form = $("#genForm");
  const settings = {
    image_model: form.elements.image_model.value,
    image_count: form.elements.image_count.value,
    image_quality: form.elements.image_quality.value,
    image_endpoint: form.elements.image_endpoint.value || "response",
    use_original_images: useOriginalReferenceImages(form),
    ratio: $("#ratioSelect").value,
    resolution: $("#resolutionSelect").value,
  };
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
}

function applySettings(settings) {
  const form = $("#genForm");
  if (settings.image_model) {
    form.elements.image_model.value = settings.image_model;
  }
  if (settings.image_count) {
    form.elements.image_count.value = settings.image_count;
  }
  if (settings.image_quality) {
    form.elements.image_quality.value = settings.image_quality;
  }
  form.elements.image_endpoint.value = settings.image_endpoint || form.elements.image_endpoint.value || "response";
  useOriginalImages = !!settings.use_original_images;
  if (settings.ratio) {
    $("#ratioSelect").value = settings.ratio;
  }
  if (settings.resolution) {
    $("#resolutionSelect").value = settings.resolution;
  }
  updateSize();
  syncSingleGenerationControls();
}

function generationAccountGroup() {
  return String((window.KuaimaAccount && window.KuaimaAccount.group) || "default").toLowerCase();
}

function generationImageCountLimit() {
  return Math.max(1, Number(window.KuaimaAccount && window.KuaimaAccount.maxConcurrency) || 1);
}

function generationImageCountHint(group, limit) {
  const value = String(group || "default").toLowerCase();
  const label = value === "svip" ? "SVIP" : value === "vip" ? "VIP" : "普通用户";
  return label + "一次最多生成 " + limit + " 张。";
}

function syncSingleGenerationControls() {
  const form = $("#genForm");
  const input = form && form.elements && form.elements.image_count ? form.elements.image_count : null;
  if (!input) {
    return;
  }
  const group = generationAccountGroup();
  const limit = generationImageCountLimit();
  input.max = String(limit);
  const current = Number(input.value || 1);
  input.value = String(Math.max(1, Math.min(limit, Number.isFinite(current) ? current : 1)));
  input.title = generationImageCountHint(group, limit);
  const note = $("#singleGenerationNote");
  if (note) {
    note.textContent = generationImageCountHint(group, limit);
  }
}

function looksLikeInsufficientBalance(message) {
  const lower = String(message || "").toLowerCase();
  return [
    "insufficient_quota",
    "insufficient quota",
    "insufficient credits",
    "insufficient balance",
    "out of credits",
    "quota exceeded",
    "balance_not_enough",
    "not enough balance",
    "余额不足",
    "额度不足",
    "额度失败",
    "余额不够",
    "额度不够",
    "欠费",
    "recharge",
    "充值",
  ].some((needle) => lower.includes(needle));
}

function handleOperationError(error, statusElement) {
  statusElement.textContent = error.message;
  statusElement.className = "status gen-status error";
  if (looksLikeInsufficientBalance(error.message)) {
    setBillingOpen(true);
    setRechargeTab("plan");
    loadRechargeInfo();
  }
}
