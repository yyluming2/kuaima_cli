(() => {
  "use strict";
  const moveStreamStatusToCanvas = () => { const status = document.querySelector("body > .ppt-stream-status"), canvas = document.querySelector(".canvas-wrap"); if (status && canvas) canvas.appendChild(status); };
  setInterval(moveStreamStatusToCanvas, 100);
  const { useState, useEffect, useRef } = React, h = React.createElement;
  const { names, defaults, boxStyle, fillStyle, imageFill, shapeStyle, visualStyle, normalizeDeck, textAlign, verticalAlign, textRuns, readEditableRuns, customPath, ImageBlock, elementTransform, api, readSSE, icon, Inspector } = window.PPTEditor;
  const pptReferences = () => ({ text: document.querySelector("#pptReferenceText")?.value || "", images: [...(document.querySelector("#pptReferenceImages")?.files || [])], deck: document.querySelector("#pptReferenceDeck")?.files?.[0] || null });
  const referenceStatus = () => { const r = pptReferences(), el = document.querySelector("#pptReferenceStatus"); if (el) el.textContent = [r.images.length ? `${r.images.length} 张图片` : "", r.deck ? r.deck.name : ""].filter(Boolean).join(" · "); };
  document.querySelector("#pptReferenceImages")?.addEventListener("change", referenceStatus);
  document.querySelector("#pptReferenceDeck")?.addEventListener("change", referenceStatus);
  setTimeout(() => [...document.querySelectorAll("button")].forEach(btn => { if (btn.textContent.includes("生成草稿")) btn.childNodes.forEach(n => { if (n.nodeType === 3 && n.textContent.includes("生成草稿")) n.textContent = n.textContent.replace("生成草稿", "生成 PPT"); }); }), 0);
  function App() {
    const [masterMode, setMasterMode] = useState(false);
    const [deck, setDeck] = useState(null), [slide, setSlide] = useState(0), [selected, setSelected] = useState(null), [editing, setEditing] = useState(null), [topic, setTopic] = useState("产品介绍"), [theme, setTheme] = useState(""), [audience, setAudience] = useState(""), [references, setReferences] = useState(""), [images, setImages] = useState(""), [plan, setPlan] = useState(null), [busy, setBusy] = useState(false), [showJson, setShowJson] = useState(false), [showPageJson, setShowPageJson] = useState(false), [jsonText, setJsonText] = useState(""), [zoom, setZoom] = useState(1), [inspectorOpen, setInspectorOpen] = useState(true), drag = useRef(null), resize = useRef(null), slideDrag = useRef(null), fileInput = useRef(null), openFileInput = useRef(null), imageTarget = useRef(null), backgroundTarget = useRef(false), editRef = useRef(null), history = useRef({ past: new Map(), future: new Map() }), wheelLock = useRef(false), elements = (masterMode ? deck?.master?.elements : deck?.slides?.[slide]?.elements) || [], current = elements.find(e => e.id === selected);
    // PPTX object ids are often reused on every slide. Clear stale selection
    // state when changing pages so old SVG connectors and resize handles are
    // removed instead of being associated with the new page.
    useEffect(() => { setSelected(null); setEditing(null); }, [slide, masterMode]);
    const applyMasterToSlides = d => { const n = structuredClone(d), m = n.master || {}; n.theme = { ...n.theme, background: m.background || n.theme?.background, foreground: m.foreground || n.theme?.foreground, accent: m.accent || n.theme?.accent, fonts: m.fonts?.length ? m.fonts : n.theme?.fonts }; (n.slides || []).forEach(s => { const own = (s.elements || []).filter(e => !String(e.id).startsWith("master-")); const shared = (m.elements || []).map(e => ({ ...structuredClone(e), locked: true })); s.elements = [...shared, ...own]; own.forEach(e => { if (e.type !== "text") return; const isTitle = /title|heading/i.test(e.id || "") || Number(e.y) < 150; const fontSize = isTitle ? Number(m.titleSize) : Number(m.bodySize), color = (isTitle ? m.accent : m.foreground) || e.style?.color, fontFamily = m.fontFamily || e.style?.fontFamily; e.style = { ...e.style, ...(fontSize > 0 ? { fontSize } : {}), ...(fontFamily ? { fontFamily } : {}), ...(color ? { color } : {}) }; if (e.textStyle) e.textStyle = { ...e.textStyle, ...(fontSize > 0 ? { fontSize } : {}), ...(fontFamily ? { fontFamily } : {}) }; if (Array.isArray(e.textRuns)) e.textRuns = e.textRuns.map(run => ({ ...run, style: { ...run.style, ...(fontSize > 0 ? { fontSize } : {}), ...(fontFamily ? { fontFamily } : {}), ...(color ? { color } : {}) } })); }); }); return n; };
    // Entering/leaving the master canvas is only an editing-mode change.  Do
    // not materialize master typography when leaving it: doing so overwrites
    // every slide's explicitly formatted text (and cannot be recovered by
    // undo).  Master property changes are propagated by changeMaster itself.
    const toggleMasterMode = () => { if (!deck) return; setMasterMode(v => !v); setSelected(null); setEditing(null); };
    useEffect(() => { const m = document.querySelector("#pptMasterMode"), toolsBar = document.querySelector(".tools"), actions = document.querySelector(".ppt-toolbar-actions"); if (!m || !toolsBar || !actions) return; toolsBar.appendChild(actions); m.onclick = toggleMasterMode; m.innerHTML = `<i data-lucide="layout-template"></i><span>${masterMode ? "退出母版画布" : "进入母版画布"}</span>`; m.classList.toggle("active", masterMode); window.lucide && window.lucide.createIcons(); }, [masterMode, deck]);
    useEffect(() => { const onPageMaster = () => { if (!deck?.master?.elements?.length) return; setDeck(d => { if (!d) return d; const n = structuredClone(d), shared = n.master.elements.map(e => ({ ...structuredClone(e), locked: true })); n.slides = (n.slides || []).map(s => ({ ...s, elements: [...shared, ...(s.elements || []).filter(e => !String(e.id).startsWith("master-"))] })); return n; }); }; addEventListener("ppt:page", onPageMaster); return () => removeEventListener("ppt:page", onPageMaster); }, [deck]);
    const slideKey = (value, index = slide) => value?.slides?.[index]?.id || "slide-" + index;
    const historyBucket = (value, index = slide) => { const key = slideKey(value, index), h = history.current; if (!h.past.has(key)) h.past.set(key, []); if (!h.future.has(key)) h.future.set(key, []); return { key, past: h.past.get(key), future: h.future.get(key) }; };
    const resetHistory = () => { history.current = { past: new Map(), future: new Map() }; };
    useEffect(() => { const onGenerated = ev => { if (!ev.detail) return; setDeck(normalizeDeck(ev.detail)); resetHistory(); setSlide(0); setSelected(null); }; const onPage = ev => setDeck(d => { if (!d) return d; const n = structuredClone(d), at = Math.max(0, Math.min(slide, n.slides.length - 1)); if (ev.detail === "add") n.slides.splice(at + 1, 0, { id: "slide-" + Date.now(), layout: "content", elements: [] }); if (ev.detail === "duplicate") n.slides.splice(at + 1, 0, structuredClone({ ...n.slides[at], id: "slide-" + Date.now() })); if (ev.detail === "delete" && n.slides.length > 1) n.slides.splice(at, 1); return n; }); const onImagePage = async ev => { const index = Number(ev.detail); if (!deck || !Number.isInteger(index) || index < 0 || index >= deck.slides.length) return; setBusy(true); try { const r = await api("/api/ppt/images", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ presentation: deck, slideIndex: index }) }); const out = await r.json(); if (out.presentation) { const bucket = historyBucket(deck, index); bucket.past.push(structuredClone(deck.slides[index])); bucket.future.length = 0; setDeck(normalizeDeck(out.presentation)); setSlide(index); } if (out.errors?.length) alert("配图生成失败：" + out.errors.join("\n")); } catch (e) { alert("配图生成失败，文字内容已保留：" + e.message); } finally { setBusy(false); } }; const onAIPage = async ev => { const index = Number(ev.detail); if (!deck || !Number.isInteger(index) || index < 0 || index >= deck.slides.length) return; setBusy(true); const status = document.createElement("div"); status.className = "ppt-stream-status"; status.textContent = "AI 正在流式生成本页…"; document.body.appendChild(status); try { const r = await api("/api/ppt/slide?stream=1", { method: "POST", headers: { "Content-Type": "application/json", "Accept": "text/event-stream" }, body: JSON.stringify({ presentation: deck, slideIndex: index }) }); const out = await readSSE(r); if (!out || !out.slide) throw new Error("未返回生成页面数据"); setDeck(d => { const n = structuredClone(d), bucket = historyBucket(d, index); bucket.past.push(structuredClone(d.slides[index])); bucket.future.length = 0; n.slides[index] = { ...out.slide, id: d.slides[index].id || out.slide.id || "slide-" + index }; return n; }); setSlide(index); setSelected(null); } catch (e) { alert("AI 生成页面失败：" + e.message); } finally { status.remove(); setBusy(false); } }; addEventListener("ppt:generated", onGenerated); addEventListener("ppt:page", onPage); addEventListener("ppt:image-page", onImagePage); addEventListener("ppt:ai-page", onAIPage); return () => { removeEventListener("ppt:generated", onGenerated); removeEventListener("ppt:page", onPage); removeEventListener("ppt:image-page", onImagePage); removeEventListener("ppt:ai-page", onAIPage); }; }, [slide, deck]);
    const trackedSetDeck = fn => setDeck(d => {
      const n = typeof fn === "function" ? fn(d) : fn;
      if (!d || !n || n === d) return n;
      if (d?.slides?.[slide] && n?.slides?.[slide] && JSON.stringify(d.slides[slide]) !== JSON.stringify(n.slides[slide])) {
        const bucket = historyBucket(d); bucket.past.push(structuredClone(d.slides[slide]));
        if (bucket.past.length > 100) bucket.past.shift();
        bucket.future.length = 0;
      }
      return n;
    });
    const update = (fn, targetId = selected, track = true) => (track ? trackedSetDeck : setDeck)(d => {
      // Canvas editing can blur after another object has been selected.  Do
      // not resolve the target from the mutable `selected` state at commit
      // time; callers editing a specific object pass its id explicitly.
      const n = structuredClone(d), e = masterMode ? n.master?.elements?.find(e => e.id === targetId) : n.slides[slide].elements.find(e => e.id === targetId);
      if (e && (masterMode || !e.locked)) fn(e);
      return n
    });
    const changeLayer = direction => {
      if (!selected) return;
      trackedSetDeck(d => {
        const n = structuredClone(d);
        const list = masterMode ? (n.master?.elements || []) : (n.slides?.[slide]?.elements || []);
        const target = list.find(element => element.id === selected);
        if (!target || (!masterMode && target.locked)) return d;
        const ordered = list.map((element, index) => ({ element, index })).sort((a, b) => (Number(a.element.zIndex) || 0) - (Number(b.element.zIndex) || 0) || a.index - b.index).map(item => item.element);
        const from = ordered.findIndex(element => element.id === selected);
        const to = direction === "front" ? ordered.length - 1 : direction === "back" ? 0 : Math.max(0, Math.min(ordered.length - 1, from + (direction === "up" ? 1 : -1)));
        if (from === to) return d;
        ordered.splice(to, 0, ordered.splice(from, 1)[0]);
        ordered.forEach((element, index) => { element.zIndex = index; });
        if (masterMode) {
          n.master.elements = ordered;
          const layers = new Map(ordered.map(element => [element.id, element.zIndex]));
          (n.slides || []).forEach(page => (page.elements || []).forEach(element => { if (layers.has(element.id)) element.zIndex = layers.get(element.id); }));
        } else n.slides[slide].elements = ordered;
        return n;
      });
    };
    const W = Number(deck?.width) || 1280, H = Number(deck?.height) || 720;
    const jsonSnapshot = current ? structuredClone(current) : null;
    const pageJsonSnapshot = deck ? { ...deck, slides: (deck.slides || []).slice(slide, slide + 1) } : null;
    useEffect(() => { if (showJson) setJsonText(JSON.stringify(jsonSnapshot || {}, null, 2)); }, [showJson, selected, slide, deck]);
    useEffect(() => { const onWheel = ev => { const target = ev.target; if (!target || !target.closest || !target.closest('.canvas-wrap')) return; if (ev.ctrlKey || ev.metaKey) { ev.preventDefault(); ev.stopPropagation(); setZoom(z => Math.max(.25, Math.min(2, +(z + (ev.deltaY < 0 ? .1 : -.1)).toFixed(2)))); return; } ev.preventDefault(); if (wheelLock.current) return; wheelLock.current = true; setSlide(i => Math.max(0, Math.min((deck?.slides?.length || 1) - 1, i + (ev.deltaY > 0 ? 1 : -1)))); setSelected(null); setTimeout(() => { wheelLock.current = false; }, 180); }; document.addEventListener('wheel', onWheel, { passive: false, capture: true }); return () => document.removeEventListener('wheel', onWheel, { capture: true }); }, [deck]);
    const undo = () => setDeck(d => { if (!d?.slides?.[slide]) return d; const bucket = historyBucket(d); if (!bucket.past.length) return d; bucket.future.push(structuredClone(d.slides[slide])); const n = structuredClone(d); n.slides[slide] = bucket.past.pop(); setSelected(null); setEditing(null); return n; });
    const redo = () => setDeck(d => { if (!d?.slides?.[slide]) return d; const bucket = historyBucket(d); if (!bucket.future.length) return d; bucket.past.push(structuredClone(d.slides[slide])); const n = structuredClone(d); n.slides[slide] = bucket.future.pop(); setSelected(null); setEditing(null); return n; });
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
      const hasContent = !!deck?.slides?.some(s => (s.elements || []).length > 0);
      if (hasContent && !window.confirm("新建 PPT 将清空当前内容，且当前编辑无法恢复。确定继续吗？")) return;
      setBusy(true);
      try {
        const refs = pptReferences();
        const r = await api("/api/ppt/draft", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title: topic, theme, references: refs.text ? [refs.text] : [] }) });
        setDeck(normalizeDeck(await r.json()));
        resetHistory();
        setSlide(0);
        setSelected(null)
      } catch (e) { alert("生成草稿失败：" + e.message) } finally { setBusy(false) }
    };
    const generateApproved = async () => {
      if (!plan) return;
      setBusy(true);
      try {
        const r = await api("/api/ppt/generate", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(plan) });
        setDeck(normalizeDeck(await r.json()));
        resetHistory();
        setSlide(0);
        setSelected(null)
        setPlan(null);
      } catch (e) { alert("生成 PPT 失败：" + e.message) } finally { setBusy(false) }
    };
    // Start with an empty canvas; the AI 制作入口 is the explicit creation flow.
    useEffect(() => { window.lucide && window.lucide.createIcons(); const color = deck?.theme?.background || "#fff"; document.querySelectorAll(".canvas, .thumb-preview").forEach(el => { el.style.background = color; }); }, [deck]);
    useEffect(() => { document.body.classList.toggle("ppt-master-editing", masterMode); const color = masterMode ? (deck?.master?.background || deck?.theme?.background || "#fff") : (deck?.theme?.background || deck?.master?.background || "#fff"); document.documentElement.style.setProperty("--ppt-master-bg", color); document.querySelectorAll(".canvas, .thumb-preview").forEach(el => { el.style.background = color; }); return () => document.body.classList.remove("ppt-master-editing"); }, [masterMode, deck]);
    const insertImage = data => {
      if (!deck || !data) return;
      const e = { id: "image-" + Date.now(), type: "image", x: 120, y: 120, width: 420, height: 260, content: "", assetUrl: data, style: { ...defaults } };
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode) { n.master = n.master || {}; const me = { ...e, id: "master-" + e.id, locked: false }; n.master.elements = [...(n.master.elements || []), me]; (n.slides || []).forEach(s => { s.elements = [...(s.elements || []).filter(x => x.id !== me.id), { ...me, locked: true }]; }); } else n.slides[slide].elements.push(e); return n });
      setSelected(masterMode ? "master-" + e.id : e.id)
    };
    const add = type => {
      if (!deck) return;
      if (type === "undo") { undo(); return; }
      if (type === "redo") { redo(); return; }
      if (type === "image") { imageTarget.current = null; fileInput.current?.click(); return }
      const e = { id: type + "-" + Date.now(), type, x: 120, y: 120, width: 300, height: 100, content: { text: "双击编辑", shape: "", table: "A | B\n1 | 2", chart: "数据图表" }[type], style: { ...defaults, background: type === "shape" ? "#dbeafe" : "transparent", ...(type === "shape" ? { shape: "rect" } : {}) }, ...(type === "text" ? { textRuns: [{ content: "双击编辑", style: { ...defaults } }] } : {}) };
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode) { n.master = n.master || {}; const me = { ...e, id: "master-" + e.id, locked: false }; n.master.elements = [...(n.master.elements || []), me]; (n.slides || []).forEach(s => { s.elements = [...(s.elements || []).filter(x => x.id !== me.id), { ...me, locked: true }]; }); } else n.slides[slide].elements.push(e); return n });
      setSelected(masterMode ? "master-" + e.id : e.id);
      if (type === "image") fileInput.current?.click()
    };
    const removeSelected = () => {
      if (!selected || editing) return;
      if (!window.confirm("确定要删除当前选中的元素吗？删除后无法恢复。")) return;
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode && String(selected).startsWith("master-")) { n.master.elements = (n.master.elements || []).filter(e => e.id !== selected); (n.slides || []).forEach(s => { s.elements = (s.elements || []).filter(e => e.id !== selected); }); } else n.slides[slide].elements = n.slides[slide].elements.filter(e => e.id !== selected); return n });
      setSelected(null)
    };
    const openDeck = () => { if (openFileInput.current) { openFileInput.current.accept = ".json,.pptx"; openFileInput.current.click(); } };
    const loadDeckFile = ev => {
      const file = ev.target.files?.[0]; ev.target.value = "";
      if (!file) return;
      const reader = new FileReader();
      reader.onload = async () => { try { const isPptx = file.name.toLowerCase().endsWith(".pptx"); const response = await fetch("/api/pptx/import", { method: "POST", headers: { "Content-Type": isPptx ? "application/vnd.openxmlformats-officedocument.presentationml.presentation" : "application/json" }, body: reader.result }); if (!response.ok) throw Error((await response.text()) || ("请求失败（" + response.status + "）")); const value = await response.json(); if (!value || !Array.isArray(value.slides) || !value.slides.length) throw Error("文件不是有效的 PPT 数据"); setDeck(normalizeDeck(value)); resetHistory(); setSlide(0); setSelected(null); setEditing(null); setTopic(value.title || "产品介绍"); } catch (e) { alert("打开 PPT 文件失败：" + e.message); } };
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
      if (d) {
        d.current = { x: d.ex + dx, y: d.ey + dy };
        const node = c.querySelector(`[data-ppt-id="${CSS.escape(d.id)}"]`);
        if (node) { node.style.left = d.current.x / W * 100 + "%"; node.style.top = d.current.y / H * 100 + "%"; }
      } else {
        let x = r.ex, y = r.ey, w = r.ew, h = r.eh;
        if (r.dir.includes("e")) w = Math.max(40, r.ew + dx);
        if (r.dir.includes("s")) h = Math.max(30, r.eh + dy);
        if (r.dir.includes("w")) {
          w = Math.max(40, r.ew - dx);
          x = r.ex + dx
        } if (r.dir.includes("n")) {
          h = Math.max(30, r.eh - dy);
          y = r.ey + dy
        }
        r.current = { x, y, width: w, height: h };
        const node = c.querySelector(`[data-ppt-id="${CSS.escape(r.id)}"]`);
        if (node) { node.style.left = x / W * 100 + "%"; node.style.top = y / H * 100 + "%"; node.style.width = w / W * 100 + "%"; node.style.height = h / H * 100 + "%"; }
      }
    };
    useEffect(() => {
      const up = () => {
        const operation = drag.current || resize.current;
        if (operation?.before && operation.current) setDeck(d => {
          const n = structuredClone(d), target = (n.slides?.[slide]?.elements || []).find(e => e.id === operation.id);
          if (!target) return d;
          Object.assign(target, operation.current);
          const bucket = historyBucket(operation.before, 0);
          bucket.past.push(structuredClone(operation.before.slides[0]));
          if (bucket.past.length > 100) bucket.past.shift();
          bucket.future.length = 0;
          return n;
        });
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
    const changeMaster = fn => setDeck(d => { const n = structuredClone(d); n.master = n.master || {}; fn(n.master); return applyMasterToSlides(n); });
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
    return h(React.Fragment, null, h("header", null, h("button", { className: "btn small", onClick: () => location.href = "/" }, icon("arrow-left"), h("span", null, "返回首页")), h("b", null, "快马 AI · PPT 工作台")), h("div", { className: "bar" }, h("input", { value: topic, onChange: e => setTopic(e.target.value), placeholder: "主题" }), h("input", { value: theme, onChange: e => setTheme(e.target.value), placeholder: "风格" }), h("button", { onClick: openDeck }, icon("folder-open"), h("span", null, "打开 PPT 文件")), h("button", { onClick: generate, disabled: busy }, icon("sparkles"), h("span", null, busy ? "生成中…" : "新建 PPT")), h("button", { onClick: exportDeck, disabled: !deck }, icon("download"), h("span", null, "导出 PPTX")), h("button", { onClick: () => setShowJson(true), disabled: !current }, icon("code-2"), h("span", null, "对象 JSON"))), h("div", { className: "tools" }, ...tools.map(([t, i, l]) => h("button", { key: t, onClick: () => add(t) }, icon(i), h("span", null, l))), h("span", { className: "zoom-tools" }, h("button", { onClick: () => setZoom(z => Math.max(.25, +(z - .1).toFixed(2))), disabled: zoom <= .25 }, "−"), h("span", null, `${Math.round(zoom * 100)}%`), h("button", { onClick: () => setZoom(z => Math.min(2, +(z + .1).toFixed(2))), disabled: zoom >= 2 }, "+"), h("button", { onClick: () => setZoom(1) }, "重置")), h("button", { onClick: () => setInspectorOpen(v => !v) }, icon(inspectorOpen ? "panel-right-close" : "panel-right-open"), h("span", null, inspectorOpen ? "收起属性" : "展开属性")), h("button", {
      className: "danger", disabled: !current, onClick: removeSelected
    }, icon("trash-2"), h("span", null, "删除"))), h("main", { className: inspectorOpen ? "" : "inspector-collapsed" }, h("div", { id: "slides" }, (deck?.slides || []).map((s, i) => h("div", {
      key: s.id || i, className: "thumb " + (i === slide ? "active" : ""), draggable: true,
      onDragStart: ev => { slideDrag.current = i; ev.dataTransfer.effectAllowed = "move"; ev.currentTarget.classList.add("dragging"); },
      onDragEnd: ev => { slideDrag.current = null; ev.currentTarget.classList.remove("dragging"); },
      onDragOver: ev => { ev.preventDefault(); ev.dataTransfer.dropEffect = "move"; },
      onDrop: ev => { ev.preventDefault(); const from = slideDrag.current; if (from == null || from === i) return; const target = from < i ? i - 1 : i; trackedSetDeck(d => { const n = structuredClone(d), moved = n.slides.splice(from, 1)[0]; n.slides.splice(i, 0, moved); return n; }); setSlide(target); slideDrag.current = null; }, onClick: () => {
        if (masterMode) toggleMasterMode();
        setSlide(i);
        setSelected(null)
      }
      }, h("div", { className: "thumb-preview" }, [...(s.elements || [])].sort((a,b) => (Number(a.zIndex)||0) - (Number(b.zIndex)||0)).map(e => { const ts = { ...defaults, ...(e.style || {}), ...visualStyle(e, e.style || {}) }; return h("div", { key: e.id, className: "thumb-el", style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: ts.opacity ?? e.opacity ?? 1, ...fillStyle(ts.background || (e.type === "shape" ? "#dbeafe" : "transparent")), ...shapeStyle(ts.shape, ts, e.width * .18, e.height * .18), color: ts.color || "#172033", textAlign: textAlign(ts.align), fontSize: Math.max(8, (Number(ts.fontSize) || 24) * 0.18), lineHeight: ts.lineHeight || undefined, letterSpacing: ts.letterSpacing ? (Number(ts.letterSpacing) * .18) + "px" : undefined, fontWeight: ts.bold ? "bold" : "normal", fontStyle: ts.italic ? "italic" : "normal", textDecoration: ts.underline ? "underline" : "none", transform: elementTransform(e), transformOrigin: "center", ...boxStyle(ts) } }, e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, style: ts }) : e.type === "image" ? icon("image") : textRuns(e, ts, .18)); })), h("span", { className: "thumb-label" }, `第${i + 1}页`)))), h("div", { className: "canvas-wrap", style: { width: W * zoom + "px", height: H * zoom + "px" } }, h("div", { className: "canvas", style: { width: W + "px", height: H + "px", transform: `scale(${zoom})` }, onClick: () => setSelected(null) }, [...elements].sort((a,b) => (Number(a.zIndex)||0) - (Number(b.zIndex)||0)).map(e => {
      const st = { ...defaults, ...e.style, ...visualStyle(e, e.style || {}) }, bold = st.bold || Number(st.fontWeight) >= 600, isEditing = editing === e.id;
      const geometryOnly = e.geometry?.type === "custom" || e.geometry?.type === "line";
      return h("div", {
        key: e.id, "data-ppt-id": e.id, className: "el " + (e.type === "image" ? "image-el " : "") + (e.id === selected ? "sel" : "") + (isEditing ? " editing" : ""), onClick: ev => {
          ev.stopPropagation();
          setSelected(e.id)
        }, onDoubleClick: ev => {
          ev.stopPropagation();
          if (e.type === "image") { imageTarget.current = e.id; fileInput.current?.click() }
          else if (e.type === "text") setEditing(e.id)
        }, onMouseDown: ev => {
          if (ev.button === 0 && !e.locked && !isEditing) {
            setSelected(e.id);
            drag.current = { id: e.id, x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, canvas: ev.currentTarget.parentElement, before: { slides: [{ id: deck.slides[slide].id, elements: structuredClone(deck.slides[slide].elements) }] } }
          }
        }, style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: st.opacity ?? e.opacity ?? 1, fontSize: st.fontSize, fontFamily: st.fontFamily, textAlign: textAlign(st.align), lineHeight: st.lineHeight || undefined, letterSpacing: st.letterSpacing ? st.letterSpacing + "px" : undefined, fontWeight: bold ? "bold" : "normal", fontStyle: st.italic ? "italic" : "normal", textDecoration: st.underline ? "underline" : "none", color: st.color, zIndex: Number(e.zIndex) || 0, transform: elementTransform(e), transformOrigin: "center" }
      }, h("div", { className: "el-content", style: { ...(geometryOnly ? {} : fillStyle(st.background || e.fill?.image?.assetUrl)), ...shapeStyle(st.shape, st, e.width, e.height), ...(geometryOnly ? {} : boxStyle(st)) } }, geometryOnly ? null : imageFill(st.background || e.fill?.image?.assetUrl), customPath(e, st), e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, alt: "图片", style: st }) : e.type === "image" ? h("div", { className: "image-placeholder" }, icon("image")) : e.type === "text" && isEditing ? h("div", { ref: editRef, className: "inline-editor", contentEditable: true, suppressContentEditableWarning: true, onBlur: ev => { const node = ev.currentTarget; const runs = readEditableRuns(node); update(x => { x.textRuns = runs.length ? runs : [{ content: node?.textContent || "", style: { ...x.style } }]; x.content = runs.map((r, i) => (i && r.paragraph !== runs[i - 1].paragraph ? "\n" : "") + r.content).join(""); }, e.id); setEditing(null) }, onKeyDown: ev => { if (ev.key === "Escape") { ev.preventDefault(); setEditing(null) } } }, textRuns(e, st)) : textRuns(e, st)), e.id === selected && !isEditing && ["nw", "ne", "sw", "se"].map(dir => h("span", {
        key: dir, className: "handle " + dir, onMouseDown: ev => {
          ev.stopPropagation();
          resize.current = { id: e.id, x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, ew: e.width, eh: e.height, dir, canvas: ev.currentTarget.parentElement.parentElement, before: { slides: [{ id: deck.slides[slide].id, elements: structuredClone(deck.slides[slide].elements) }] } }
        }
      })))
    }))), h("input", {
      ref: fileInput, type: "file", accept: "image/*", style: { display: "none" }, onChange: ev => {
        const f = ev.target.files?.[0];
        if (f) {
          const r = new FileReader();
          r.onload = () => { if (backgroundTarget.current === "master") { setDeck(d => { const n = structuredClone(d); n.master = n.master || {}; n.master.background = r.result; n.theme = { ...n.theme, background: r.result }; return n }); backgroundTarget.current = false; } else if (backgroundTarget.current) { const id = backgroundTarget.current; update(e => { e.style = { ...e.style, background: r.result }; e.fill = { type: "image", image: { assetUrl: r.result, rotateWithShape: true } } }, id); setSelected(id); backgroundTarget.current = false; } else if (imageTarget.current) { const id = imageTarget.current; update(e => { e.assetUrl = r.result }, id); setSelected(id); imageTarget.current = null } else insertImage(r.result) };
          r.readAsDataURL(f)
        } ev.target.value = ""
      }
    }), h("input", { ref: openFileInput, type: "file", accept: ".json,application/json", style: { display: "none" }, onChange: loadDeckFile }), inspectorOpen && h(Inspector, { element: current, onChange: update, onLayer: changeLayer, master: masterMode ? (deck?.master || {}) : null, onMasterChange: changeMaster, onPickBackground: () => { backgroundTarget.current = "master"; fileInput.current?.click() }, onPickElementBackground: id => { backgroundTarget.current = id; fileInput.current?.click() }, themeColors: [{ label: "主题", value: deck?.master?.accent || deck?.theme?.accent }, { label: "前景", value: deck?.master?.foreground || deck?.theme?.foreground }, { label: "背景", value: deck?.master?.background || deck?.theme?.background }], onReplaceImage: id => { imageTarget.current = id; fileInput.current?.click() } }), showJson && h("div", { className: "json-overlay", onClick: () => setShowJson(false) }, h("div", { className: "json-panel", onClick: e => e.stopPropagation() }, h("div", { className: "json-head" }, h("b", null, current ? `对象 JSON · ${names[current.type] || current.type}` : "对象 JSON"), h("button", { onClick: () => setShowJson(false) }, "关闭")), current ? h("textarea", { className: "json-editor", value: jsonText, onChange: e => setJsonText(e.target.value), spellCheck: false }) : h("div", { className: "json-empty" }, "请先选择一个对象"), current && h("div", { className: "json-actions" }, h("button", { className: "btn primary", onClick: () => { try { const parsed = JSON.parse(jsonText); update(x => { Object.keys(x).forEach(k => delete x[k]); Object.assign(x, parsed); }); setShowJson(false); } catch (e) { alert("JSON 格式错误：" + e.message); } } }, "应用修改"), h("button", { onClick: () => setJsonText(JSON.stringify(jsonSnapshot || {}, null, 2)) }, "还原"))))))
  }
  ReactDOM.createRoot(document.getElementById("app")).render(h(App));
})();
