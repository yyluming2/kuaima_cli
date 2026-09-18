(() => {
  "use strict";

  const h = React.createElement;
  const names = { text: "文本", shape: "形状", table: "表格", chart: "图表", image: "图片" };
  const defaults = { fontSize: 24, fontFamily: "Microsoft YaHei", bold: false, italic: false, underline: false, color: "#111827", background: "transparent", lineHeight: 1.2, letterSpacing: 0 };
  const shapeClip = shape => ({
    ellipse: "ellipse(50% 50% at 50% 50%)", oval: "ellipse(50% 50% at 50% 50%)",
    triangle: "polygon(50% 0%,100% 100%,0% 100%)", diamond: "polygon(50% 0%,100% 50%,50% 100%,0% 50%)",
    hexagon: "polygon(25% 0%,75% 0%,100% 50%,75% 100%,25% 100%,0% 50%)",
    pentagon: "polygon(50% 0%,100% 38%,82% 100%,18% 100%,0% 38%)",
    parallelogram: "polygon(20% 0%,100% 0%,80% 100%,0% 100%)",
    righttriangle: "polygon(0 0,100% 100%,0 100%)", rttriangle: "polygon(0 0,100% 100%,0 100%)"
  }[String(shape || "").toLowerCase()]);

  const boxStyle = (style = {}) => ({
    border: style.border ? (typeof style.border === "string" ? style.border : `${style.border.width || 1}px solid ${style.border.color || "#64748b"}`) : undefined,
    boxShadow: style.shadow || undefined
  });
  const fillStyle = value => !value ? {} : (String(value).includes("gradient") ? { backgroundImage: value, backgroundColor: "transparent" } : { backgroundColor: value });
  const shapeStyle = (shape, style = {}) => {
    const key = String(shape || "").toLowerCase();
    if (key === "donut") return { maskImage: "radial-gradient(circle, transparent 0 38%, #000 40%)", WebkitMaskImage: "radial-gradient(circle, transparent 0 38%, #000 40%)" };
    if (key === "roundrect") return { borderRadius: `${Math.max(0, Math.min(50, Number(style.radius) || 12))}%` };
    return { clipPath: shapeClip(shape) };
  };
  const visualStyle = (element, style = {}) => {
    const out = {}, fill = element?.fill, line = element?.line, text = element?.textStyle, geometry = element?.geometry;
    if (fill && style.background == null) {
      if (fill.type === "solid" && fill.color) out.background = fill.color.rgb || fill.color.css || "transparent";
      else if (fill.type === "gradient" && fill.gradient?.stops) out.background = `linear-gradient(${Number(fill.gradient.angle) || 0}deg,${fill.gradient.stops.map(x => `${x.color.rgb || x.color.css || "#000"} ${Number(x.position) || 0}%`).join(",")})`;
      else if (fill.type === "none") out.background = "transparent";
    }
    if (line && style.border == null && line.color && line.color.type !== "none") out.border = `${Number(line.width) || 1}px ${line.dash && /dot/i.test(line.dash) ? "dotted" : line.dash && line.dash !== "solid" ? "dashed" : "solid"} ${line.color.rgb || line.color.css || "#64748b"}`;
    if (text) {
      if (style.fontFamily == null && text.fontFamily) out.fontFamily = text.fontFamily;
      if (style.fontSize == null && text.fontSize) out.fontSize = text.fontSize;
      if (style.bold == null && text.bold != null) out.bold = text.bold;
      if (style.italic == null && text.italic != null) out.italic = text.italic;
      if (style.underline == null && text.underline != null) out.underline = text.underline;
      if (style.lineHeight == null && text.lineHeight) out.lineHeight = text.lineHeight;
      if (style.align == null && text.align) out.align = text.align;
    }
    if (geometry?.type && style.shape == null) out.shape = geometry.preset || geometry.type;
    if (element?.effects) {
      if (style.shadow == null) out.shadow = element.effects.shadow || undefined;
      if (style.boxShadow == null && element.effects.innerShadow) out.boxShadow = element.effects.innerShadow;
    }
    return out;
  };
  const canonicalStyle = element => ({ ...defaults, ...(element?.style || {}), ...visualStyle(element, element?.style || {}) });
  const normalizeDeck = value => {
    const next = structuredClone(value);
    (next.slides || []).forEach((slide, index) => {
      if (!slide.id) slide.id = "slide-" + (index + 1);
      (slide.elements || []).forEach(element => { element.style = canonicalStyle(element); });
    });
    if (next.master?.elements) next.master.elements = next.master.elements.map((element, index) => ({
      ...element,
      id: String(element.id || `master-${index + 1}`).startsWith("master-") ? (element.id || `master-${index + 1}`) : `master-${element.id}`,
      locked: false,
      style: canonicalStyle(element)
    }));
    return next;
  };
  const textAlign = value => ({ l: "left", ctr: "center", r: "right", just: "justify", dist: "justify", thaiDist: "justify" }[String(value || "").toLowerCase()] || value || undefined);
  const textRuns = (element, base, scale = 1) => {
    const runs = Array.isArray(element?.textRuns) ? element.textRuns : [];
    if (!runs.length) return element?.content || "";
    return runs.flatMap((run, index) => {
      const style = { ...base, ...(run?.style || {}) };
      const lead = index > 0 && Number(run?.paragraph) !== Number(runs[index - 1]?.paragraph) ? h("br", { key: "br" + index }) : null;
      return [lead, h("span", { key: index, style: { fontFamily: style.fontFamily, fontSize: Number(style.fontSize) ? Number(style.fontSize) * scale : undefined, color: style.color, fontWeight: style.bold ? "bold" : "normal", fontStyle: style.italic ? "italic" : "normal", textDecoration: style.underline ? "underline" : "none" } }, run?.content || "")];
    });
  };
  const rewriteRunContent = (element, content) => {
    const runs = Array.isArray(element.textRuns) ? element.textRuns : [];
    if (!runs.length) { element.content = content; return; }
    let cursor = 0;
    element.textRuns = runs.map((run, index) => {
      const remainingRuns = runs.length - index - 1;
      const take = index === runs.length - 1 ? content.length - cursor : Math.min(String(run.content || "").length, Math.max(0, content.length - cursor - remainingRuns));
      const part = content.slice(cursor, cursor + take);
      cursor += take;
      return { ...run, content: part };
    });
    element.content = content;
  };
  const customPath = element => element?.geometry?.type === "custom" && element.geometry.path ? h("svg", { className: "shape-svg", viewBox: element.geometry.viewBox || "0 0 1000 1000", preserveAspectRatio: "none", style: { position: "absolute", inset: 0, width: "100%", height: "100%", pointerEvents: "none" } }, h("path", { d: element.geometry.path, fill: element.fill?.color ? (element.fill.color.rgb || element.fill.color.css || "currentColor") : "currentColor", stroke: element.line?.color?.type !== "none" ? (element.line?.color?.rgb || element.line?.color?.css || "none") : "none", strokeWidth: element.line?.width ? Number(element.line.width) : 0 })) : null;
  const imageCropStyle = (style = {}) => {
    const crop = style.crop || {}, left = Math.max(0, Math.min(.99, Number(crop.left) || 0)), top = Math.max(0, Math.min(.99, Number(crop.top) || 0)), right = Math.max(0, Math.min(.99 - left, Number(crop.right) || 0)), bottom = Math.max(0, Math.min(.99 - top, Number(crop.bottom) || 0)), width = 100 / (1 - left - right), height = 100 / (1 - top - bottom);
    return { position: "absolute", maxWidth: "none", width: width + "%", height: height + "%", left: (-left * width) + "%", top: (-top * height) + "%", objectFit: "fill" };
  };
  const ImageBlock = ({ src, style, alt = "" }) => h("div", { className: "image-frame" }, h("img", { src, alt, style: imageCropStyle(style) }));
  const elementTransform = element => element.rotation ? `rotate(${Number(element.rotation) || 0}deg)` : undefined;
  const api = async (url, options) => { const response = await fetch(url, options); if (!response.ok) throw Error("请求失败（" + response.status + "）"); return response; };
  const readSSE = async response => {
    const reader = response.body.getReader(), decoder = new TextDecoder(); let pending = "", done = null;
    while (true) {
      const part = await reader.read(); pending += decoder.decode(part.value || new Uint8Array(), { stream: !part.done });
      const chunks = pending.split("\n\n"); pending = chunks.pop() || "";
      for (const block of chunks) {
        const line = block.split("\n").find(value => value.startsWith("data:")); if (!line) continue;
        let value; try { value = JSON.parse(line.slice(5).trim()); } catch (_) { continue; }
        if (block.includes("event: error")) throw new Error(value.error || "流式生成失败");
        if (block.includes("event: done")) done = value;
      }
      if (part.done) break;
    }
    return done;
  };
  const icon = name => h("i", { "data-lucide": name });

  window.PPTEditor = { ...(window.PPTEditor || {}), names, defaults, boxStyle, fillStyle, shapeStyle, visualStyle, normalizeDeck, textAlign, textRuns, rewriteRunContent, customPath, ImageBlock, elementTransform, api, readSSE, icon };
})();
