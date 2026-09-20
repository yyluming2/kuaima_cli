(() => {
  "use strict";

  const h = React.createElement;
  const names = { text: "文本", shape: "形状", table: "表格", chart: "图表", image: "图片" };
  // Browser defaults are correct for omitted PPTX typography. In particular,
  // do not invent fontFamily or lineHeight fields during normalization.
  const defaults = { fontSize: 24, bold: false, italic: false, underline: false, color: "#111827", background: "transparent", letterSpacing: 0 };
  const shapeClip = shape => ({
    ellipse: "ellipse(50% 50% at 50% 50%)", oval: "ellipse(50% 50% at 50% 50%)",
    triangle: "polygon(50% 0%,100% 100%,0% 100%)", diamond: "polygon(50% 0%,100% 50%,50% 100%,0% 50%)",
    hexagon: "polygon(25% 0%,75% 0%,100% 50%,75% 100%,25% 100%,0% 50%)",
    pentagon: "polygon(50% 0%,100% 38%,82% 100%,18% 100%,0% 38%)",
    parallelogram: "polygon(20% 0%,100% 0%,80% 100%,0% 100%)",
    righttriangle: "polygon(0 0,100% 100%,0 100%)", rttriangle: "polygon(0 0,100% 100%,0 100%)"
  }[String(shape || "").toLowerCase()]);

  const boxStyle = (style = {}) => ({
    border: style.border && !(typeof style.border === "object" && (Number(style.border.width) <= 0 || String(style.border.style || "").toLowerCase() === "none")) ? (typeof style.border === "string" ? style.border : `${style.border.width || 0}px ${style.border.style || "solid"} ${style.border.color || "#64748b"}`) : undefined,
    boxShadow: style.shadow || undefined
  });
  const fillStyle = value => {
    if (!value) return {};
    const v = String(value);
    if (v.includes("gradient")) return { backgroundImage: v, backgroundColor: "transparent" };
    if (/^data:image\//i.test(v) || /^https?:\/\//i.test(v)) return { backgroundImage: `url("${v}")`, backgroundSize: "cover", backgroundPosition: "center", backgroundRepeat: "no-repeat", backgroundColor: "transparent" };
    return { backgroundColor: v };
  };
  const imageFill = value => (/^data:image\//i.test(String(value || "")) || /^https?:\/\//i.test(String(value || ""))) ? h("img", { src: value, alt: "", className: "fill-image", style: /^data:image\/svg\+xml/i.test(String(value || "")) ? { objectFit: "contain" } : undefined, draggable: false }) : null;
  const shapeStyle = (shape, style = {}, width, height) => {
    const key = String(shape || "").toLowerCase();
    if (["donut", "ring", "torus", "concentriccircles", "concentriccircle"].includes(key)) {
      const hole = Math.max(1, Math.min(95, Number(style.holeRadius ?? style.innerRadius) || 38));
      return { maskImage: `radial-gradient(ellipse at center, transparent 0 ${hole}%, #000 ${Math.min(100, hole + 1)}% 100%)`, WebkitMaskImage: `radial-gradient(ellipse at center, transparent 0 ${hole}%, #000 ${Math.min(100, hole + 1)}% 100%)` };
    }
    if (key === "roundrect") {
      const percent = Number(style.radius);
      const bounded = Math.max(0, Math.min(50, percent || 12));
      // DrawingML's adjustment is relative to the short side. CSS percent
      // radii are relative to each axis independently, so use px when the
      // element dimensions are available to preserve the PPTX geometry.
      if (Number(width) > 0 && Number(height) > 0) return { borderRadius: `${Math.min(width, height) * bounded / 100}px` };
      return { borderRadius: `${bounded}%` };
    }
    return { clipPath: shapeClip(shape) };
  };
  const visualStyle = (element, style = {}) => {
    const out = {}, fill = element?.fill, line = element?.line, text = element?.textStyle, geometry = element?.geometry;
    if (fill && style.background == null) {
      if (fill.type === "solid" && fill.color) out.background = fill.color.rgb || fill.color.css || "transparent";
      else if (fill.type === "gradient" && fill.gradient?.stops) out.background = `linear-gradient(${Number(fill.gradient.angle) || 0}deg,${fill.gradient.stops.map(x => `${x.color.rgb || x.color.css || "#000"} ${Number(x.position) || 0}%`).join(",")})`;
      else if (fill.type === "image" && fill.image?.assetUrl) out.background = fill.image.assetUrl;
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
      if (style.verticalAlign == null && text.verticalAlign) out.verticalAlign = text.verticalAlign;
    }
    if (geometry?.type && style.shape == null) out.shape = geometry.preset || geometry.type;
    if (element?.effects) {
      if (style.shadow == null) out.shadow = element.effects.shadow || undefined;
      if (style.boxShadow == null && element.effects.innerShadow) out.boxShadow = element.effects.innerShadow;
    }
    return out;
  };
  const canonicalStyle = element => ({ ...(element?.style || {}), ...visualStyle(element, element?.style || {}) });
  const registerFontAssets = assets => (assets || []).forEach((font, index) => {
    if (!font?.family || !font?.assetUrl || font.obfuscated || !window.FontFace) return;
    const face = new FontFace(font.family, `url(${font.assetUrl})`, { style: font.style || "normal", weight: String(font.weight || 400) });
    face.load().then(loaded => document.fonts.add(loaded)).catch(() => {});
  });
  const normalizeDeck = value => {
    const next = structuredClone(value);
    registerFontAssets(next.fontAssets);
    (next.slides || []).forEach((slide, index) => {
      if (!slide.id) slide.id = "slide-" + (index + 1);
      (slide.elements || []).forEach(element => {
        element.style = canonicalStyle(element);
        if (String(element.style.shape || "").toLowerCase() === "roundrect" && element.style.radius == null) element.style.radius = 12;
        element.flipH = !!element.flipH;
        element.flipV = !!element.flipV;
      });
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
  const verticalAlign = value => ({ t: "flex-start", top: "flex-start", ctr: "center", center: "center", middle: "center", b: "flex-end", bottom: "flex-end" }[String(value || "").toLowerCase()] || undefined);
  const textRuns = (element, base, scale = 1) => {
    const runs = Array.isArray(element?.textRuns) ? element.textRuns : [];
    const containerStyle = { display: "flex", flexDirection: "column", justifyContent: verticalAlign(base.verticalAlign) || "flex-start", width: "100%", height: "100%", textAlign: textAlign(base.align) };
    if (!runs.length) return h("div", { "data-ppt-text-container": "true", style: containerStyle }, element?.content || "");
    // Some producers keep line breaks only in the compatibility `content`
    // field. Preserve those breaks when there is a single rich-text run.
    if (runs.length === 1 && typeof element?.content === "string" && element.content.includes("\n") && !String(runs[0]?.content || "").includes("\n")) {
      return h("div", { "data-ppt-text-container": "true", style: containerStyle }, h("span", { style: { ...base, fontSize: Number(base.fontSize) ? Number(base.fontSize) * scale : undefined, whiteSpace: "pre-wrap" } }, element.content));
    }
    const rendered = runs.flatMap((run, index) => {
      const style = { ...base, ...(run?.style || {}) };
      return { paragraph: Number.isFinite(Number(run?.paragraph)) ? Number(run.paragraph) : 0, node: h("span", { key: index, style: { fontFamily: style.fontFamily, fontSize: Number(style.fontSize) ? Number(style.fontSize) * scale : undefined, color: style.color, fontWeight: style.bold ? "bold" : "normal", fontStyle: style.italic ? "italic" : "normal", textDecoration: style.underline ? "underline" : "none" } }, run?.content || "") };
    });
    const blocks = [];
    rendered.forEach(item => {
      let block = blocks[blocks.length - 1];
      if (!block || block.paragraph !== item.paragraph) { block = { paragraph: item.paragraph, nodes: [] }; blocks.push(block); }
      block.nodes.push(item.node);
    });
    const paragraphs = Array.isArray(element?.textParagraphs) ? element.textParagraphs : [];
    return h("div", { "data-ppt-text-container": "true", style: containerStyle }, blocks.map((block, index) => h("div", { key: "paragraph" + index, style: { display: "block", width: "100%", whiteSpace: "pre-wrap", textAlign: textAlign(paragraphs[block.paragraph]?.align) || textAlign(base.align) } }, block.nodes)));
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
  // Read the rendered rich-text tree without collapsing spans that carry
  // character-level formatting. Paragraph containers are kept as indexes.
  const readEditableRuns = node => {
    const out = [], container = node?.querySelector?.("[data-ppt-text-container]") || node, paragraphs = [...(container?.childNodes || [])];
    const visit = (n, paragraph, inherited) => {
      if (n.nodeType === 3) {
        if (n.nodeValue) out.push({ content: n.nodeValue, paragraph, style: inherited });
        return;
      }
      if (n.nodeType !== 1) return;
      const cs = getComputedStyle(n);
      const style = { fontFamily: cs.fontFamily, fontSize: parseFloat(cs.fontSize) || undefined, color: cs.color, bold: Number(cs.fontWeight) >= 600, italic: cs.fontStyle === "italic", underline: (cs.textDecorationLine || "").includes("underline") };
      [...n.childNodes].forEach(child => visit(child, paragraph, style));
    };
    paragraphs.forEach((paragraph, index) => [...paragraph.childNodes].forEach(child => visit(child, index, {})));
    return out.filter(run => run.content);
  };
  const customPath = (element, style = {}) => {
    const geometry = element?.geometry;
    if (!geometry || (geometry.type !== "custom" && geometry.type !== "line")) return null;
    const line = element.line || {};
    const hasLine = line.color && line.color.type !== "none" || style.border;
    const stroke = hasLine ? (line.color?.type !== "none" ? (line.color?.rgb || line.color?.css || style.border?.split?.(" ").slice(-1)[0] || "currentColor") : (style.border ? style.border.split(" ").slice(-1)[0] : "none")) : "none";
    const width = hasLine ? (Number(line.width) || Number(style.border?.split?.(" ")[0]) || 1) : 0;
    if (geometry.type === "line") return h("svg", { className: "shape-svg", viewBox: "0 0 100 100", preserveAspectRatio: "none", style: { position: "absolute", inset: 0, width: "100%", height: "100%", pointerEvents: "none", overflow: "visible" } }, h("line", { x1: 0, y1: 0, x2: 100, y2: 100, fill: "none", stroke, strokeWidth: width, vectorEffect: "non-scaling-stroke" }));
    const fillID = "ppt-fill-" + String(element.id || "shape").replace(/[^a-zA-Z0-9_-]/g, "_");
    const fill = element.fill?.type === "none" ? "none" : element.fill?.type === "image" || /^data:image\//i.test(String(style.background || "")) ? `url(#${fillID})` : (element.fill?.color ? (element.fill.color.rgb || element.fill.color.css || "currentColor") : (style.background || "currentColor"));
    const image = style.background || element.fill?.image?.assetUrl;
    const viewBox = geometry.viewBox || "0 0 1000 1000";
    return h("svg", { className: "shape-svg", viewBox, preserveAspectRatio: "none", style: { position: "absolute", inset: 0, width: "100%", height: "100%", pointerEvents: "none" } }, h("defs", null, fill === `url(#${fillID})` && h("pattern", { id: fillID, patternContentUnits: "objectBoundingBox", width: 1, height: 1 }, h("image", { href: image, xlinkHref: image, x: 0, y: 0, width: 1, height: 1, preserveAspectRatio: "none" }))), h("path", { d: geometry.path, fill, fillRule: "evenodd", stroke, strokeWidth: width || undefined }));
  };
  const imageCropStyle = (style = {}) => {
    const crop = style.crop || {}, left = Math.max(0, Math.min(.99, Number(crop.left) || 0)), top = Math.max(0, Math.min(.99, Number(crop.top) || 0)), right = Math.max(0, Math.min(.99 - left, Number(crop.right) || 0)), bottom = Math.max(0, Math.min(.99 - top, Number(crop.bottom) || 0)), width = 100 / (1 - left - right), height = 100 / (1 - top - bottom);
    return { position: "absolute", maxWidth: "none", width: width + "%", height: height + "%", left: (-left * width) + "%", top: (-top * height) + "%", objectFit: "fill" };
  };
  const ImageBlock = ({ src, style, alt = "" }) => h("div", { className: "image-frame" }, h("img", { src, alt, loading: "lazy", decoding: "async", style: imageCropStyle(style) }));
  const elementTransform = element => {
    const rotation = Number(element?.rotation) || 0;
    const sx = element?.flipH ? -1 : 1;
    const sy = element?.flipV ? -1 : 1;
    if (!rotation && sx === 1 && sy === 1) return undefined;
    return `rotate(${rotation}deg) scale(${sx}, ${sy})`;
  };
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

  window.PPTEditor = { ...(window.PPTEditor || {}), names, defaults, boxStyle, fillStyle, imageFill, shapeStyle, visualStyle, normalizeDeck, textAlign, verticalAlign, textRuns, rewriteRunContent, readEditableRuns, customPath, ImageBlock, elementTransform, api, readSSE, icon };
})();
