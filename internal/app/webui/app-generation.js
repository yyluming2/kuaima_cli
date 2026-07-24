async function generateStream(form, signal) {
  const images = [];
  const result = { images, saved: [], text: "", timing: null };
  const startedAt = performance.now();
  let firstResponseAt = 0;
  const markFirstResponse = () => {
    if (!firstResponseAt) {
      firstResponseAt = performance.now();
    }
  };
  [$("#workflowSteps"), $("#workflowReview")].forEach((container) => {
    if (container) {
      container.hidden = true;
      container.innerHTML = "";
    }
  });
  renderGenerationPlaceholder("正在等待图片...", startedAt, Number(form.elements.image_count && form.elements.image_count.value) || 1);
  renderResultTiming(null);
  if (selectedImages.length > 0 && !referenceCompressionDisabled(form)) {
    $("#genStatus").textContent = "正在压缩参考图...";
  }
  const formData = await buildGenerateFormData(form);
  if (window.KuaimaMaskEditor) {
    await window.KuaimaMaskEditor.appendMask(formData);
  }
  await apiStream("/api/generate/stream", {
    method: "POST",
    body: formData,
    signal,
  }, {
    status(payload) {
      $("#genStatus").textContent = payload.message || "生成中...";
    },
    text(payload) {
      if (payload.text) {
        markFirstResponse();
        result.text += payload.text;
        $("#genStatus").textContent = result.text;
      }
    },
    image(payload) {
      if (payload.url && !images.includes(payload.url)) {
        markFirstResponse();
        images.push(payload.url);
        renderGalleryImages(images);
      }
    },
    done(payload) {
      markFirstResponse();
      result.saved = payload.saved || [];
      result.text = payload.text || result.text;
      (payload.images || []).forEach((url) => {
        if (!images.includes(url)) {
          images.push(url);
        }
      });
      renderGalleryImages(images);
    },
    error(payload) {
      throw new Error(payload.error || "生成失败");
    },
  });
  const finishedAt = performance.now();
  result.timing = {
    response_ms: Math.round((firstResponseAt || finishedAt) - startedAt),
    total_ms: Math.round(finishedAt - startedAt),
  };
  renderResultTiming(result.timing);
  return result;
}

async function addImageURLAsReference(url) {
  $("#genStatus").textContent = "正在添加历史图片...";
  $("#genStatus").className = "status gen-status";
  if (selectedImages.some((file) => historySourceURLs.get(file) === url)) {
    $("#genStatus").textContent = "历史图片已存在";
    $("#genStatus").className = "status gen-status";
    return;
  }
  await addReferenceFiles([await fileFromHistoryURL(url)], "添加历史图片");
}

async function initConfig() {
  const config = await api("/api/config");
  const versionLabel = $("#appVersion");
  if (versionLabel) {
    versionLabel.textContent = config.version ? "v" + config.version : "";
    versionLabel.hidden = !config.version;
  }
  if (config.group) {
    window.KuaimaAccount = Object.assign({}, window.KuaimaAccount || {}, {
      group: String(config.group || "default").toLowerCase(),
    });
  }
  for (const [key, value] of Object.entries(config)) {
    const input = document.querySelector('[name="' + key + '"]');
    if (input && value !== "" && value != null) {
      input.value = value;
    }
  }
  applySize(config.image_size);
  applySettings(readSettings());
  syncSingleGenerationControls();
  if (window.KuaimaMaskEditor) {
    window.KuaimaMaskEditor.init();
  }
}
