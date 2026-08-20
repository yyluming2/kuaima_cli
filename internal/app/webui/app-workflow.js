(function () {
  const STORAGE_KEY = "kuaima.webui.workflow.v1";
  const state = {
    catalog: [],
    cache: readWorkflowCache(),
    activeStepId: "",
    pendingReview: null,
    lastResult: null,
    accountGroup: "default",
    maxConcurrency: 1,
    runStartedAt: 0,
    progressTimer: 0,
  };

  function readWorkflowCache() {
    try {
      const value = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
      return value && typeof value === "object" ? value : {};
    } catch {
      return {};
    }
  }

  function saveWorkflowCache() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state.cache));
    } catch {
      // Ignore storage failures.
    }
  }

  function workflowIdInput() {
    return $("#workflowId");
  }

  function workflowTabs() {
    return $("#workflowTabs");
  }

  function workflowTemplatesInput() {
    return $("#workflowTemplates");
  }

  function workflowFields() {
    return $("#workflowFields");
  }

  function workflowTemplatePicker() {
    return $("#workflowTemplatePicker");
  }

  function workflowRunOptions() {
    return $("#workflowRunOptions");
  }

  function promptTextGroup() {
    return $("#promptTextGroup");
  }

  function singleGenerationControls() {
    return $("#singleGenerationControls");
  }

  function workflowSteps() {
    return $("#workflowSteps");
  }

  function workflowReview() {
    return $("#workflowReview");
  }

  function promptFieldLabel() {
    return $("#promptFieldLabel");
  }

  function referenceUploadLabel() {
    return $("#referenceUploadLabel");
  }

  function workflowDef(id) {
    return state.catalog.find((item) => String(item.id || "") === String(id || ""));
  }

  function currentWorkflowId() {
    const input = workflowIdInput();
    return input ? String(input.value || "single") : "single";
  }

  function workflowLabel(id) {
    if (String(id || "") === "single") {
      return "自由生图";
    }
    if (String(id || "") === "agent") {
      return "对话模式";
    }
    const def = workflowDef(id);
    return def ? def.name : String(id || "");
  }

  function openWorkflowCustomizationModal() {
    const modal = $("#imageModal");
    const title = $("#imagePreviewTitle");
    const size = $("#imageModalSize");
    const img = $("#imageModalImg");
    if (!modal || !title || !size || !img) {
      return;
    }
    title.textContent = "工作流定制";
    size.textContent = "联系客服添加微信";
    img.src = "imgs/qrcode.png";
    img.alt = "联系客服添加微信二维码";
    modal.dataset.previewUrl = "";
    const fineEdit = $("#openFineEditor");
    if (fineEdit) fineEdit.hidden = true;
    modal.hidden = false;
  }

  function currentWorkflowValues() {
    const fields = workflowFields();
    const values = {};
    if (!fields || fields.hidden) {
      const templates = currentWorkflowTemplateIDs();
      if (templates.length > 0) {
        values.templates = templates.join(",");
      }
      return values;
    }
    fields.querySelectorAll("[data-workflow-key]").forEach((input) => {
      const key = input.dataset.workflowKey;
      values[key] = String(input.value || "").trim();
    });
    const templates = currentWorkflowTemplateIDs();
    if (templates.length > 0) {
      values.templates = templates.join(",");
    }
    return values;
  }

  function openWorkflowContactModal() {
    const modal = $("#workflowContactModal");
    const close = $("#closeWorkflowContactModal");
    if (!modal) {
      return;
    }
    modal.hidden = false;
    if (close) {
      close.focus();
    }
  }

  function closeWorkflowContactModal() {
    const modal = $("#workflowContactModal");
    if (modal) {
      modal.hidden = true;
    }
  }

  function renderWorkflowTabs() {
    const container = workflowTabs();
    if (!container) {
      return;
    }
    const current = currentWorkflowId();
    const agentPane = document.querySelector('[data-mode-pane="agent"]');
    const agentActive = Boolean(agentPane && !agentPane.hidden);
    const videoPane = document.querySelector('[data-mode-pane="video"]');
    const videoActive = Boolean(videoPane && !videoPane.hidden);
    const items = [{ id: "single", name: "图片生成" }];
    const workflowButtons = items.map((item) => (
      '<button class="workflow-tab' + (!agentActive && String(item.id) === String(current) ? " active" : "") + '" type="button" role="tab" ' +
      'aria-selected="' + String(!agentActive && String(item.id) === String(current)) + '" data-mode-tab="generate" data-workflow-id="' + esc(item.id) + '">' +
      iconHTML("image-plus") + '<span>' + esc(item.name) + '</span></button>'
    )).join("");
    const catalogButtons = state.catalog.map((item) => (
      '<button class="workflow-tab' + (!agentActive && String(item.id) === String(current) ? " active" : "") + '" type="button" role="tab" ' +
      'aria-selected="' + String(!agentActive && String(item.id) === String(current)) + '" data-mode-tab="generate" data-workflow-id="' + esc(item.id) + '">' +
      iconHTML(item.icon || "workflow") + '<span>' + esc(item.name) + '</span></button>'
    )).join("");
    container.innerHTML = workflowButtons +
      '<button class="workflow-tab' + (videoActive ? " active" : "") + '" type="button" role="tab" aria-selected="' +
      String(videoActive) + '" data-mode-tab="video">' + iconHTML("video") + '<span>视频生成</span></button>' +
      '<button class="workflow-tab" type="button" role="tab" aria-selected="false" data-fine-edit-tab>' +
      iconHTML("scan-search") + '<span>图片编辑</span></button>' +
      '<button class="workflow-tab" type="button" role="tab" aria-selected="false" data-cutout-tab>' +
      iconHTML("scissors") + '<span>一键抠图</span></button>' +
      '<button class="workflow-tab' + (agentActive ? " active" : "") + '" type="button" role="tab" aria-selected="' +
      String(agentActive) + '" data-mode-tab="agent" data-workflow-id="agent">' + iconHTML("bot-message-square") + '<span>对话模式</span></button>' +
      //'<button class="workflow-tab" type="button" role="tab" aria-selected="false" data-mode-tab="generate" data-workflow-id="workflow-custom">' +
      //iconHTML("sparkles") + '<span>联系客服</span></button>' +
      catalogButtons;
  }

  function buildInputValue(field, cached) {
    if (cached != null && String(cached).trim() !== "") {
      return String(cached);
    }
    if (field && field.default != null && String(field.default).trim() !== "") {
      return String(field.default);
    }
    return "";
  }

  function workflowFieldByKey(key) {
    const def = workflowDef(currentWorkflowId());
    return (def && Array.isArray(def.inputs) ? def.inputs : []).find((field) => String(field.key || "") === String(key || "")) || null;
  }

  function workflowOptionForValue(field, value) {
    const text = String(value || "").trim();
    return (field && Array.isArray(field.options) ? field.options : []).find((option) => (
      String(option.value || "").trim() === text || String(option.label || "").trim() === text
    )) || null;
  }

  function workflowFieldIconHTML(field, value) {
    const option = workflowOptionForValue(field, value);
    const src = option && option.icon ? String(option.icon) : "";
    if (!src) {
      return "";
    }
    return '<img src="' + esc(src) + '" alt="' + esc(String(value || "").trim()) + '" loading="lazy">';
  }

  function workflowOptionExampleHTML(field, value) {
    const option = workflowOptionForValue(field, value);
    const src = option && option.example ? String(option.example) : "";
    if (!src) {
      return "";
    }
    const label = option.label || option.value || field.label || field.key || "示例";
    return '<figure class="workflow-field-example" data-workflow-option-example><img src="' + esc(src) + '" alt="' + esc(label) + '示例" loading="lazy"></figure>';
  }

  function workflowFieldHelpHTML(field) {
    if (String(field && field.key || "") === "image_resolution") {
      return '<div class="workflow-help">4k 分辨率仅限 VIP 及以上用户。</div>';
    }
    return field && field.help ? '<div class="workflow-help">' + esc(field.help) + '</div>' : "";
  }

  function workflowOptionDisplayLabel(field, option) {
    const label = option.label || option.value || "";
    if (String(field && field.key || "") === "image_resolution" && String(option && option.value || "") === "4k") {
      return label + "（VIP+）";
    }
    return label;
  }

  function workflowComboClass(field) {
    return field.key === "platform" ? " platform" : field.key === "copy_language" ? " language" : "";
  }

  function workflowOptionLabel(field, option) {
    const value = option && option.value != null ? String(option.value) : "";
    const icon = workflowFieldIconHTML(field, value);
    const example = option && option.example ? '<img class="workflow-option-example" src="' + esc(option.example) + '" alt="' + esc(option.label || option.value || "示例") + '示例" loading="lazy">' : "";
    return (icon ? '<span class="workflow-option-icon">' + icon + '</span>' : "") +
      example +
      '<span>' + esc(workflowOptionDisplayLabel(field, option)) + '</span>';
  }

  function workflowNormalizeResolutionInput(input, options = {}) {
    if (!input || String(input.dataset.workflowKey || "") !== "image_resolution") {
      return true;
    }
    const value = String(input.value || "").trim().toLowerCase();
    if (value !== "4k" || resolutionCanUse4k(workflowAccountGroup())) {
      input.dataset.previousResolution = input.value;
      return true;
    }
    input.value = input.dataset.previousResolution || "2k";
    if (options.notify !== false) {
      alert(resolutionUpgradeMessage());
    }
    if (options.notify !== false && window.KuaimaBilling && typeof window.KuaimaBilling.openPlans === "function") {
      window.setTimeout(() => window.KuaimaBilling.openPlans(), 0);
    }
    return false;
  }

  function syncWorkflowResolutionState(container) {
    const root = container || workflowFields();
    if (!root) {
      return;
    }
    root.querySelectorAll('[data-workflow-key="image_resolution"]').forEach((input) => {
      input.dataset.previousResolution = input.value;
    });
  }

  function renderWorkflowField(field, cached) {
    const name = "workflow_" + field.key;
    const value = buildInputValue(field, cached[field.key]);
    const help = workflowFieldHelpHTML(field);
    let control = "";
    if (field.type === "textarea") {
      control = '<div class="workflow-textarea-group">' +
        '<textarea data-workflow-key="' + esc(field.key) + '" name="' + esc(name) + '" placeholder="' +
        esc(field.placeholder || "") + '"' + (field.required ? " required" : "") + ">" + esc(value) + "</textarea>" +
        '<button class="btn small workflow-rewrite" type="button" data-workflow-rewrite>' +
        iconHTML("wand-sparkles") + '<span>AI改写</span></button>' +
        '</div>';
    } else if (field.type === "combo") {
      const icon = workflowFieldIconHTML(field, value);
      control = '<div class="workflow-combo' + workflowComboClass(field) + '" data-workflow-combo>' +
        '<div class="workflow-combo-control' + (icon ? " has-icon" : "") + '">' +
        (icon ? '<span class="workflow-combo-icon" data-workflow-combo-icon>' + icon + '</span>' : "") +
        '<input data-workflow-key="' + esc(field.key) + '" name="' + esc(name) + '" type="text" value="' +
        esc(value) + '" placeholder="' + esc(field.placeholder || "") + '"' +
        (field.required ? " required" : "") + ' autocomplete="off">' +
        '<button class="workflow-combo-toggle" type="button" aria-label="展开选项" data-workflow-combo-toggle>v</button>' +
        '</div>' +
        '<div class="workflow-combo-menu" data-workflow-combo-menu hidden>' +
        (field.options || []).map((option) => (
          '<button type="button" data-workflow-combo-option="' + esc(option.value) + '">' +
          workflowOptionLabel(field, option) + '</button>'
        )).join("") + "</div></div>";
    } else if (field.type === "select") {
      control = '<select data-workflow-key="' + esc(field.key) + '" name="' + esc(name) + '">' +
        (field.options || []).map((option) => (
          '<option value="' + esc(option.value) + '"' + (String(option.value) === String(value) ? " selected" : "") + '>' +
          esc(workflowOptionDisplayLabel(field, option)) + "</option>"
        )).join("") + "</select>";
    } else if (field.type === "number") {
      control = '<input data-workflow-key="' + esc(field.key) + '" name="' + esc(name) + '" type="number" value="' +
        esc(value) + '"' +
        (field.min != null ? ' min="' + esc(field.min) + '"' : "") +
        (field.max != null ? ' max="' + esc(field.max) + '"' : "") +
        (field.required ? " required" : "") +
        '>';
    } else {
      control = '<input data-workflow-key="' + esc(field.key) + '" name="' + esc(name) + '" type="' +
        esc(field.type || "text") + '" value="' + esc(value) + '" placeholder="' + esc(field.placeholder || "") + '"' +
        (field.required ? " required" : "") + ">";
    }
    const example = field.example ? '<figure class="workflow-field-example"><img src="' + esc(field.example) + '" alt="' + esc(field.label || field.key) + '示例" loading="lazy"></figure>' : workflowOptionExampleHTML(field, value);
    return '<div class="workflow-input" data-workflow-field="' + esc(field.key) + '">' +
      '<label>' + esc(field.label || field.key) + (field.required ? '<span class="workflow-required">*</span>' : "") + "</label>" +
      control +
      help +
      example +
      "</div>";
  }

  function workflowFieldSummary(container) {
    if (!container) {
      return "";
    }
    const summaryKeys = ["platform", "copy_language", "audience", "visual_style", "image_resolution"];
    return summaryKeys.map((key) => {
      const input = container.querySelector('[data-workflow-key="' + key + '"]');
      const value = input ? String(input.value || "").trim() : "";
      if (!value) {
        return "";
      }
      const icon = workflowFieldIconHTML(workflowFieldByKey(key), value);
      return '<span class="workflow-summary-chip">' + (icon ? '<span class="workflow-summary-icon">' + icon + '</span>' : "") +
        '<span>' + esc(value) + '</span></span>';
    }).filter(Boolean).join("");
  }

  function updateWorkflowSectionSummaries() {
    const container = workflowFields();
    if (!container) {
      return;
    }
    const target = container.querySelector("[data-workflow-config-summary]");
    if (target) {
      target.innerHTML = workflowFieldSummary(container) || '<span class="workflow-summary-chip">未配置</span>';
    }
    container.querySelectorAll("[data-workflow-combo]").forEach((combo) => {
      const input = combo.querySelector("[data-workflow-key]");
      if (!input) {
        return;
      }
      updateWorkflowComboIcon(combo, input);
      updateWorkflowComboExample(combo, input);
    });
  }

  function updateWorkflowComboIcon(combo, input) {
    const control = combo && combo.querySelector(".workflow-combo-control");
    const iconHTMLValue = workflowFieldIconHTML(workflowFieldByKey(input.dataset.workflowKey), input.value);
    let icon = combo && combo.querySelector("[data-workflow-combo-icon]");
    if (!control) {
      return;
    }
    if (!iconHTMLValue) {
      if (icon) {
        icon.remove();
      }
      control.classList.remove("has-icon");
      return;
    }
    if (!icon) {
      icon = document.createElement("span");
      icon.className = "workflow-combo-icon";
      icon.dataset.workflowComboIcon = "";
      control.insertBefore(icon, input);
    }
    icon.innerHTML = iconHTMLValue;
    control.classList.add("has-icon");
  }

  function updateWorkflowComboExample(combo, input) {
    const field = workflowFieldByKey(input.dataset.workflowKey);
    const wrapper = combo && combo.closest(".workflow-input");
    const nextHTML = workflowOptionExampleHTML(field, input.value);
    const current = wrapper && wrapper.querySelector("[data-workflow-option-example]");
    if (!wrapper) {
      return;
    }
    if (!nextHTML) {
      if (current) {
        current.remove();
      }
      return;
    }
    if (current) {
      current.outerHTML = nextHTML;
    } else {
      combo.insertAdjacentHTML("afterend", nextHTML);
    }
  }

  function renderWorkflowFields(def) {
    const container = workflowFields();
    const status = $("#genStatus");
    if (!container || !status) {
      return;
    }
    if (!def || String(def.id || "") === "single") {
      container.hidden = true;
      container.innerHTML = "";
      status.textContent = "自由生图";
      status.className = "status gen-status";
      return;
    }

    const cached = state.cache[def.id] || {};
    status.textContent = def.description || def.name || workflowLabel(def.id);
    status.className = "status gen-status";
    container.hidden = false;
    const inputs = def.inputs || [];
    if (String(def.id || "") === "product-pack") {
      const mainFields = inputs.filter((field) => field.key === "product_brief");
      const configFields = inputs.filter((field) => field.key !== "product_brief");
      container.innerHTML =
        '<section class="workflow-section">' +
        '<div class="workflow-section-head"><span class="workflow-step-index">1</span><div><strong>商品信息</strong><span>先描述产品，再补充图片参考。</span></div></div>' +
        mainFields.map((field) => renderWorkflowField(field, cached)).join("") +
        '</section>' +
        '<details class="workflow-section workflow-collapsible">' +
        '<summary><span class="workflow-step-index">2</span><div><strong>配置偏好</strong><span data-workflow-config-summary></span></div></summary>' +
        '<div class="workflow-section-grid">' + configFields.map((field) => renderWorkflowField(field, cached)).join("") + '</div>' +
        '</details>';
      updateWorkflowSectionSummaries();
      return;
    }
    container.innerHTML = inputs.map((field) => renderWorkflowField(field, cached)).join("");
    syncWorkflowResolutionState(container);
  }

  function workflowImageSteps(def) {
    return (def && Array.isArray(def.steps) ? def.steps : []).filter((step) => (
      String(step.kind || "").toLowerCase() === "image"
    ));
  }

  function workflowResponseSteps(def) {
    return (def && Array.isArray(def.steps) ? def.steps : []).filter((step) => (
      String(step.kind || "").toLowerCase() === "response"
    ));
  }

  function workflowNeedsReview(def) {
    let seenResponse = false;
    for (const step of (def && Array.isArray(def.steps) ? def.steps : [])) {
      const kind = String(step.kind || "").toLowerCase();
      if (kind === "response") {
        seenResponse = true;
      } else if (kind === "image" && seenResponse) {
        return true;
      }
    }
    return false;
  }

  function workflowAutoRunEnabled() {
    const input = $("#workflowAutoRun");
    return Boolean(input && input.checked);
  }

  function selectedWorkflowSteps(def, options = {}) {
    const selectedTemplates = new Set(currentWorkflowTemplateIDs());
    const includeImages = options.includeImages !== false;
    return (def && Array.isArray(def.steps) ? def.steps : []).filter((step) => {
      const kind = String(step.kind || "").toLowerCase();
      if (kind === "response") {
        return true;
      }
      if (kind === "image" && includeImages) {
        return selectedTemplates.has(String(step.id || ""));
      }
      return false;
    });
  }

  function workflowProgressTotal(def, mode) {
    return selectedWorkflowSteps(def, { includeImages: String(mode || "") !== "draft" }).length;
  }

  function formatWorkflowDuration(value) {
    if (typeof formatDuration === "function") {
      return formatDuration(value);
    }
    const duration = Number(value);
    if (!Number.isFinite(duration) || duration < 0) {
      return "-";
    }
    return duration < 1000 ? Math.max(1, Math.round(duration)) + "ms" : (duration / 1000).toFixed(1).replace(/\.0$/, "") + "s";
  }

  function stepDurationMs(step) {
    const started = Number(step && step.started_at_ms);
    if (!Number.isFinite(started) || started <= 0) {
      return null;
    }
    const ended = Number(step.ended_at_ms);
    const current = Number.isFinite(ended) && ended >= started ? ended : performance.now();
    return Math.max(0, current - started);
  }

  function renderWorkflowRunOptions(def) {
    const container = workflowRunOptions();
    if (!container) {
      return;
    }
    if (!def || String(def.id || "") === "single") {
      container.hidden = true;
      container.innerHTML = "";
      return;
    }
    const needsReview = workflowNeedsReview(def);
    const checked = state.cache.skipReview === true ? " checked" : "";
    const soundChecked = state.cache.playDoneSound !== false ? " checked" : "";
    container.hidden = false;
    container.innerHTML = '<details class="workflow-run-options-compact" open>' +
      '<summary>' + iconHTML("settings-2") + '<span>运行选项</span></summary>' +
      '<div class="workflow-run-options-body">' +
      (needsReview ? '<label class="workflow-run-toggle workflow-run-toggle-inline">' +
        '<input id="workflowAutoRun" type="checkbox"' + checked + '>' +
        '<span class="workflow-run-toggle-box">' + iconHTML("fast-forward") + '</span>' +
        '<span>无需确认直接运行</span>' +
        '</label>' : "") +
      '<label class="workflow-run-toggle workflow-run-toggle-inline">' +
      '<input id="workflowPlayDoneSound" type="checkbox"' + soundChecked + '>' +
      '<span class="workflow-run-toggle-box">' + iconHTML("volume-2") + '</span>' +
      '<span>完成后播放声音</span>' +
      '</label>' +
      '</div></details>';
  }

  function workflowAccountGroup() {
    const value = String(state.accountGroup || (window.KuaimaAccount && window.KuaimaAccount.group) || "default").toLowerCase();
    return value === "svip" || value === "vip" ? value : "default";
  }

  function workflowConcurrencyForGroup() {
    return Math.max(1, Number(state.maxConcurrency || (window.KuaimaAccount && window.KuaimaAccount.maxConcurrency)) || 1);
  }

  function workflowGroupLabel(group) {
    return group === "svip" ? "SVIP" : group === "vip" ? "VIP" : "普通用户";
  }

  function workflowNextPlan(group) {
    if (group === "svip") {
      return null;
    }
    return group === "vip" ? { group: "svip", label: "SVIP" } : { group: "vip", label: "VIP" };
  }

  function workflowSpeedHint(group, selectedCount) {
    const current = workflowConcurrencyForGroup();
    return selectedCount > current ? "将按每批 " + current + " 张并发完成" : "可一次并发完成";
  }

  function workflowConcurrencySummary(group) {
    return workflowGroupLabel(group) + "可同时生成 " + workflowConcurrencyForGroup() + " 张";
  }

  function workflowOrderedImages(result) {
    const ordered = [];
    const seen = new Set();
    const push = (url) => {
      const value = String(url || "").trim();
      if (!value || seen.has(value)) {
        return;
      }
      seen.add(value);
      ordered.push(value);
    };
    const steps = Array.isArray(result && result.steps) ? result.steps : [];
    steps.forEach((step) => {
      (Array.isArray(step.images) ? step.images : []).forEach(push);
    });
    (Array.isArray(result && result.images) ? result.images : []).forEach(push);
    return ordered;
  }

  function workflowProgressMetaHTML(def, result) {
    const group = workflowAccountGroup();
    const selectedCount = currentWorkflowTemplateIDs().length || workflowDefaultTemplateIDs(def).length;
    const next = workflowNextPlan(group);
    return '<div class="workflow-progress-meta">' +
      '<span class="workflow-progress-chip">' + esc(workflowConcurrencySummary(group)) + '</span>' +
      '<span class="workflow-progress-chip">' + esc("本次选择 " + selectedCount + " 张") + '</span>' +
      workflowResultDirHTML(result) +
      (next ? '<button class="btn small primary" type="button" data-workflow-upgrade>' +
        iconHTML("badge-plus") + '<span>升级到' + esc(next.label) + '</span></button>' : "") +
      '</div>';
  }

  function workflowResultDirHTML(result) {
    const dir = String(result && result.output_dir || "").trim();
    if (!dir) {
      return "";
    }
    return '<button class="workflow-result-dir" type="button" data-workflow-open-output-dir="' + esc(dir) + '">' +
      iconHTML("folder-open") +
      '<span>打开文件目录</span>' +
      '</button>';
  }

  async function openWorkflowOutputDir(saveDir) {
    saveDir = String(saveDir || "").trim();
    if (!saveDir) {
      return;
    }
    try {
      await api("/api/output/open", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ save_dir: saveDir }),
      });
    } catch (error) {
      const status = $("#genStatus");
      if (status) {
        status.textContent = error && error.message ? error.message : "打开输出目录失败";
        status.className = "status gen-status error";
      }
    }
  }

  function workflowDefaultTemplateIDs(def) {
    return workflowImageSteps(def).map((step) => String(step.id || "").trim()).filter(Boolean);
  }

  function currentWorkflowTemplateIDs() {
    const input = workflowTemplatesInput();
    const def = workflowDef(currentWorkflowId());
    const defaults = workflowDefaultTemplateIDs(def);
    const raw = input ? String(input.value || "").trim() : "";
    if (!raw) {
      return defaults;
    }
    const allowed = new Set(defaults);
    const selected = raw.split(",").map((item) => String(item || "").trim()).filter((item) => item && allowed.has(item));
    return selected.length > 0 ? selected : defaults;
  }

  function setWorkflowTemplateIDs(ids) {
    const input = workflowTemplatesInput();
    const def = workflowDef(currentWorkflowId());
    const allowed = new Set(workflowImageSteps(def).map((step) => String(step.id || "").trim()).filter(Boolean));
    const values = Array.isArray(ids) ? ids.map((item) => String(item || "").trim()).filter((item) => item && allowed.has(item)) : [];
    const finalValues = values.length > 0 ? values : Array.from(allowed);
    if (input) {
      input.value = finalValues.join(",");
    }
    const container = workflowTemplatePicker();
    if (container) {
      container.querySelectorAll("[data-workflow-template]").forEach((box) => {
        const selected = finalValues.includes(box.dataset.workflowTemplate || "");
        box.checked = selected;
        const card = box.closest(".workflow-template-card");
        if (card) {
          card.classList.toggle("active", selected);
        }
      });
      renderWorkflowTemplateSummary(def, finalValues);
      renderWorkflowRunOptions(def);
    }
  }

  function renderWorkflowTemplates(def) {
    const container = workflowTemplatePicker();
    if (!container) {
      return;
    }
    const templates = workflowImageSteps(def);
    if (!def || String(def.id || "") === "single" || templates.length === 0) {
      container.hidden = true;
      container.innerHTML = "";
      return;
    }
    container.hidden = false;
    container.innerHTML =
      '<section class="workflow-section workflow-template-section">' +
      '<div class="workflow-section-head"><span class="workflow-step-index">3</span><div><strong>出图类型</strong><span>已选择的主图和详情图会按顺序生成。</span></div></div>' +
      '<div class="workflow-template-summary" data-workflow-template-summary></div>' +
      '<button class="btn small workflow-template-add" type="button" data-workflow-template-open>' +
      iconHTML("plus") + '<span>选择图片</span></button>' +
      '</section>' +
      '<div class="modal-backdrop workflow-template-modal-backdrop" data-workflow-template-modal hidden>' +
      '<div class="workflow-template-modal" role="dialog" aria-modal="true" aria-labelledby="workflowTemplateTitle">' +
      '<button class="modal-close" type="button" aria-label="关闭" data-workflow-template-close>×</button>' +
      '<div class="workflow-template-modal-head"><p class="eyebrow">Templates</p><h2 id="workflowTemplateTitle">选择出图类型</h2></div>' +
      '<div class="workflow-template-modal-body">' +
      renderWorkflowTemplateGroup(def, "主图", "main", templates.filter((step) => workflowTemplateType(step) === "main")) +
      renderWorkflowTemplateGroup(def, "详情图", "detail", templates.filter((step) => workflowTemplateType(step) === "detail")) +
      '</div></div></div>';
    setWorkflowTemplateIDs(currentWorkflowTemplateIDs());
  }

  function workflowTemplateType(step) {
    if (step && step.template_type) {
      return String(step.template_type);
    }
    const title = String(step && step.title || "");
    return title.includes("主图") ? "main" : title.includes("详情图") ? "detail" : "other";
  }

  function workflowTemplateShortTitle(step) {
    return String(step && (step.title || step.id) || "模板").replace(/^主图\s*·\s*/, "").replace(/^详情图\s*·\s*/, "");
  }

  function renderWorkflowTemplateGroup(def, title, type, items) {
    if (!items.length) {
      return "";
    }
    return '<div class="workflow-template-modal-group">' +
      '<h3>' + esc(title) + '</h3>' +
      '<div class="workflow-template-grid">' + items.map((step) => (
        '<label class="workflow-template-card" data-workflow-template-card="' + esc(step.id) + '">' +
        '<input type="checkbox" data-workflow-template="' + esc(step.id) + '" checked>' +
        (step.example ? '<figure class="workflow-template-example"><img src="' + esc(step.example) + '" alt="' + esc(workflowTemplateShortTitle(step)) + '示例" loading="lazy"></figure>' : "") +
        '<div><strong>' + esc(workflowTemplateShortTitle(step)) + '</strong>' +
        '<p>' + esc(workflowStepMeta(step)) + '</p></div>' +
        '</label>'
      )).join("") + "</div></div>";
  }

  function renderWorkflowTemplateSummary(def, selectedIds) {
    const container = workflowTemplatePicker();
    const target = container && container.querySelector("[data-workflow-template-summary]");
    if (!target) {
      return;
    }
    const selected = workflowImageSteps(def).filter((step) => selectedIds.includes(String(step.id || "")));
    const main = selected.filter((step) => workflowTemplateType(step) === "main");
    const detail = selected.filter((step) => workflowTemplateType(step) === "detail");
    target.innerHTML = renderSelectedTemplateBlock("主图", main, "image") +
      renderSelectedTemplateBlock("详情图", detail, "images");
  }

  function renderSelectedTemplateBlock(title, items, icon) {
    return '<div class="workflow-selected-block">' +
      '<div class="workflow-selected-head">' + iconHTML(icon) + '<strong>' + esc(title) + '</strong><span>' + items.length + ' 项</span></div>' +
      '<div class="workflow-selected-list">' +
      (items.length ? items.map((step) => (
        '<span class="workflow-selected-chip">' + esc(workflowTemplateShortTitle(step)) + '</span>'
      )).join("") : '<span class="workflow-selected-empty">未选择</span>') +
      '</div></div>';
  }

  function workflowStepMeta(step) {
    const title = String(step && step.title || "");
    const type = title.includes("主图") ? "主图 1:1" : title.includes("详情图") ? "详情图 3:4" : "图片";
    return type + " · 生成 1 张";
  }

  function syncWorkflowMode() {
    const id = currentWorkflowId();
    const def = workflowDef(id);
    renderWorkflowFields(def || { id: "single" });
    renderWorkflowTemplates(def || { id: "single" });
    renderWorkflowRunOptions(def || { id: "single" });
    const promptGroup = promptTextGroup();
    const promptTextarea = $("#genForm") && $("#genForm").elements && $("#genForm").elements.prompt;
    if (promptGroup) {
      promptGroup.hidden = String(id || "") !== "single";
    }
    if (promptTextarea) {
      promptTextarea.required = String(id || "") === "single";
      promptTextarea.disabled = String(id || "") !== "single";
    }
    const promptLabel = promptFieldLabel();
    if (promptLabel) {
      promptLabel.textContent = String(id || "") === "single" ? "提示词" : "产品参考图片";
    }
    const uploadLabel = referenceUploadLabel();
    if (uploadLabel) {
      uploadLabel.textContent = String(id || "") === "single" ? "参考图" : "产品参考图";
    }
    const controls = singleGenerationControls();
    if (controls) {
      controls.hidden = String(id || "") !== "single";
    }
    const review = workflowReview();
    if (review) {
      review.hidden = true;
      review.innerHTML = "";
    }
    state.pendingReview = null;
    const submit = $("#genForm") && $("#genForm").querySelector('button[type="submit"]');
    if (submit) {
      submit.innerHTML = iconHTML(String(id || "") === "single" ? "image-plus" : "workflow") +
        '<span>' + (String(id || "") === "single" ? "生成图片" : "运行工作流") + '</span>';
    }
    renderWorkflowTabs();
  }

  function persistCurrentValues(options = {}) {
    const id = currentWorkflowId();
    if (String(id || "") === "single") {
      return;
    }
    workflowNormalizeResolutionInput(workflowFields() && workflowFields().querySelector('[data-workflow-key="image_resolution"]'), { notify: false });
    state.cache[id] = currentWorkflowValues();
    if (!options.keepActiveId) {
      state.cache.activeWorkflowId = id;
    }
    saveWorkflowCache();
  }

  function setWorkflow(id, values) {
    const input = workflowIdInput();
    if (!input) {
      return;
    }
    input.value = String(id || "single");
    if (String(input.value || "") !== "single" && values && typeof values === "object") {
      state.cache[input.value] = Object.assign({}, state.cache[input.value] || {}, values);
      saveWorkflowCache();
    }
    const def = workflowDef(input.value);
    renderWorkflowTabs();
    renderWorkflowFields(def || { id: "single" });
    const container = workflowFields();
    if (container && values) {
      container.querySelectorAll("[data-workflow-key]").forEach((input) => {
        const key = input.dataset.workflowKey;
        if (values[key] != null) {
          input.value = values[key];
        }
      });
      syncWorkflowResolutionState(container);
      workflowNormalizeResolutionInput(container.querySelector('[data-workflow-key="image_resolution"]'), { notify: false });
    }
    const templateValues = String(values && (values.templates || values.workflow_templates) || "").trim();
    if (templateValues) {
      setWorkflowTemplateIDs(templateValues.split(","));
    } else {
      setWorkflowTemplateIDs(currentWorkflowTemplateIDs());
    }
    syncWorkflowMode();
  }

  function renderWorkflowSteps(result) {
    const container = workflowSteps();
    if (!container) {
      return;
    }
    const steps = Array.isArray(result && result.steps) ? result.steps : [];
    if (steps.length === 0) {
      container.hidden = true;
      container.innerHTML = "";
      return;
    }
    container.hidden = false;
    const total = Math.max(Number(result && result.progress_total) || steps.length, steps.length, 1);
    const completed = steps.filter((step) => step.status === "done").length;
    const activeIndex = steps.findIndex((step) => step.status === "running");
    const current = Math.min(total, Math.max(completed + (activeIndex >= 0 ? 1 : 0), steps.length ? 1 : 0));
    const percent = Math.max(0, Math.min(100, Math.round((current / total) * 100)));
    const def = workflowDef(currentWorkflowId());
    const progressHTML = '<div class="workflow-progress">' +
      '<div class="workflow-progress-head"><strong>工作流进度</strong><span>' + esc(current) + ' / ' + esc(total) + '</span></div>' +
      workflowProgressMetaHTML(def, result) +
      '<div class="workflow-progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="' + esc(total) +
      '" aria-valuenow="' + esc(current) + '"><span style="width:' + esc(percent) + '%"></span></div>' +
      '</div>';
    container.innerHTML = progressHTML + steps.map((step) => {
      const images = Array.isArray(step.images) ? step.images : [];
      const text = String(step.text || "").trim();
      const kind = String(step.kind || "").toLowerCase();
      const showText = !(result && result.needs_review && kind === "response");
      const status = String(step.status || "done");
      const duration = stepDurationMs(step);
      const canRerun = status === "done" && String(step.id || "").trim() && String(step.kind || "").toLowerCase() === "image";
      const imageBlocks = images.map((url) => (
        '<button class="workflow-step-image" type="button" data-preview-image="' + esc(url) + '">' +
        '<img src="' + esc(url) + '" alt="' + esc(step.title || "workflow image") + '">' +
        "</button>"
      ));
      if (workflowStepNeedsImagePlaceholder(step)) {
        imageBlocks.push(
          '<div class="workflow-step-image workflow-step-image-placeholder" aria-hidden="true">' +
          '<span class="spinner"></span>' +
          '<span>等待图片输出...</span>' +
          '</div>'
        );
      }
      return '<section class="workflow-step ' + esc(status) + '">' +
        '<div class="workflow-step-head">' +
        '<div><strong>' + esc(step.title || step.id || "步骤") + "</strong>" +
        '<span class="workflow-step-sub">' + esc(step.kind || "") + '</span></div>' +
        '<div class="workflow-step-meta">' +
        (canRerun ? '<button class="btn small workflow-step-rerun" type="button" title="重跑此步骤" data-workflow-rerun-step="' +
          esc(step.id || "") + '">' + iconHTML("refresh-cw") + '<span>重跑</span></button>' : "") +
        '<span class="workflow-step-kind">' + esc(status === "running" ? "进行中" : status === "done" ? "完成" : status) + "</span>" +
        (duration == null ? "" : '<span class="workflow-step-duration">' + esc(formatWorkflowDuration(duration)) + '</span>') +
        '</div>' +
        "</div>" +
        (showText && text ? '<pre class="workflow-step-text">' + esc(text) + "</pre>" : "") +
        (imageBlocks.length ? '<div class="workflow-step-images">' + imageBlocks.join("") + "</div>" : "") +
        "</section>";
    }).join("");
  }

  function workflowStepNeedsImagePlaceholder(step) {
    return String(step && step.kind || "").toLowerCase() === "image" &&
      String(step && step.status || "done") === "running" &&
      !(Array.isArray(step && step.images) && step.images.length > 0);
  }

  function currentWorkflowReviewTexts() {
    const review = workflowReview();
    const values = {};
    if (!review) {
      return values;
    }
    review.querySelectorAll("[data-workflow-review-step]").forEach((input) => {
      const key = String(input.dataset.workflowReviewStep || "").trim();
      if (key) {
        values[key] = String(input.value || "").trim();
      }
    });
    return values;
  }

  function renderWorkflowReview(result) {
    const container = workflowReview();
    if (!container) {
      return;
    }
    const steps = Array.isArray(result && result.steps) ? result.steps.filter((step) => String(step.kind || "").toLowerCase() === "response") : [];
    if (!result || !result.needs_review || steps.length === 0) {
      container.hidden = true;
      container.innerHTML = "";
      return;
    }
    container.hidden = false;
    container.innerHTML = (
      '<div class="workflow-review-head">' +
      '<strong>确认文案后继续</strong>' +
      '<span>可直接调整卖点、痛点、参数等内容，再开始后续生图。</span>' +
      '</div>' +
      steps.map((step) => {
        const value = String(step.text || "");
        return (
          '<label class="workflow-review-item">' +
          '<span>' + esc(step.title || step.id || "文案") + '</span>' +
          '<textarea data-workflow-review-step="' + esc(step.id || "") + '">' + esc(value) + '</textarea>' +
          '</label>'
        );
      }).join("") +
      '<div class="workflow-review-actions">' +
      '<button class="btn primary" type="button" data-workflow-review-action="continue">' +
      iconHTML("circle-check") + '<span>确认并继续</span></button>' +
      '</div>'
    );
  }

  async function loadCatalog() {
    try {
      const data = await api("/api/workflows");
      state.catalog = Array.isArray(data.workflows) ? data.workflows : [];
    } catch (error) {
      console.warn("workflow catalog load failed", error);
      state.catalog = [];
    }
    renderWorkflowTabs();
    const activeId = state.cache.activeWorkflowId || "single";
    const values = state.cache[activeId] || {};
    setWorkflow(activeId, values);
    state.cache.activeWorkflowId = activeId;
    saveWorkflowCache();
  }

  async function generateWorkflowStream(form, signal, options = {}) {
    const workflowId = currentWorkflowId();
    const def = workflowDef(workflowId);
    const result = { workflow_id: workflowId, workflow_name: workflowLabel(workflowId), prompt: "", params: {}, output_dir: "", steps: [], images: [], saved: [], text: "", timing: null, needs_review: false, progress_total: workflowProgressTotal(def, options.mode) };
    result.previousResult = state.lastResult || null;
    state.lastResult = result;
    const preserveResults = options.preserveResults === true;
    const startedAt = performance.now();
    let firstResponseAt = 0;
    let activeStep = null;
    const stepsById = new Map();
    const markFirstResponse = () => {
      if (!firstResponseAt) {
        firstResponseAt = performance.now();
      }
    };
    const completeStep = (step) => {
      if (!step) {
        return;
      }
      if (!step.started_at_ms) {
        step.started_at_ms = performance.now();
      }
      if (!step.ended_at_ms) {
        step.ended_at_ms = performance.now();
      }
      step.status = "done";
    };
    const finishActiveStep = () => {
      if (activeStep && activeStep.status === "running") {
        completeStep(activeStep);
      }
    };
    const finishStepById = (id) => {
      const key = String(id || "").trim();
      const step = key ? stepsById.get(key) : activeStep;
      if (step && step.status === "running") {
        completeStep(step);
      }
    };

    setResultsVisible(true);
    if (!preserveResults) {
      workflowSteps().hidden = false;
      workflowSteps().innerHTML = '<div class="generation-placeholder"><span class="spinner"></span><span>正在执行工作流...</span></div>';
    }
    state.runStartedAt = startedAt;
    if (state.progressTimer) {
      window.clearInterval(state.progressTimer);
    }
    if (!preserveResults) {
      state.progressTimer = window.setInterval(() => {
        if (result.steps.length > 0) {
          renderWorkflowSteps(result);
        }
      }, 1000);
      const review = workflowReview();
      if (review) {
        review.hidden = true;
        review.innerHTML = "";
      }
      renderGenerationPlaceholder("正在等待步骤输出...", startedAt);
      renderResultTiming(null);
    }

    const formData = await buildGenerateFormData(form);
    formData.set("workflow_id", workflowId);
    const templates = currentWorkflowTemplateIDs();
    if (templates.length > 0) {
      formData.set("workflow_templates", templates.join(","));
    }
    const mode = String(options.mode || "").trim();
    if (mode) {
      formData.set("workflow_mode", mode);
    }
    const stepTexts = options.stepTexts && typeof options.stepTexts === "object" ? options.stepTexts : null;
    if (stepTexts) {
      formData.set("workflow_step_texts", JSON.stringify(stepTexts));
    }
    const stepId = String(options.stepId || "").trim();
    if (stepId) {
      formData.set("workflow_step_id", stepId);
    }
    const outputDir = String(options.outputDir || "").trim();
    if (outputDir) {
      formData.set("workflow_output_dir", outputDir);
    }
    await apiStream("/api/workflow/run?stream=1", {
      method: "POST",
      body: formData,
      signal,
    }, {
      status(payload) {
        $("#genStatus").textContent = payload.message || "工作流运行中...";
      },
      step(payload) {
        markFirstResponse();
        const id = String(payload.id || "");
        const existing = id ? stepsById.get(id) : null;
        if (activeStep && activeStep !== existing && activeStep.status === "running" &&
          String(activeStep.kind || "").toLowerCase() === "response") {
          completeStep(activeStep);
        }
        activeStep = existing || {
          id,
          kind: payload.kind || "",
          title: payload.title || "",
          text: "",
          images: [],
          saved: [],
          status: "running",
          started_at_ms: performance.now(),
          ended_at_ms: null,
        };
        activeStep.kind = payload.kind || activeStep.kind || "";
        activeStep.title = payload.title || activeStep.title || "";
        activeStep.status = "running";
        if (!activeStep.started_at_ms) {
          activeStep.started_at_ms = performance.now();
        }
        if (id) {
          stepsById.set(id, activeStep);
        }
        if (!existing) {
          result.steps.push(activeStep);
        }
        if (!preserveResults) {
          renderWorkflowSteps(result);
        }
      },
      step_done(payload) {
        markFirstResponse();
        finishStepById(payload.id);
        if (!preserveResults) {
          renderWorkflowSteps(result);
        }
      },
      text(payload) {
        markFirstResponse();
        const text = String(payload.text || "");
        if (!text) {
          return;
        }
        if (activeStep) {
          activeStep.text += text;
        } else {
          result.text += text;
        }
        if (!preserveResults) {
          renderWorkflowSteps(result);
        }
      },
      image(payload) {
        markFirstResponse();
        const url = String(payload.url || "");
        if (!url) {
          return;
        }
        if (!result.images.includes(url)) {
          result.images.push(url);
        }
        const stepId = String(payload.step_id || "");
        const target = stepId && result.steps.find((item) => item.id === stepId) || activeStep;
        if (target) {
          if (!target.images.includes(url)) {
            target.images.push(url);
          }
          if (String(target.kind || "").toLowerCase() === "image") {
            completeStep(target);
          }
        }
        result.images = workflowOrderedImages(result);
        if (!preserveResults) {
          renderWorkflowSteps(result);
          renderGalleryImages(result.images);
        }
      },
      done(payload) {
        markFirstResponse();
        finishActiveStep();
        result.workflow_id = payload.workflow_id || result.workflow_id;
        result.workflow_name = payload.workflow_name || result.workflow_name;
        result.prompt = payload.prompt || "";
        result.params = Object.assign({}, currentWorkflowValues(), payload.params || {});
        result.output_dir = payload.output_dir || result.output_dir || "";
        if (Array.isArray(payload.steps)) {
          const previousById = new Map(result.steps.map((step) => [String(step.id || ""), step]));
          result.steps = payload.steps.map((step) => {
            const previous = previousById.get(String(step.id || ""));
            return Object.assign({}, step, {
              status: "done",
              started_at_ms: previous && previous.started_at_ms || step.started_at_ms || null,
              ended_at_ms: previous && previous.ended_at_ms || step.ended_at_ms || performance.now(),
            });
          });
        }
        if (Array.isArray(payload.images)) {
          result.images = payload.images;
        }
        result.images = workflowOrderedImages(result);
        result.saved = Array.isArray(payload.saved) ? payload.saved : result.saved;
        result.text = payload.text || result.text;
        result.needs_review = Boolean(payload.needs_review);
        result.progress_total = Math.max(result.progress_total || 0, result.steps.length);
        state.lastResult = result;
        if (!preserveResults) {
          renderWorkflowSteps(result);
          if (result.needs_review) {
            $("#gallery").classList.add("empty");
            $("#gallery").innerHTML = '<div class="status">文案已生成，请确认后继续</div>';
          } else {
            renderGalleryImages(result.images);
          }
          renderWorkflowReview(result);
        }
      },
      error(payload) {
        throw new Error(payload.error || "工作流运行失败");
      },
    });

    const finishedAt = performance.now();
    finishActiveStep();
    if (state.progressTimer) {
      window.clearInterval(state.progressTimer);
      state.progressTimer = 0;
    }
    const baseTiming = options.baseTiming && typeof options.baseTiming === "object" ? options.baseTiming : null;
    result.timing = {
      response_ms: Math.round((firstResponseAt || finishedAt) - startedAt),
      total_ms: Math.round(finishedAt - startedAt),
    };
    if (baseTiming && String(options.mode || "") === "final") {
      const responseMs = Number(baseTiming.response_ms);
      const totalMs = Number(baseTiming.total_ms);
      if (Number.isFinite(responseMs) && responseMs > 0) {
        result.timing.response_ms = Math.round(responseMs);
      }
      if (Number.isFinite(totalMs) && totalMs > 0) {
        result.timing.total_ms = Math.round(totalMs + result.timing.total_ms);
      }
    }
    if (!preserveResults) {
      renderWorkflowSteps(result);
      renderResultTiming(result.timing);
    }
    state.lastResult = result;
    return result;
  }

  async function runWorkflowSubmission(form, options = {}) {
    const submitButton = form.querySelector('button[type="submit"]');
    const cancelButton = $("#cancelGenerate");
    activeGenerationController = new AbortController();
    const isReviewContinue = String(options.mode || "") === "final" && options.stepTexts && Object.keys(options.stepTexts).length > 0;
    const runningLabel = isReviewContinue ? "继续生成" : "运行工作流";
    if (submitButton) {
      submitButton.disabled = true;
      submitButton.innerHTML = '<span class="spinner small"></span><span>' + runningLabel + '...</span>';
    }
    cancelButton.hidden = false;
    $("#genStatus").textContent = runningLabel + "...";
    $("#genStatus").className = "status gen-status loading";
    try {
      if (isReviewContinue && state.pendingReview && state.pendingReview.timing) {
        options = Object.assign({}, options, { baseTiming: state.pendingReview.timing });
      }
      if (isReviewContinue && state.pendingReview && state.pendingReview.output_dir) {
        options = Object.assign({}, options, { outputDir: state.pendingReview.output_dir });
      }
      const result = await generateWorkflowStream(form, activeGenerationController.signal, options);
      if (result.needs_review) {
        state.pendingReview = result;
        state.lastResult = result;
        $("#genStatus").textContent = "文案已生成，请确认后继续";
        $("#genStatus").className = "status gen-status";
        return result;
      }
      state.pendingReview = null;
      state.lastResult = result;
      const historyError = await saveHistorySafely(result);
      $("#genStatus").textContent = "已保存" + result.saved.length + " 张" +
        (historyError ? "，历史保存失败：" + historyError : "");
      $("#genStatus").className = "status gen-status";
      renderWorkflowSteps(result);
      if (state.cache.playDoneSound !== false && typeof Audio !== "undefined") {
        try {
          const audio = new Audio("done.mp3");
          audio.play().catch(() => { });
        } catch {
          // Ignore sound playback failures.
        }
      }
      loadBalance();
      return result;
    } catch (error) {
      if (state.progressTimer) {
        window.clearInterval(state.progressTimer);
        state.progressTimer = 0;
      }
      const canceled = error && error.name === "AbortError";
      const steps = workflowSteps();
      if (steps && steps.querySelector(".generation-placeholder")) {
        steps.hidden = false;
        steps.innerHTML = '<div class="' + (canceled ? "status" : "status error") + '">' +
          esc(canceled ? "已取消工作流" : "工作流失败：" + error.message) + '</div>';
      }
      if ($("#gallery").querySelector(".generation-placeholder")) {
        renderGalleryMessage(canceled ? "已取消工作流" : "工作流失败：" + error.message, canceled ? "status" : "status error");
      }
      if (canceled) {
        $("#genStatus").textContent = "已取消工作流";
        $("#genStatus").className = "status gen-status";
        return null;
      }
      const lastResult = state.lastResult && (Array.isArray(state.lastResult.images) || Array.isArray(state.lastResult.saved))
        ? state.lastResult
        : null;
      const hasPartialOutput = Boolean(lastResult && ((lastResult.images || []).length > 0 || (lastResult.saved || []).length > 0));
      await saveHistorySafely(lastResult || { images: [] }, {
        status: hasPartialOutput ? "partial" : "failed",
        error: error.message,
      });
      if (!hasPartialOutput && state.lastResult && state.lastResult.previousResult) {
        state.lastResult = state.lastResult.previousResult;
      }
      handleOperationError(error, $("#genStatus"));
      return null;
    } finally {
      if (state.progressTimer) {
        window.clearInterval(state.progressTimer);
        state.progressTimer = 0;
      }
      activeGenerationController = null;
      cancelButton.hidden = true;
      if (submitButton) {
        submitButton.disabled = false;
        submitButton.innerHTML = iconHTML(String(currentWorkflowId() || "") === "single" ? "image-plus" : "workflow") +
          '<span>' + (String(currentWorkflowId() || "") === "single" ? "生成图片" : "运行工作流") + '</span>';
      }
    }
  }

  async function continueWorkflowReview() {
    const form = $("#genForm");
    if (!form || !state.pendingReview) {
      return;
    }
    updateSize();
    syncImageInput();
    saveSettings();
    persistCurrentValues();
    const stepTexts = currentWorkflowReviewTexts();
    await runWorkflowSubmission(form, {
      mode: "final",
      stepTexts,
    });
  }

  function workflowStepTextsFromResult(result, excludeStepId = "") {
    const values = {};
    const excluded = String(excludeStepId || "").trim();
    const steps = Array.isArray(result && result.steps) ? result.steps : [];
    steps.forEach((step) => {
      const id = String(step.id || "").trim();
      const text = String(step.text || "").trim();
      if (id && id !== excluded && text) {
        values[id] = text;
      }
    });
    Object.assign(values, currentWorkflowReviewTexts());
    if (excluded) {
      delete values[excluded];
    }
    return values;
  }

  async function rerunWorkflowStep(stepId) {
    stepId = String(stepId || "").trim();
    const form = $("#genForm");
    const previous = state.lastResult;
    if (!form || !previous || !stepId) {
      return;
    }
    let rerunApplied = false;
    const originalSteps = Array.isArray(previous.steps) ? previous.steps : [];
    const stepIndex = originalSteps.findIndex((step) => String(step.id || "") === stepId);
    if (stepIndex < 0) {
      return;
    }
    updateSize();
    syncImageInput();
    saveSettings();
    persistCurrentValues();
    const button = workflowSteps() && workflowSteps().querySelector('[data-workflow-rerun-step="' + CSS.escape(stepId) + '"]');
    if (button) {
      button.disabled = true;
      button.innerHTML = '<span class="spinner small"></span><span>重跑...</span>';
    }
    $("#genStatus").textContent = "正在重跑步骤...";
    $("#genStatus").className = "status gen-status loading";
    activeGenerationController = new AbortController();
    try {
      const rerun = await generateWorkflowStream(form, activeGenerationController.signal, {
        mode: "final",
        stepId,
        outputDir: previous.output_dir || "",
        stepTexts: workflowStepTextsFromResult(previous, stepId),
        preserveResults: true,
      });
      if (!rerun || !Array.isArray(rerun.steps) || rerun.steps.length === 0) {
        return;
      }
      const nextSteps = originalSteps.slice();
      const oldStep = originalSteps[stepIndex] || {};
      const newStep = rerun.steps[0] || {};
      const isImageStep = String(oldStep.kind || newStep.kind || "").toLowerCase() === "image";
      const mergedStep = Object.assign({}, oldStep, newStep, { status: "done" });
      if (isImageStep) {
        mergedStep.images = Array.from(new Set([].concat(oldStep.images || [], newStep.images || [])));
        mergedStep.saved = Array.from(new Set([].concat(oldStep.saved || [], newStep.saved || [])));
      }
      nextSteps[stepIndex] = mergedStep;
      const merged = Object.assign({}, previous, {
        steps: nextSteps,
        images: workflowOrderedImages({
          steps: nextSteps,
          images: nextSteps.flatMap((step) => Array.isArray(step.images) ? step.images : []),
        }),
        saved: nextSteps.flatMap((step) => Array.isArray(step.saved) ? step.saved : []),
        text: nextSteps.map((step) => String(step.text || "").trim()).filter(Boolean).join("\n\n"),
        output_dir: rerun.output_dir || previous.output_dir || "",
        needs_review: false,
        timing: rerun.timing || previous.timing,
      });
      state.lastResult = merged;
      state.pendingReview = null;
      renderWorkflowSteps(merged);
      renderGalleryImages(merged.images);
      renderWorkflowReview(merged);
      renderResultTiming(merged.timing);
      const historyError = await saveHistorySafely(merged);
      $("#genStatus").textContent = "步骤已重跑，已保存" + merged.saved.length + " 张" +
        (historyError ? "，历史保存失败：" + historyError : "");
      $("#genStatus").className = "status gen-status";
      loadBalance();
      rerunApplied = true;
    } catch (error) {
      handleOperationError(error, $("#genStatus"));
    } finally {
      activeGenerationController = null;
      const cancelButton = $("#cancelGenerate");
      if (cancelButton) {
        cancelButton.hidden = true;
      }
      if (!rerunApplied) {
        state.lastResult = previous;
      }
      renderWorkflowSteps(rerunApplied ? (state.lastResult || previous) : previous);
    }
  }

  function restoreWorkflow(workflowId, values) {
    setWorkflow(workflowId || "single", values || {});
  }

  function init() {
    const tabs = workflowTabs();
    if (!tabs) {
      return;
    }
    tabs.addEventListener("click", (event) => {
      const button = event.target.closest("[data-workflow-id]");
      if (!button) {
        return;
      }
      const id = String(button.dataset.workflowId || "single");
      if (id === "agent") {
        return;
      }
      if (id === "workflow-custom") {
        openWorkflowContactModal();
        return;
      }
      if (id === currentWorkflowId()) {
        return;
      }
      persistCurrentValues({ keepActiveId: true });
      setWorkflow(id, state.cache[id] || {});
      state.cache.activeWorkflowId = id;
      saveWorkflowCache();
    });
    const fields = workflowFields();
    if (fields) {
      fields.addEventListener("input", persistCurrentValues);
      fields.addEventListener("change", (event) => {
        const input = event.target.closest("[data-workflow-key]");
        if (input && !workflowNormalizeResolutionInput(input, { notify: true })) {
          updateWorkflowSectionSummaries();
          persistCurrentValues();
          return;
        }
        persistCurrentValues();
      });
      fields.addEventListener("input", updateWorkflowSectionSummaries);
      fields.addEventListener("change", updateWorkflowSectionSummaries);
      fields.addEventListener("click", (event) => {
        const toggle = event.target.closest("[data-workflow-combo-toggle]");
        if (toggle) {
          const combo = toggle.closest("[data-workflow-combo]");
          const menu = combo && combo.querySelector("[data-workflow-combo-menu]");
          if (menu) {
            menu.hidden = !menu.hidden;
          }
          return;
        }
        const option = event.target.closest("[data-workflow-combo-option]");
        if (!option) {
          return;
        }
        const combo = option.closest("[data-workflow-combo]");
        const input = combo && combo.querySelector("[data-workflow-key]");
        const menu = combo && combo.querySelector("[data-workflow-combo-menu]");
        if (input) {
          input.value = option.dataset.workflowComboOption || "";
          input.dispatchEvent(new Event("input", { bubbles: true }));
          input.dispatchEvent(new Event("change", { bubbles: true }));
          input.focus();
        }
        if (menu) {
          menu.hidden = true;
        }
      });
      fields.addEventListener("input", (event) => {
        const input = event.target.closest("[data-workflow-combo] [data-workflow-key]");
        if (!input) {
          return;
        }
        const combo = input.closest("[data-workflow-combo]");
        const menu = combo && combo.querySelector("[data-workflow-combo-menu]");
        if (!menu) {
          return;
        }
        const needle = String(input.value || "").trim().toLowerCase();
        menu.hidden = false;
        /*
        menu.querySelectorAll("[data-workflow-combo-option]").forEach((option) => {
          option.hidden = Boolean(needle) && !String(option.textContent || "").toLowerCase().includes(needle) &&
            !String(option.dataset.workflowComboOption || "").toLowerCase().includes(needle);
        });
        */
      });
    }
    document.addEventListener("click", (event) => {
      if (event.target.closest("[data-workflow-combo]")) {
        return;
      }
      document.querySelectorAll("[data-workflow-combo-menu]").forEach((menu) => {
        menu.hidden = true;
      });
    });
    document.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") {
        return;
      }
      const modal = workflowTemplatePicker() && workflowTemplatePicker().querySelector("[data-workflow-template-modal]");
      if (modal && !modal.hidden) {
        modal.hidden = true;
        return;
      }
      const contactModal = $("#workflowContactModal");
      if (contactModal && !contactModal.hidden) {
        closeWorkflowContactModal();
      }
    });
    const templatePicker = workflowTemplatePicker();
    if (templatePicker) {
      templatePicker.addEventListener("click", (event) => {
        const open = event.target.closest("[data-workflow-template-open]");
        if (open) {
          const modal = templatePicker.querySelector("[data-workflow-template-modal]");
          if (modal) {
            modal.hidden = false;
          }
          return;
        }
        const close = event.target.closest("[data-workflow-template-close]");
        const backdrop = event.target.matches("[data-workflow-template-modal]");
        if (close || backdrop) {
          const modal = templatePicker.querySelector("[data-workflow-template-modal]");
          if (modal) {
            modal.hidden = true;
          }
        }
      });
      templatePicker.addEventListener("change", () => {
        const selected = Array.from(templatePicker.querySelectorAll("[data-workflow-template]"))
          .filter((input) => input.checked)
          .map((input) => String(input.dataset.workflowTemplate || "").trim())
          .filter(Boolean);
        setWorkflowTemplateIDs(selected.length > 0 ? selected : workflowDefaultTemplateIDs(workflowDef(currentWorkflowId())));
        persistCurrentValues();
      });
    }
    const runOptions = workflowRunOptions();
    if (runOptions) {
      runOptions.addEventListener("change", (event) => {
        const autoRun = event.target.closest("#workflowAutoRun");
        if (autoRun) {
          state.cache.skipReview = Boolean(autoRun.checked);
          saveWorkflowCache();
          return;
        }
        const playSound = event.target.closest("#workflowPlayDoneSound");
        if (!playSound) {
          return;
        }
        state.cache.playDoneSound = Boolean(playSound.checked);
        saveWorkflowCache();
      });
      runOptions.addEventListener("click", (event) => {
        const button = event.target.closest("[data-workflow-upgrade]");
        if (!button) {
          return;
        }
        event.preventDefault();
        event.stopImmediatePropagation();
        if (window.KuaimaBilling && typeof window.KuaimaBilling.openPlans === "function") {
          window.setTimeout(() => window.KuaimaBilling.openPlans(), 0);
        }
      });
    }
    window.addEventListener("kuaima:account", (event) => {
      const group = event && event.detail && event.detail.group;
      state.accountGroup = String(group || "default").toLowerCase();
      state.maxConcurrency = Math.max(1, Number(event && event.detail && event.detail.maxConcurrency) || 1);
      renderWorkflowRunOptions(workflowDef(currentWorkflowId()) || { id: "single" });
    });
    const review = workflowReview();
    if (review) {
      review.addEventListener("click", async (event) => {
        const button = event.target.closest("[data-workflow-review-action]");
        if (!button) {
          return;
        }
        if (button.dataset.workflowReviewAction === "continue") {
          event.preventDefault();
          await continueWorkflowReview();
        }
      });
    }
    const steps = workflowSteps();
    if (steps) {
      steps.addEventListener("click", async (event) => {
        const upgrade = event.target.closest("[data-workflow-upgrade]");
        if (upgrade) {
          event.preventDefault();
          event.stopImmediatePropagation();
          if (window.KuaimaBilling && typeof window.KuaimaBilling.openPlans === "function") {
            window.setTimeout(() => window.KuaimaBilling.openPlans(), 0);
          }
          return;
        }
        const openOutputDir = event.target.closest("[data-workflow-open-output-dir]");
        if (openOutputDir) {
          event.preventDefault();
          event.stopImmediatePropagation();
          const dir = String(openOutputDir.dataset.workflowOpenOutputDir || "").trim();
          if (dir) {
            await openWorkflowOutputDir(dir);
          }
          return;
        }
        const button = event.target.closest("[data-workflow-rerun-step]");
        if (!button) {
          return;
        }
        event.preventDefault();
        await rerunWorkflowStep(button.dataset.workflowRerunStep || "");
      });
    }
    const contactModal = $("#workflowContactModal");
    if (contactModal) {
      contactModal.addEventListener("click", (event) => {
        if (event.target.closest("#closeWorkflowContactModal") || event.target === contactModal) {
          closeWorkflowContactModal();
        }
      });
    }
    const workflowResolution = $("#resolutionSelect");
    if (workflowResolution) {
      workflowResolution.addEventListener("change", (event) => {
        const select = event.target;
        if (!resolutionCanUse4k(workflowAccountGroup()) && String(select.value || "").toLowerCase() === "4k") {
          select.value = select.dataset.previousResolution || "2k";
          alert(resolutionUpgradeMessage());
          if (window.KuaimaBilling && typeof window.KuaimaBilling.openPlans === "function") {
            window.setTimeout(() => window.KuaimaBilling.openPlans(), 0);
          }
          updateSize();
          return;
        }
        select.dataset.previousResolution = select.value;
        updateSize();
      });
      workflowResolution.dataset.previousResolution = workflowResolution.value;
    }
    const form = $("#genForm");
    if (form) {
      form.addEventListener("submit", async (event) => {
        if (currentWorkflowId() === "single") {
          return;
        }
        event.preventDefault();
        event.stopImmediatePropagation();
        setWorkflowTemplateIDs(currentWorkflowTemplateIDs());
        updateSize();
        syncImageInput();
        saveSettings();
        persistCurrentValues();
        const def = workflowDef(currentWorkflowId());
        const mode = workflowNeedsReview(def) && !workflowAutoRunEnabled() ? "draft" : "final";
        await runWorkflowSubmission(form, { mode });
      }, true);
    }
    loadCatalog();
  }

  window.KuaimaWorkflow = {
    init,
    currentValues: currentWorkflowValues,
    currentWorkflowId,
    currentWorkflowLabel: workflowLabel,
    generateWorkflowStream,
    restore: restoreWorkflow,
    sync: syncWorkflowMode,
    renderSteps: renderWorkflowSteps,
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init, { once: true });
  } else {
    init();
  }
})();
