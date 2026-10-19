(function () {
  const presets = [{ id: "original", name: "原图", hint: "恢复原始图片" }, { id: "clear", name: "变清晰", hint: "增强细节" }, { id: "auto", name: "自动增强", hint: "亮度、对比度与锐化" }, { id: "vivid", name: "鲜艳", hint: "提升色彩" }, { id: "warm", name: "暖色", hint: "温暖氛围" }, { id: "mono", name: "黑白", hint: "经典质感" }];

  // A restrained 3x3 unsharp-style kernel works well for slides: it brings back
  // text and vector-like edges without the strong halos of a high-radius sharpen.
  function sharpen(ctx, width, height, amount, threshold) {
    const source = ctx.getImageData(0, 0, width, height), input = source.data;
    const output = new Uint8ClampedArray(input), weights = [0, -1, 0, -1, 5, -1, 0, -1, 0];
    for (let y = 1; y < height - 1; y++) {
      for (let x = 1; x < width - 1; x++) {
        const p = (y * width + x) * 4;
        for (let channel = 0; channel < 3; channel++) {
          let value = 0;
          for (let ky = -1; ky <= 1; ky++) for (let kx = -1; kx <= 1; kx++) {
            value += input[((y + ky) * width + x + kx) * 4 + channel] * weights[(ky + 1) * 3 + kx + 1];
          }
          // Do not amplify tiny variations in flat backgrounds; this matters
          // for already-super-resolved images where noise can look like texture.
          const delta = value - input[p + channel];
          output[p + channel] = Math.abs(delta) > threshold
            ? input[p + channel] + delta * amount
            : input[p + channel];
        }
      }
    }
    source.data.set(output);
    ctx.putImageData(source, 0, 0);
  }

  function autoEnhance(ctx, width, height) {
    const image = ctx.getImageData(0, 0, width, height), data = image.data;
    let sum = 0, sumSq = 0, count = width * height;
    for (let i = 0; i < data.length; i += 4) {
      const luma = (data[i] * 0.2126 + data[i + 1] * 0.7152 + data[i + 2] * 0.0722) / 255;
      sum += luma; sumSq += luma * luma;
    }
    const mean = sum / count, deviation = Math.sqrt(Math.max(0, sumSq / count - mean * mean));
    // Keep the correction deliberately modest so slide backgrounds stay natural.
    const brightness = Math.max(-0.12, Math.min(0.12, (0.52 - mean) * 0.55));
    const contrast = Math.max(0.92, Math.min(1.18, 0.18 / Math.max(deviation, 0.12)));
    for (let i = 0; i < data.length; i += 4) {
      for (let channel = 0; channel < 3; channel++) {
        const normalized = data[i + channel] / 255;
        data[i + channel] = Math.max(0, Math.min(255, ((normalized - 0.5) * contrast + 0.5 + brightness) * 255));
      }
    }
    ctx.putImageData(image, 0, 0);
    sharpen(ctx, width, height, 0.5, 4);
  }

  async function apply(url, id, options) {
    if (id === "original") return url;
    const image = new Image(); image.crossOrigin = "anonymous"; image.src = url;
    await new Promise((resolve, reject) => { image.onload = resolve; image.onerror = reject; });
    const canvas = document.createElement("canvas"), ctx = canvas.getContext("2d", { willReadFrequently: id === "clear" || id === "auto" });
    canvas.width = image.naturalWidth; canvas.height = image.naturalHeight;
    if (id === "clear" || id === "auto") {
      ctx.imageSmoothingEnabled = true;
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
      if (id === "auto") autoEnhance(ctx, canvas.width, canvas.height);
      else {
        const amount = options && Number.isFinite(options.amount) ? options.amount : 0.72;
        const threshold = options && Number.isFinite(options.threshold) ? options.threshold : 3;
        sharpen(ctx, canvas.width, canvas.height, amount, threshold);
      }
    } else {
      ctx.filter = ({ vivid: "contrast(1.12) saturate(1.28)", warm: "saturate(1.16) sepia(.16) hue-rotate(-8deg)", mono: "grayscale(1) contrast(1.08)" })[id] || "none";
      ctx.drawImage(image, 0, 0);
    }
    return canvas.toDataURL("image/png");
  }
  async function openPicker(source, onSelect) {
    if (!source) return;
    const base = source, overlay = document.createElement("div"); overlay.className = "ppt-effect-picker";
    overlay.innerHTML = '<div class="ppt-effect-dialog"><div class="ppt-effect-head"><b>滤镜</b><button type="button" data-close>×</button></div>' +
      '<div class="ppt-effect-controls" hidden><label>锐化强度 <output data-sharpness-value>72%</output><input data-sharpness type="range" min="0" max="120" value="72"></label><label>细节阈值 <output data-threshold-value>3</output><input data-threshold type="range" min="0" max="12" value="3"></label></div>' +
      '<div class="ppt-effect-grid">' + presets.map(p => '<button type="button" class="ppt-effect-card" data-effect="' + p.id + '"><span class="ppt-effect-thumb"><span class="ppt-effect-loading">生成预览…</span></span><strong>' + p.name + '</strong><small>' + p.hint + '</small></button>').join("") + '</div></div>';
    document.body.appendChild(overlay);
    const close = () => overlay.remove(); overlay.querySelector("[data-close]").onclick = close;
    overlay.onclick = e => { if (e.target === overlay) close(); };
    const controls = overlay.querySelector(".ppt-effect-controls"), sharpness = overlay.querySelector("[data-sharpness]"), threshold = overlay.querySelector("[data-threshold]");
    const sharpnessValue = overlay.querySelector("[data-sharpness-value]"), thresholdValue = overlay.querySelector("[data-threshold-value]");
    let clearSrc = "";
    const renderClear = async () => {
      sharpnessValue.value = sharpnessValue.textContent = sharpness.value + "%"; thresholdValue.value = thresholdValue.textContent = threshold.value;
      try { clearSrc = await apply(base, "clear", { amount: Number(sharpness.value) / 100, threshold: Number(threshold.value) }); const card = overlay.querySelector('[data-effect="clear"]'); card.querySelector(".ppt-effect-thumb").innerHTML = '<img src="' + clearSrc + '" alt="变清晰预览">'; card.onclick = () => { controls.hidden = false; onSelect(clearSrc, base); }; if (!controls.hidden) onSelect(clearSrc, base); } catch (_) {}
    };
    sharpness.addEventListener("input", renderClear); threshold.addEventListener("input", renderClear);
    await Promise.all(presets.filter(p => p.id !== "clear").map(async p => { try { const src = await apply(base, p.id), card = overlay.querySelector('[data-effect="' + p.id + '"]'); card.querySelector(".ppt-effect-thumb").innerHTML = '<img src="' + src + '" alt="' + p.name + '预览">'; card.onclick = () => { controls.hidden = true; onSelect(src, base); }; } catch (_) {} }));
    await renderClear();
  }
  window.KuaimaImageEffects = { presets, apply, openPicker };
})();
