(() => {
  "use strict";
  const { useState, useEffect, useRef } = React, h = React.createElement, names = { text: "文本", shape: "形状", table: "表格", chart: "图表", image: "图片" }, defaults = { fontSize: 24, fontFamily: "Microsoft YaHei", bold: false, italic: false, underline: false, color: "#111827", background: "transparent", lineHeight: 1.2, letterSpacing: 0 }, shapeClip = s => ({ ellipse: "ellipse(50% 50% at 50% 50%)", oval: "ellipse(50% 50% at 50% 50%)", triangle: "polygon(50% 0%,100% 100%,0% 100%)", diamond: "polygon(50% 0%,100% 50%,50% 100%,0% 50%)", hexagon: "polygon(25% 0%,75% 0%,100% 50%,75% 100%,25% 100%,0% 50%)", pentagon: "polygon(50% 0%,100% 38%,82% 100%,18% 100%,0% 38%)", parallelogram: "polygon(20% 0%,100% 0%,80% 100%,0% 100%)", righttriangle: "polygon(0 0,100% 100%,0 100%)", rttriangle: "polygon(0 0,100% 100%,0 100%)" }[String(s || "").toLowerCase()]);
  const boxStyle = (st = {}) => ({ border: st.border ? (typeof st.border === "string" ? st.border : `${st.border.width || 1}px solid ${st.border.color || "#64748b"}`) : undefined, boxShadow: st.shadow || undefined });
  const fillStyle = v => !v ? {} : (String(v).includes("gradient") ? { backgroundImage: v, backgroundColor: "transparent" } : { backgroundColor: v });
  const shapeStyle = (s, st = {}) => { const k = String(s || "").toLowerCase(); if (k === "donut") return { maskImage: "radial-gradient(circle, transparent 0 38%, #000 40%)", WebkitMaskImage: "radial-gradient(circle, transparent 0 38%, #000 40%)" }; if (k === "roundrect") return { borderRadius: `${Math.max(0, Math.min(50, Number(st.radius) || 12))}%` }; return { clipPath: shapeClip(s) }; };
  // Normalize the structured DrawingML model into the legacy CSS surface used
  // by the editor.  Imported decks may contain either representation.
  // `style` is the single editable source of truth. Structured fields from
  // PPTX import are fallbacks only, so re-rendering cannot undo an edit.
  const visualStyle = (e, st = {}) => { const out = {}; const f = e && e.fill, l = e && e.line, t = e && e.textStyle, g = e && e.geometry; if (f && st.background == null) { if (f.type === "solid" && f.color) out.background = f.color.rgb || f.color.css || "transparent"; else if (f.type === "gradient" && f.gradient && f.gradient.stops) out.background = `linear-gradient(${Number(f.gradient.angle) || 0}deg,${f.gradient.stops.map(x => `${x.color.rgb || x.color.css || "#000"} ${Number(x.position) || 0}%`).join(",")})`; else if (f.type === "none") out.background = "transparent"; } if (l && st.border == null && l.color && l.color.type !== "none") out.border = `${Number(l.width) || 1}px ${l.dash && /dot/i.test(l.dash) ? "dotted" : l.dash && l.dash !== "solid" ? "dashed" : "solid"} ${l.color.rgb || l.color.css || "#64748b"}`; if (t) { if (st.fontFamily == null && t.fontFamily) out.fontFamily = t.fontFamily; if (st.fontSize == null && t.fontSize) out.fontSize = t.fontSize; if (st.bold == null && t.bold != null) out.bold = t.bold; if (st.italic == null && t.italic != null) out.italic = t.italic; if (st.underline == null && t.underline != null) out.underline = t.underline; if (st.lineHeight == null && t.lineHeight) out.lineHeight = t.lineHeight; if (st.align == null && t.align) out.align = t.align; } if (g && g.type && st.shape == null) out.shape = g.preset || g.type; if (e && (e.flipH || e.flipV)) out.transform = `${e.flipH ? "scaleX(-1) " : ""}${e.flipV ? "scaleY(-1)" : ""}`.trim(); if (e && e.effects) { if (st.shadow == null) out.shadow = e.effects.shadow || undefined; if (st.boxShadow == null && e.effects.innerShadow) out.boxShadow = e.effects.innerShadow; } return out; };
  const canonicalStyle = e => ({ ...defaults, ...(e && e.style || {}), ...visualStyle(e, e && e.style || {}) });
  const normalizeDeck = value => { const n = structuredClone(value); (n.slides || []).forEach(s => (s.elements || []).forEach(e => { e.style = canonicalStyle(e); })); return n; };
  const textAlign = value => ({ l: "left", ctr: "center", r: "right", just: "justify", dist: "justify", thaiDist: "justify" }[String(value || "").toLowerCase()] || value || undefined);
  const textRuns = (e, base, scale = 1) => {
    const runs = Array.isArray(e && e.textRuns) ? e.textRuns : [];
    if (!runs.length) return e && e.content || "";
    return runs.flatMap((run, i) => {
      const rs = { ...base, ...(run && run.style || {}) };
      const lead = i > 0 && Number(run?.paragraph) !== Number(runs[i-1]?.paragraph) ? h("br", { key: "br"+i }) : null;
      return [lead, h("span", { key: i, style: { fontFamily: rs.fontFamily, fontSize: Number(rs.fontSize) ? Number(rs.fontSize) * scale : undefined, color: rs.color, fontWeight: rs.bold ? "bold" : "normal", fontStyle: rs.italic ? "italic" : "normal", textDecoration: rs.underline ? "underline" : "none" } }, run && run.content || "")];
    });
  };
const splitEditorParagraphs = runs => { let p = 0, out = []; (runs || []).forEach(r => String(r.content || "").split(/\n/).forEach((part, i, a) => { if (part) out.push({ ...r, content: part, paragraph: p }); if (i < a.length - 1) p++; })); return out; };
  const rewriteRunContent = (element, content) => {
    const runs = Array.isArray(element.textRuns) ? element.textRuns : [];
    if (!runs.length) { element.content = content; return; }
    let cursor = 0;
    element.textRuns = runs.map((run, i) => {
      const remainingRuns = runs.length - i - 1;
      const oldPartLength = String(run.content || "").length;
      const take = i === runs.length - 1 ? content.length - cursor : Math.min(oldPartLength, Math.max(0, content.length - cursor - remainingRuns));
      const part = content.slice(cursor, cursor + take);
      cursor += take;
      return { ...run, content: part };
    });
    // Keep the flat field in sync for export, validation and integrations.
    element.content = content;
  };
  // TextBlock owns rich-text display and the contentEditable lifecycle. The
  // canvas only decides when editing starts and commits the normalized runs.
  const TextBlock = ({ element, style, editing, editRef, scale = 1, onCommit }) => {
    if (!editing) return textRuns(element, style, scale);
    return h("div", { ref: editRef, className: "inline-editor", contentEditable: true, suppressContentEditableWarning: true,
      onBlur: ev => {
        const next = [];
        [...(ev.currentTarget.childNodes || [])].forEach(n => {
          const content = n.textContent || "";
          if (!content) return;
          if (n.nodeType === 3) next.push({ content, style: {} });
          else { const cs = getComputedStyle(n); next.push({ content, style: { fontFamily: cs.fontFamily, fontSize: parseFloat(cs.fontSize) || undefined, color: cs.color, bold: cs.fontWeight >= 600, italic: cs.fontStyle === "italic", underline: (cs.textDecorationLine || "").includes("underline") } }); }
        });
        onCommit(next.filter(r => r.content), ev.currentTarget.textContent || "");
      },
      onKeyDown: ev => { if (ev.key === "Escape") { ev.preventDefault(); onCommit(null); } }
    }, textRuns(element, style));
  };
  const customPath = e => e && e.geometry && e.geometry.type === "custom" && e.geometry.path ? h("svg", { className: "shape-svg", viewBox: e.geometry.viewBox || "0 0 1000 1000", preserveAspectRatio: "none", style: { position: "absolute", inset: 0, width: "100%", height: "100%", pointerEvents: "none" } }, h("path", { d: e.geometry.path, fill: e.fill && e.fill.color ? (e.fill.color.rgb || e.fill.color.css || "currentColor") : "currentColor", stroke: e.line && e.line.color && e.line.color.type !== "none" ? (e.line.color.rgb || e.line.color.css || "none") : "none", strokeWidth: e.line && e.line.width ? Number(e.line.width) : 0 })) : null;
  // OOXML srcRect values are thousandths of a percent.  Expand the image
  // inside its clipped frame so the requested crop is visible in the editor.
  const imageCropStyle = (style = {}) => { const c = style.crop || {}, l = Math.max(0, Math.min(0.99, Number(c.left) || 0)), t = Math.max(0, Math.min(0.99, Number(c.top) || 0)), r = Math.max(0, Math.min(0.99 - l, Number(c.right) || 0)), b = Math.max(0, Math.min(0.99 - t, Number(c.bottom) || 0)), iw = 100 / (1 - l - r), ih = 100 / (1 - t - b); return { position: "absolute", maxWidth: "none", width: iw + "%", height: ih + "%", left: (-l * iw) + "%", top: (-t * ih) + "%", objectFit: "fill" }; };
  // Keep the image inside its element frame. The crop transform intentionally
  // enlarges/repositions the bitmap, so clipping must happen on a wrapper;
  // applying overflow:hidden to `.el` would also hide its resize handles.
  const ImageBlock = ({ src, style, alt = "" }) => h("div", { className: "image-frame" }, h("img", { src, alt, style: imageCropStyle(style) }));
  const elementTransform = e => e.rotation ? `rotate(${Number(e.rotation) || 0}deg)` : undefined;
  const api = async (url, o) => {
    const r = await fetch(url, o);
    if (!r.ok) throw Error("请求失败（" + r.status + "）");
    return r
  }, icon = n => h("i", { "data-lucide": n });
  const pptReferences = () => ({ text: document.querySelector("#pptReferenceText")?.value || "", images: [...(document.querySelector("#pptReferenceImages")?.files || [])], deck: document.querySelector("#pptReferenceDeck")?.files?.[0] || null });
  const referenceStatus = () => { const r = pptReferences(), el = document.querySelector("#pptReferenceStatus"); if (el) el.textContent = [r.images.length ? `${r.images.length} 张图片` : "", r.deck ? r.deck.name : ""].filter(Boolean).join(" · "); };
  document.querySelector("#pptReferenceImages")?.addEventListener("change", referenceStatus);
  document.querySelector("#pptReferenceDeck")?.addEventListener("change", referenceStatus);
  setTimeout(() => [...document.querySelectorAll("button")].forEach(btn => { if (btn.textContent.includes("生成草稿")) btn.childNodes.forEach(n => { if (n.nodeType === 3 && n.textContent.includes("生成草稿")) n.textContent = n.textContent.replace("生成草稿", "生成 PPT"); }); }), 0);
  function App() {
    const [deck, setDeck] = useState(null), [slide, setSlide] = useState(0), [selected, setSelected] = useState(null), [editing, setEditing] = useState(null), [topic, setTopic] = useState("产品介绍"), [theme, setTheme] = useState(""), [audience, setAudience] = useState(""), [references, setReferences] = useState(""), [images, setImages] = useState(""), [plan, setPlan] = useState(null), [busy, setBusy] = useState(false), [showJson, setShowJson] = useState(false), [showPageJson, setShowPageJson] = useState(false), [jsonText, setJsonText] = useState(""), [zoom, setZoom] = useState(1), [inspectorOpen, setInspectorOpen] = useState(true), drag = useRef(null), resize = useRef(null), fileInput = useRef(null), openFileInput = useRef(null), imageTarget = useRef(null), editRef = useRef(null), history = useRef({ past: [], future: [] }), wheelLock = useRef(false), elements = deck?.slides?.[slide]?.elements || [], current = elements.find(e => e.id === selected);
    useEffect(() => { const onGenerated = ev => { if (!ev.detail) return; setDeck(normalizeDeck(ev.detail)); history.current = { past: [], future: [] }; setSlide(0); setSelected(null); }; const onPage = ev => setDeck(d => { if (!d) return d; const n = structuredClone(d), at = Math.max(0, Math.min(slide, n.slides.length - 1)); if (ev.detail === "add") n.slides.splice(at + 1, 0, { id: "slide-" + Date.now(), layout: "content", elements: [] }); if (ev.detail === "duplicate") n.slides.splice(at + 1, 0, structuredClone({ ...n.slides[at], id: "slide-" + Date.now() })); if (ev.detail === "delete" && n.slides.length > 1) n.slides.splice(at, 1); return n; }); addEventListener("ppt:generated", onGenerated); addEventListener("ppt:page", onPage); return () => { removeEventListener("ppt:generated", onGenerated); removeEventListener("ppt:page", onPage); }; }, [slide]);
    const trackedSetDeck = fn => setDeck(d => {
      const n = typeof fn === "function" ? fn(d) : fn;
      if (!d || !n || n === d) return n;
      history.current.past.push(structuredClone(d));
      if (history.current.past.length > 100) history.current.past.shift();
      history.current.future = [];
      return n;
    });
    const update = (fn, targetId = selected, track = true) => (track ? trackedSetDeck : setDeck)(d => {
      // Canvas editing can blur after another object has been selected.  Do
      // not resolve the target from the mutable `selected` state at commit
      // time; callers editing a specific object pass its id explicitly.
      const n = structuredClone(d), e = n.slides[slide].elements.find(e => e.id === targetId);
      if (e) fn(e);
      return n
    });
    const W = Number(deck?.width) || 1280, H = Number(deck?.height) || 720;
    const jsonSnapshot = current ? structuredClone(current) : null;
    const pageJsonSnapshot = deck ? { ...deck, slides: (deck.slides || []).slice(slide, slide + 1) } : null;
    useEffect(() => { if (showJson) setJsonText(JSON.stringify(jsonSnapshot || {}, null, 2)); }, [showJson, selected, slide, deck]);
    useEffect(() => { const onWheel = ev => { const target = ev.target; if (!target || !target.closest || !target.closest('.canvas-wrap')) return; if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); ev.stopPropagation(); setZoom(z => Math.max(.25, Math.min(2, +(z + (ev.deltaY < 0 ? .1 : -.1)).toFixed(2)))); return; } ev.preventDefault(); if (wheelLock.current) return; wheelLock.current = true; setSlide(i => Math.max(0, Math.min((deck?.slides?.length || 1) - 1, i + (ev.deltaY > 0 ? 1 : -1)))); setSelected(null); setTimeout(() => { wheelLock.current = false; }, 180); }; document.addEventListener('wheel', onWheel, { passive: false, capture: true }); return () => document.removeEventListener('wheel', onWheel, { capture: true }); }, [deck]);
    const undo = () => { const h = history.current; if (!h.past.length) return; setDeck(d => { const prev = h.past.pop(); h.future.push(structuredClone(d)); return prev; }); setSelected(null); setEditing(null); };
    const redo = () => { const h = history.current; if (!h.future.length) return; setDeck(d => { const next = h.future.pop(); h.past.push(structuredClone(d)); return next; }); setSelected(null); setEditing(null); };
    useEffect(() => {
      if (!editing || !editRef.current) return;
      editRef.current.focus();
      const range = document.createRange();
      range.selectNodeContents(editRef.current);
      range.collapse(false);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
    }, [editing]);
    const generate = async () => {
      setBusy(true);
      try {
        const refs = pptReferences();
        const r = await api("/api/ppt/draft", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title: topic, theme, references: refs.text ? [refs.text] : [] }) });
        setDeck(await r.json());
        history.current = { past: [], future: [] };
        setSlide(0);
        setSelected(null)
      } catch (e) { alert("生成草稿失败：" + e.message) } finally { setBusy(false) }
    };
    const generateApproved = async () => {
      if (!plan) return;
      setBusy(true);
      try {
        const r = await api("/api/ppt/generate", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(plan) });
        setDeck(await r.json());
        history.current = { past: [], future: [] };
        setSlide(0);
        setSelected(null)
        setPlan(null);
      } catch (e) { alert("生成 PPT 失败：" + e.message) } finally { setBusy(false) }
    };
    // Start with an empty canvas; the AI 制作入口 is the explicit creation flow.
    useEffect(() => { window.lucide && window.lucide.createIcons() });
    const insertImage = data => {
      if (!deck || !data) return;
      const e = { id: "image-" + Date.now(), type: "image", x: 120, y: 120, width: 420, height: 260, content: "", assetUrl: data, style: { ...defaults } };
      trackedSetDeck(d => {
        const n = structuredClone(d);
        n.slides[slide].elements.push(e);
        return n
      });
      setSelected(e.id)
    };
    const add = type => {
      if (!deck) return;
      if (type === "undo") { undo(); return; }
      if (type === "redo") { redo(); return; }
      if (type === "image") { imageTarget.current = null; fileInput.current?.click(); return }
      const e = { id: type + "-" + Date.now(), type, x: 120, y: 120, width: 300, height: 100, content: { text: "双击编辑", shape: "形状", table: "A | B\n1 | 2", chart: "数据图表" }[type], style: { ...defaults, background: type === "shape" ? "#dbeafe" : "transparent" }, ...(type === "text" ? { textRuns: [{ content: "双击编辑", style: { ...defaults } }] } : {}) };
      trackedSetDeck(d => {
        const n = structuredClone(d);
        n.slides[slide].elements.push(e);
        return n
      });
      setSelected(e.id);
      if (type === "image") fileInput.current?.click()
    };
    const removeSelected = () => {
      if (!selected || editing) return;
      if (!window.confirm("确定要删除当前选中的元素吗？删除后无法恢复。")) return;
      trackedSetDeck(d => {
        const n = structuredClone(d);
        n.slides[slide].elements = n.slides[slide].elements.filter(e => e.id !== selected);
        return n
      });
      setSelected(null)
    };
    const openDeck = () => { if (openFileInput.current) { openFileInput.current.accept = ".json,.pptx"; openFileInput.current.click(); } };
    const loadDeckFile = ev => {
      const file = ev.target.files?.[0]; ev.target.value = "";
      if (!file) return;
      const reader = new FileReader();
      reader.onload = async () => { try { const isPptx = file.name.toLowerCase().endsWith(".pptx"); const response = await fetch("/api/pptx/import", { method: "POST", headers: { "Content-Type": isPptx ? "application/vnd.openxmlformats-officedocument.presentationml.presentation" : "application/json" }, body: reader.result }); if (!response.ok) throw Error((await response.text()) || ("请求失败（" + response.status + "）")); const value = await response.json(); if (!value || !Array.isArray(value.slides) || !value.slides.length) throw Error("文件不是有效的 PPT 数据"); setDeck(normalizeDeck(value)); history.current = { past: [], future: [] }; setSlide(0); setSelected(null); setEditing(null); setTopic(value.title || "产品介绍"); } catch (e) { alert("打开 PPT 文件失败：" + e.message); } };
      reader.readAsArrayBuffer(file);
    };
    useEffect(() => {
      const onKeyDown = ev => {
        const target = ev.target;
        const tag = target?.tagName?.toLowerCase();
        if (editing || tag === "input" || tag === "textarea" || target?.isContentEditable) return;
        if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "z") { ev.preventDefault(); ev.shiftKey ? redo() : undo(); return; }
        if ((ev.ctrlKey || ev.metaKey) && (ev.key.toLowerCase() === "y" || (ev.key.toLowerCase() === "z" && ev.shiftKey))) { ev.preventDefault(); redo(); return; }
        if ((ev.key === "Delete" || ev.key === "Backspace") && selected) {
          ev.preventDefault();
          removeSelected();
        }
      };
      addEventListener("keydown", onKeyDown);
      return () => removeEventListener("keydown", onKeyDown)
    }, [selected, editing, slide, deck]);
    useEffect(() => {
      const paste = ev => {
        const i = [...(ev.clipboardData?.items || [])].find(i => i.type.startsWith("image/"));
        if (!i) return;
        const f = i.getAsFile();
        if (!f) return;
        const r = new FileReader();
        r.onload = () => insertImage(r.result);
        r.readAsDataURL(f);
        ev.preventDefault()
      };
      addEventListener("paste", paste);
      return () => removeEventListener("paste", paste)
    }, [deck, slide]);
    const move = ev => {
      const d = drag.current, r = resize.current, c = (d || r)?.canvas;
      if (!c) return;
      const q = c.getBoundingClientRect(), dx = (ev.clientX - (d || r).x) * W / q.width, dy = (ev.clientY - (d || r).y) * H / q.height;
      if (d) update(e => {
        e.x = d.ex + dx;
        e.y = d.ey + dy
      });
      else update(e => {
        let x = r.ex, y = r.ey, w = r.ew, h = r.eh;
        if (r.dir.includes("e")) w = Math.max(40, r.ew + dx);
        if (r.dir.includes("s")) h = Math.max(30, r.eh + dy);
        if (r.dir.includes("w")) {
          w = Math.max(40, r.ew - dx);
          x = r.ex + dx
        } if (r.dir.includes("n")) {
          h = Math.max(30, r.eh - dy);
          y = r.ey + dy
        } e.x = x;
        e.y = y;
        e.width = w;
        e.height = h
      })
    };
    useEffect(() => {
      const up = () => {
        drag.current = null;
        resize.current = null
      };
      addEventListener("mousemove", move);
      addEventListener("mouseup", up);
      return () => {
        removeEventListener("mousemove", move);
        removeEventListener("mouseup", up)
      }
    }, [selected, slide, deck]);
    const exportDeck = async () => {
      if (!deck) return;
      try {
        const r = await api("/api/pptx/export", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(deck) }), a = document.createElement("a");
        a.href = URL.createObjectURL(await r.blob());
        a.download = "presentation.pptx";
        a.click()
      } catch (e) { alert("导出失败：" + e.message) }
    };
    const tools = [["undo", "undo-2", "撤销"], ["redo", "redo-2", "重做"], ["text", "type", "文本"], ["shape", "shapes", "形状"], ["table", "table-2", "表格"], ["chart", "chart-column", "图表"], ["image", "image-plus", "图片"]];
    return h(React.Fragment, null, h("header", null, h("button", { className: "btn small", onClick: () => location.href = "/" }, icon("arrow-left"), h("span", null, "返回首页")), h("b", null, "快马 AI · PPT 工作台")), h("div", { className: "bar" }, h("input", { value: topic, onChange: e => setTopic(e.target.value), placeholder: "主题" }), h("input", { value: theme, onChange: e => setTheme(e.target.value), placeholder: "风格" }), h("button", { onClick: openDeck }, icon("folder-open"), h("span", null, "打开 PPT 文件")), h("button", { onClick: generate, disabled: busy }, icon("sparkles"), h("span", null, busy ? "生成中…" : "生成草稿")), h("button", { onClick: exportDeck, disabled: !deck }, icon("download"), h("span", null, "导出 PPTX")), h("button", { onClick: () => setShowJson(true), disabled: !current }, icon("code-2"), h("span", null, "对象 JSON"))), h("div", { className: "tools" }, ...tools.map(([t, i, l]) => h("button", { key: t, onClick: () => add(t) }, icon(i), h("span", null, l))), h("span", { className: "zoom-tools" }, h("button", { onClick: () => setZoom(z => Math.max(.25, +(z - .1).toFixed(2))), disabled: zoom <= .25 }, "−"), h("span", null, `${Math.round(zoom * 100)}%`), h("button", { onClick: () => setZoom(z => Math.min(2, +(z + .1).toFixed(2))), disabled: zoom >= 2 }, "+"), h("button", { onClick: () => setZoom(1) }, "重置")), h("button", { onClick: () => setInspectorOpen(v => !v) }, icon(inspectorOpen ? "panel-right-close" : "panel-right-open"), h("span", null, inspectorOpen ? "收起属性" : "展开属性")), h("button", {
      className: "danger", disabled: !current, onClick: removeSelected
    }, icon("trash-2"), h("span", null, "删除"))), h("main", { className: inspectorOpen ? "" : "inspector-collapsed" }, h("div", { id: "slides" }, (deck?.slides || []).map((s, i) => h("div", {
      key: i, className: "thumb " + (i === slide ? "active" : ""), onClick: () => {
        setSlide(i);
        setSelected(null)
      }
      }, h("div", { className: "thumb-preview" }, [...(s.elements || [])].sort((a,b) => (Number(a.zIndex)||0) - (Number(b.zIndex)||0)).map(e => { const ts = { ...defaults, ...(e.style || {}), ...visualStyle(e, e.style || {}) }; return h("div", { key: e.id, className: "thumb-el", style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: ts.opacity ?? e.opacity ?? 1, ...fillStyle(ts.background || (e.type === "shape" ? "#dbeafe" : "transparent")), ...shapeStyle(ts.shape, ts), color: ts.color || "#172033", textAlign: textAlign(ts.align), fontSize: Math.max(8, (Number(ts.fontSize) || 24) * 0.18), lineHeight: ts.lineHeight || 1.2, letterSpacing: ts.letterSpacing ? (Number(ts.letterSpacing) * .18) + "px" : undefined, fontWeight: ts.bold ? "bold" : "normal", fontStyle: ts.italic ? "italic" : "normal", textDecoration: ts.underline ? "underline" : "none", transform: elementTransform(e), transformOrigin: "center", ...boxStyle(ts) } }, e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, style: ts }) : e.type === "image" ? icon("image") : textRuns(e, ts, .18)); })), h("span", { className: "thumb-label" }, `第${i + 1}页`)))), h("div", { className: "canvas-wrap", style: { width: W * zoom + "px", height: H * zoom + "px" } }, h("div", { className: "canvas", style: { width: W + "px", height: H + "px", transform: `scale(${zoom})` }, onClick: () => setSelected(null) }, [...elements].sort((a,b) => (Number(a.zIndex)||0) - (Number(b.zIndex)||0)).map(e => {
      const st = { ...defaults, ...e.style, ...visualStyle(e, e.style || {}) }, bold = st.bold || Number(st.fontWeight) >= 600, isEditing = editing === e.id;
      return h("div", {
        key: e.id, className: "el " + (e.type === "image" ? "image-el " : "") + (e.id === selected ? "sel" : "") + (isEditing ? " editing" : ""), onClick: ev => {
          ev.stopPropagation();
          setSelected(e.id)
        }, onDoubleClick: ev => {
          ev.stopPropagation();
          if (e.type === "image") { imageTarget.current = e.id; fileInput.current?.click() }
          else if (e.type === "text") setEditing(e.id)
        }, onMouseDown: ev => {
          if (ev.button === 0 && !e.locked && !isEditing) {
            setSelected(e.id);
            drag.current = { x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, canvas: ev.currentTarget.parentElement }
          }
        }, style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: st.opacity ?? e.opacity ?? 1, fontSize: st.fontSize, fontFamily: st.fontFamily, textAlign: textAlign(st.align), lineHeight: st.lineHeight || 1.2, letterSpacing: st.letterSpacing ? st.letterSpacing + "px" : undefined, fontWeight: bold ? "bold" : "normal", fontStyle: st.italic ? "italic" : "normal", textDecoration: st.underline ? "underline" : "none", color: st.color, ...fillStyle(st.background), ...shapeStyle(st.shape, st), zIndex: Number(e.zIndex) || 0, transform: elementTransform(e), transformOrigin: "center", ...boxStyle(st) }
      }, customPath(e), e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, alt: "图片", style: st }) : e.type === "image" ? h("div", { className: "image-placeholder" }, icon("image")) : e.type === "text" && isEditing ? h("div", { ref: editRef, className: "inline-editor", contentEditable: true, suppressContentEditableWarning: true, onBlur: ev => { const node = ev.currentTarget; const next = []; [...(node?.childNodes || [])].forEach((n, i) => { if (n.nodeType === 3) next.push({ content: n.nodeValue || "", style: {} }); else { const txt = n.textContent || ""; if (txt) { const cs = getComputedStyle(n); next.push({ content: txt, style: { fontFamily: cs.fontFamily, fontSize: parseFloat(cs.fontSize) || undefined, color: cs.color, bold: cs.fontWeight >= 600, italic: cs.fontStyle === "italic", underline: (cs.textDecorationLine || "").includes("underline") } }); } } }); const runs = next.filter(r => r.content); update(x => { x.textRuns = runs.length ? runs : [{ content: node?.textContent || "", style: { ...x.style } }]; x.content = runs.map(r => r.content).join(""); }, e.id); setEditing(null) }, onKeyDown: ev => { if (ev.key === "Escape") { ev.preventDefault(); setEditing(null) } } }, textRuns(e, st)) : textRuns(e, st), e.id === selected && !isEditing && ["nw", "ne", "sw", "se"].map(dir => h("span", {
        key: dir, className: "handle " + dir, onMouseDown: ev => {
          ev.stopPropagation();
          resize.current = { x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, ew: e.width, eh: e.height, dir, canvas: ev.currentTarget.parentElement.parentElement }
        }
      })))
    }))), h("input", {
      ref: fileInput, type: "file", accept: "image/*", style: { display: "none" }, onChange: ev => {
        const f = ev.target.files?.[0];
        if (f) {
          const r = new FileReader();
          r.onload = () => { if (imageTarget.current) { const id = imageTarget.current; update(e => { e.assetUrl = r.result }, id); setSelected(id); imageTarget.current = null } else insertImage(r.result) };
          r.readAsDataURL(f)
        } ev.target.value = ""
      }
    }), h("input", { ref: openFileInput, type: "file", accept: ".json,application/json", style: { display: "none" }, onChange: loadDeckFile }), inspectorOpen && h(Inspector, { element: current, onChange: update, onReplaceImage: id => { imageTarget.current = id; fileInput.current?.click() } }), showJson && h("div", { className: "json-overlay", onClick: () => setShowJson(false) }, h("div", { className: "json-panel", onClick: e => e.stopPropagation() }, h("div", { className: "json-head" }, h("b", null, current ? `对象 JSON · ${names[current.type] || current.type}` : "对象 JSON"), h("button", { onClick: () => setShowJson(false) }, "关闭")), current ? h("textarea", { className: "json-editor", value: jsonText, onChange: e => setJsonText(e.target.value), spellCheck: false }) : h("div", { className: "json-empty" }, "请先选择一个对象"), current && h("div", { className: "json-actions" }, h("button", { className: "btn primary", onClick: () => { try { const parsed = JSON.parse(jsonText); update(x => { Object.keys(x).forEach(k => delete x[k]); Object.assign(x, parsed); }); setShowJson(false); } catch (e) { alert("JSON 格式错误：" + e.message); } } }, "应用修改"), h("button", { onClick: () => setJsonText(JSON.stringify(jsonSnapshot || {}, null, 2)) }, "还原"))))))
  }
  function Inspector({ element, onChange, onReplaceImage, onJson }) {
    const st = { ...defaults, ...(element?.style || {}) };
    if (!element) return h("aside", { className: "side" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, "未选择元素"));
    const setStyle = (x, key, value) => {
      x.style = { ...x.style, [key]: value };
      if (Array.isArray(x.textRuns)) {
        x.textRuns = x.textRuns.map(run => ({ ...run, style: { ...run.style, [key]: value } }));
      }
    }, field = (label, key, type = "text") => h("label", null, label, h("input", {
      type, step: type === "number" ? "0.1" : undefined, value: type === "number" && ["x", "y", "width", "height"].includes(key) ? Number(element[key] || 0).toFixed(1) : (element[key] ?? st[key] ?? ""), onChange: e => onChange(x => {
        if (key in x) x[key] = type === "number" ? Number(e.target.value) : e.target.value;
        else setStyle(x, key, type === "number" ? Number(e.target.value) : e.target.value)
      })
    })), cropField = (label, key) => h("label", null, label, h("input", { type: "number", min: 0, max: 0.99, step: 0.01, value: Number(st.crop?.[key] || 0), onChange: e => onChange(x => x.style = { ...x.style, crop: { ...(x.style?.crop || {}), [key]: Number(e.target.value) } }) })), toggle = (label, key) => h("label", { className: "check" }, h("input", { type: "checkbox", checked: !!st[key], onChange: e => onChange(x => setStyle(x, key, e.target.checked)) }), label);
    const hasShape = element.type === "shape" || !!st.shape, fillLabel = hasShape && element.type !== "text" ? "形状填充" : "文字填充";
    return h("aside", { className: "side" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, names[element.type] || element.type, st.shapeName ? ` · ${st.shapeName}` : ""), hasShape && field("形状类型", "shape"), hasShape && field("形状名称", "shapeName"), String(st.shape || "").toLowerCase() === "roundrect" && field("圆角大小(%)", "radius", "number"), element.type === "image" && h("button", { className: "replace-image", type: "button", onClick: () => onReplaceImage?.(element.id) }, "替换图片"), element.type === "image" && h("section", { className: "geometry" }, cropField("裁剪左", "left"), cropField("裁剪上", "top"), cropField("裁剪右", "right"), cropField("裁剪下", "bottom")), element.type !== "image" && h("label", null, "内容", h("textarea", { value: element.content || "", onChange: e => onChange(x => {
      // Keep character-level formatting when editing text from the inspector.
      // Removing textRuns here made the renderer fall back to the element
      // defaults, so imported bold/colour/font differences disappeared.
      if (x.type === "text") rewriteRunContent(x, e.target.value);
      else x.content = e.target.value;
    }) })), h("section", { className: "geometry" }, field("X 位置", "x", "number"), field("Y 位置", "y", "number"), field("宽度", "width", "number"), field("高度", "height", "number")), field("对象透明度", "opacity", "number"), element.type === "text" && h("section", null, field("字号", "fontSize", "number"), field("字体", "fontFamily"), field("文字颜色", "color", "color"), h("label", null, "文字对齐", h("select", { value: st.align || "l", onChange: e => onChange(x => x.style = { ...x.style, align: e.target.value }) }, h("option", { value: "l" }, "左对齐"), h("option", { value: "ctr" }, "居中"), h("option", { value: "r" }, "右对齐"), h("option", { value: "just" }, "两端对齐"))), field("行间距", "lineHeight", "number"), field("字间距", "letterSpacing", "number"), h("div", { className: "checks" }, toggle("加粗", "bold"), toggle("斜体", "italic"), toggle("下划线", "underline"))), (element.type === "shape" || element.type === "text") && field(fillLabel, "background"), h("section", { className: "geometry" }, field("边框", "border"), field("阴影", "shadow")), field("旋转角度", "rotation", "number"))
  }
  ReactDOM.createRoot(document.getElementById("app")).render(h(App));
})();



