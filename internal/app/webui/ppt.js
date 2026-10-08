(() => {
  "use strict";
  const moveStreamStatusToCanvas = () => { const status = document.querySelector("body > .ppt-stream-status"), canvas = document.querySelector(".canvas-wrap"); if (status && canvas) canvas.appendChild(status); };
  const moveStreamStatusOnce = () => { moveStreamStatusToCanvas(); if (document.querySelector("body > .ppt-stream-status")) requestAnimationFrame(moveStreamStatusOnce); };
  requestAnimationFrame(moveStreamStatusOnce);
  const { useState, useEffect, useRef } = React, h = React.createElement;
  const { names, defaults, boxStyle, fillStyle, imageFill, shapeStyle, visualStyle, normalizeDeck, textAlign, verticalAlign, textRuns, readEditableRuns, customPath, ImageBlock, elementTransform, api, readSSE, icon, Inspector } = window.PPTEditor;
  const fallbackFonts = ["Microsoft YaHei", "SimSun", "SimHei", "Arial", "Calibri", "Aptos", "Times New Roman"];
  const presentationFonts = deck => {
    const found = new Set(), add = value => { if (typeof value === "string") value.split(",").map(item => item.trim()).filter(Boolean).forEach(item => found.add(item)); };
    (deck?.theme?.fonts || []).forEach(add); (deck?.master?.fonts || []).forEach(add); add(deck?.master?.fontFamily);
    (deck?.fontAssets || []).forEach(font => add(font?.family));
    const elements = [...(deck?.master?.elements || []), ...(deck?.slides || []).flatMap(slide => slide.elements || [])];
    elements.forEach(element => { add(element?.style?.fontFamily); add(element?.textStyle?.fontFamily); (element?.textRuns || []).forEach(run => add(run?.style?.fontFamily)); });
    return [...found];
  };
  const pptReferences = () => ({ text: document.querySelector("#pptReferenceText")?.value || "", images: [...(document.querySelector("#pptReferenceImages")?.files || [])], deck: document.querySelector("#pptReferenceDeck")?.files?.[0] || null });
  const referenceStatus = () => { const r = pptReferences(), el = document.querySelector("#pptReferenceStatus"); if (el) el.textContent = [r.images.length ? `${r.images.length} 张图片` : "", r.deck ? r.deck.name : ""].filter(Boolean).join(" · "); };
  document.querySelector("#pptReferenceImages")?.addEventListener("change", referenceStatus);
  document.querySelector("#pptReferenceDeck")?.addEventListener("change", referenceStatus);
  setTimeout(() => [...document.querySelectorAll("button")].forEach(btn => { if (btn.textContent.includes("生成草稿")) btn.childNodes.forEach(n => { if (n.nodeType === 3 && n.textContent.includes("生成草稿")) n.textContent = n.textContent.replace("生成草稿", "生成 PPT"); }); }), 0);
  function App() {
    useEffect(() => { const button = document.querySelector("#pptAiOpen"), bar = document.querySelector(".bar"), entry = document.querySelector(".ppt-ai-entry"); if (button && bar) { bar.appendChild(button); if (entry) entry.remove();[...bar.querySelectorAll("button")].forEach(node => { if (node !== button && node.textContent.includes("AI 制作 PPT")) { const text = node.querySelector("span"); if (text) text.textContent = "新建 PPT"; } }); } }, []);
    const [masterMode, setMasterMode] = useState(false), [inspectorOpen, setInspectorOpen] = useState(true);
    useEffect(() => { const button = document.querySelector("#pptInspectorReopen"), panel = document.querySelector(".side"), main = document.querySelector("main"); if (!button) return; button.hidden = false; button.setAttribute("aria-expanded", String(inspectorOpen)); button.title = inspectorOpen ? "收起属性" : "展开属性"; button.onclick = () => setInspectorOpen(v => !v); const place = () => { const rect = panel?.getBoundingClientRect(); if (rect) { button.style.left = `${Math.max(4, rect.left - button.offsetWidth)}px`; button.style.top = `${rect.top + 10}px`; button.style.right = "auto"; } }; place(); addEventListener("resize", place); main?.addEventListener("transitionend", place); return () => { removeEventListener("resize", place); main?.removeEventListener("transitionend", place); }; }, [inspectorOpen]);
    useEffect(() => { const toggle = () => setInspectorOpen(v => !v); addEventListener("ppt-toggle-inspector", toggle); return () => removeEventListener("ppt-toggle-inspector", toggle); }, []);
    const [localFonts, setLocalFonts] = useState(fallbackFonts);
    const [deck, setDeck] = useState(null), [slide, setSlide] = useState(0), [selected, setSelected] = useState(null), [editing, setEditing] = useState(null), [topic, setTopic] = useState("产品介绍"), [theme, setTheme] = useState(""), [audience, setAudience] = useState(""), [references, setReferences] = useState(""), [images, setImages] = useState(""), [plan, setPlan] = useState(null), [busy, setBusy] = useState(false), [ocrBusy, setOcrBusy] = useState(false), [ocrTarget, setOcrTarget] = useState(null), [showJson, setShowJson] = useState(false), [showPageJson, setShowPageJson] = useState(false), [jsonText, setJsonText] = useState(""), [zoom, setZoom] = useState(1), [ocrMode, setOcrMode] = useState(false), [ocrRect, setOcrRect] = useState(null), [ocrProvider, setOcrProvider] = useState("local:PaddleOCR"), drag = useRef(null), resize = useRef(null), slideDrag = useRef(null), fileInput = useRef(null), openFileInput = useRef(null), imageTarget = useRef(null), backgroundTarget = useRef(false), editRef = useRef(null), history = useRef({ past: new Map(), future: new Map() }), wheelLock = useRef(false), elements = (masterMode ? deck?.master?.elements : deck?.slides?.[slide]?.elements) || [], current = elements.find(e => e.id === selected);
    useEffect(() => { setInspectorOpen(!!selected); }, [selected]);
    useEffect(() => { document.body.classList.toggle("ppt-ocr-busy", ocrBusy); document.body.classList.toggle("ppt-ocr-mode", ocrMode && !ocrBusy); return () => { document.body.classList.remove("ppt-ocr-busy", "ppt-ocr-mode"); }; }, [ocrBusy, ocrMode]);
    useEffect(() => {
      let cancelled = false;
      if (typeof window.queryLocalFonts !== "function") return undefined;
      window.queryLocalFonts().then(fonts => {
        if (!cancelled) setLocalFonts([...new Set([...fallbackFonts, ...fonts.map(font => font.family).filter(Boolean)])]);
      }).catch(() => { });
      return () => { cancelled = true; };
    }, [ocrBusy, ocrMode]);
    const fontFamilies = [...new Set([...localFonts, ...presentationFonts(deck)])].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
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
    // All consumers capture the same editor canvas after committing the requested page.
    const pageSnapshot = async (index = slide, value = deck) => {
      if (!value?.slides?.[index]) throw new Error("页面不存在");
      ReactDOM.flushSync(() => { setDeck(value); setSlide(index); setMasterMode(false); setSelected(null); setEditing(null); });
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      await document.fonts.ready;
      const source = document.querySelector(".canvas");
      if (!source) throw new Error("页面 Canvas 尚未就绪");
      await Promise.all([...source.querySelectorAll("img")].map(img => img.decode()));
      const clone = source.cloneNode(true), originals = [source, ...source.querySelectorAll("*")], copies = [clone, ...clone.querySelectorAll("*")];
      const inline = async url => {
        if (url.startsWith("data:")) return url;
        const response = await fetch(url);
        if (!response.ok) throw new Error("页面图片读取失败");
        const blob = await response.blob();
        return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.onerror = reject; reader.readAsDataURL(blob); });
      };
      await Promise.all(copies.map(async (node, i) => {
        const computed = getComputedStyle(originals[i]);
        for (const property of computed) node.style.setProperty(property, computed.getPropertyValue(property));
        if (node.tagName === "IMG") node.src = await inline(originals[i].currentSrc || originals[i].src);
        const background = computed.backgroundImage;
        const urls = [...background.matchAll(/url\(["']?(.*?)["']?\)/g)];
        let next = background;
        for (const match of urls) next = next.replace(match[0], `url("${await inline(match[1])}")`);
        node.style.backgroundImage = next;
      }));
      clone.querySelectorAll(".handles, .resize-handle, .selection-box, .ocr-selection").forEach(node => node.remove());
      clone.querySelectorAll(".selected").forEach(node => { node.style.outline = "none"; });
      const width = Number(value.width) || 1280, height = Number(value.height) || 720;
      Object.assign(clone.style, { transform: "none", width: width + "px", height: height + "px", margin: "0", border: "none", boxShadow: "none" });
      const fonts = (value.fontAssets || []).filter(font => font.family && font.assetUrl).map(font => `@font-face{font-family:${JSON.stringify(font.family)};src:url("${font.assetUrl}");font-weight:${font.weight || 400};font-style:${font.style || "normal"};}`).join("");
      const root = document.createElement("div"); root.setAttribute("xmlns", "http://www.w3.org/1999/xhtml");
      const style = document.createElement("style"); style.textContent = fonts; root.append(style, clone);
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><foreignObject width="100%" height="100%">${new XMLSerializer().serializeToString(root)}</foreignObject></svg>`;
      return new Promise((resolve, reject) => {
        const image = new Image();
        image.onload = () => { try { const canvas = document.createElement("canvas"); canvas.width = width; canvas.height = height; canvas.getContext("2d").drawImage(image, 0, 0); resolve(canvas.toDataURL("image/png")); } catch (error) { reject(error); } };
        image.onerror = () => reject(new Error("页面快照生成失败"));
        image.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
      });
    };
    useEffect(() => {
      window.PPTPageSnapshot = pageSnapshot;
      window.PPTRenderDeckSnapshots = async value => {
        const normalized = normalizeDeck(value), shots = [];
        for (let index = 0; index < normalized.slides.length; index++) shots.push(await pageSnapshot(index, normalized));
        ReactDOM.flushSync(() => setSlide(0));
        return shots;
      };
    }, [deck, slide]);
    useEffect(() => { const onGenerated = ev => { if (!ev.detail) return; setDeck(normalizeDeck(ev.detail)); resetHistory(); setSlide(0); setSelected(null); }; const onPage = ev => setDeck(d => { if (!d) return d; const detail = ev.detail || {}, n = structuredClone(d), at = Math.max(0, Math.min(Number.isInteger(detail.index) ? detail.index : slide, n.slides.length - 1)); if ((detail.action || detail) === "add") n.slides.splice(at + 1, 0, { id: "slide-" + Date.now(), layout: "content", elements: [] }); if ((ev.detail?.action || ev.detail) === "duplicate") n.slides.splice(at + 1, 0, structuredClone({ ...n.slides[at], id: "slide-" + Date.now() })); if ((ev.detail?.action || ev.detail) === "delete" && n.slides.length > 1) n.slides.splice(at, 1); return n; }); const onImagePage = async ev => { const index = Number(ev.detail); if (!deck || !Number.isInteger(index) || index < 0 || index >= deck.slides.length) return; setBusy(true); try { const r = await api("/api/ppt/images", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ presentation: deck, slideIndex: index }) }); const out = await r.json(); if (out.presentation) { const bucket = historyBucket(deck, index); bucket.past.push(structuredClone(deck.slides[index])); bucket.future.length = 0; setDeck(normalizeDeck(out.presentation)); setSlide(index); } if (out.errors?.length) alert("配图生成失败：" + out.errors.join("\n")); } catch (e) { alert("配图生成失败，文字内容已保留：" + e.message); } finally { setBusy(false); } }; const onAIPage = async ev => { const detail = ev.detail || {}, index = Number(detail.index ?? detail); if (!deck || !Number.isInteger(index) || index < 0 || index >= deck.slides.length) return; setBusy(true); const status = document.createElement("div"); status.className = "ppt-stream-status ppt-regenerate-blocker"; status.textContent = "AI 正在用生图模型重绘当前页…"; (document.querySelector(".canvas-wrap") || document.body).appendChild(status); document.body.classList.add("ppt-regenerating"); try { const r = await api("/api/ppt/slide?stream=1", { method: "POST", headers: { "Content-Type": "application/json", "Accept": "text/event-stream" }, body: JSON.stringify({ presentation: deck, slideIndex: index, prompt: detail.prompt || "", referenceImage: detail.referenceImage || "", imageModel: detail.imageModel || "", pageImage: detail.pageImage || await pageSnapshot(index) }) }); const out = await readSSE(r); if (!out || !out.slide) throw new Error("未返回生成页面数据"); setDeck(d => { const n = structuredClone(d), bucket = historyBucket(d, index); bucket.past.push(structuredClone(d.slides[index])); bucket.future.length = 0; n.slides[index] = { ...out.slide, id: d.slides[index].id || out.slide.id || "slide-" + index }; return n; }); setSlide(index); setSelected(null); detail.redesignOverlay?.remove(); } catch (e) { alert("AI 生成页面失败：" + e.message); } finally { if (status.parentNode) status.parentNode.removeChild(status); document.body.classList.remove("ppt-regenerating"); setBusy(false); } }; addEventListener("ppt:generated", onGenerated); addEventListener("ppt:page", onPage); addEventListener("ppt:image-page", onImagePage); addEventListener("ppt:ai-page", onAIPage); return () => { removeEventListener("ppt:generated", onGenerated); removeEventListener("ppt:page", onPage); removeEventListener("ppt:image-page", onImagePage); removeEventListener("ppt:ai-page", onAIPage); }; }, [slide, deck]);
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
    const ocrCache = useRef(new Map());
    const imageAtPoint = (x, y) => (deck?.slides?.[slide]?.elements || []).filter(e => e.type === "image" && e.assetUrl && x >= e.x && x <= e.x + e.width && y >= e.y && y <= e.y + e.height).sort((a, b) => (Number(b.zIndex) || 0) - (Number(a.zIndex) || 0))[0] || null;
    const ocrKey = image => {
      const src = String(image?.assetUrl || "");
      return `${slideKey(deck, slide)}|${image?.id || "image"}|${ocrProvider}|${src.length}:${src.slice(0, 24)}:${src.slice(-24)}`;
    };
    const cachedOCRAtPoint = (image, x, y) => {
      const entry = image ? ocrCache.current.get(ocrKey(image)) : null;
      const item = (entry?.items || []).filter(e => x >= e.x && x <= e.x + e.width && y >= e.y && y <= e.y + e.height)
        .sort((a, b) => (a.width * a.height) - (b.width * b.height))[0] || null;
      return { scanned: !!entry?.scanned, item };
    };
    const materializeOCR = item => {
      if (!item) return false;
      setDeck(d => {
        const n = structuredClone(d), list = n.slides[slide].elements || (n.slides[slide].elements = []);
        if (!list.some(e => e.id === item.id)) list.push(structuredClone(item));
        return n;
      });
      setSelected(item.id); setEditing(item.id);
      return true;
    };
    const runOCR = async (rect, image) => {
      if (!deck || !rect || !image) return;
      setBusy(true); setOcrBusy(true); setOcrTarget({ x: image.x, y: image.y, width: image.width, height: image.height, rotation: image.rotation || 0 }); setOcrRect(null);
      try {
        let imageData = image?.assetUrl || "";
        // Data URLs are already inline. Generated images may be served from a
        // short URL, so materialize the actual bytes before sending OCR.
        if (imageData && !imageData.startsWith("data:image/")) {
          const response = await fetch(imageData);
          if (!response.ok) throw new Error(`图片读取失败（HTTP ${response.status}）`);
          const blob = await response.blob();
          imageData = await new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.onerror = reject; reader.readAsDataURL(blob); });
        }
        const localRect = { ...rect, x: rect.x - Number(image.x || 0), y: rect.y - Number(image.y || 0), asset: imageData, cropWidth: image.width, cropHeight: image.height, imageOffsetX: image.x || 0, imageOffsetY: image.y || 0, scanAll: true };
        const deckForOCR = structuredClone(deck);
        if (imageData.startsWith("data:image/")) (deckForOCR.slides || []).forEach(s => (s.elements || []).forEach(e => { if (e.type === "image") e.assetUrl = ""; }));
        const payload = { presentation: deckForOCR, slideIndex: slide, provider: ocrProvider, imageData, ...localRect };
        console.info("[ppt-ocr] request", { provider: ocrProvider, slideIndex: slide, rect, bytes: new Blob([JSON.stringify(payload)]).size });
        const r = await fetch("/api/ppt/ocr", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
        const out = await r.json().catch(() => ({}));
        console.info("[ppt-ocr] response", { status: r.status, ok: r.ok, body: out });
        if (!r.ok) throw new Error(out.error || `OCR 请求失败（HTTP ${r.status}）`);
        const items = (out.elements || []).filter(e => e?.type === "text");
        ocrCache.current.set(ocrKey(image), { scanned: true, items });
        const hit = items.filter(e => rect.x >= e.x && rect.x <= e.x + e.width && rect.y >= e.y && rect.y <= e.y + e.height).sort((a, b) => (a.width * a.height) - (b.width * b.height))[0];
        materializeOCR(hit);
      } catch (e) { alert("OCR 失败：" + e.message); } finally { setOcrBusy(false); setOcrTarget(null); setBusy(false); }
    };
    const convertImageToEditable = async image => {
      if (!deck || !image) return;
      if (!window.confirm("此操作会替换原图片并自动修复文字背景，原图片可能无法恢复。建议先备份 PPT，是否继续？")) return;
      setBusy(true); setOcrBusy(true); setOcrTarget({ x: image.x, y: image.y, width: image.width, height: image.height, rotation: image.rotation || 0 });
      try {
        let workingDeck = structuredClone(deck);
        for (let pageIndex = slide; pageIndex < (workingDeck.slides || []).length; pageIndex++) {
          const page = workingDeck.slides[pageIndex];
          const pageImages = (page.elements || []).filter(e => e?.type === "image");
          if (pageIndex !== slide && (pageImages.length !== 1 || (page.elements || []).length !== 1)) break;
          const currentImage = pageImages[0];
          setSlide(pageIndex); setSelected(null); setEditing(null);
          setOcrTarget({ x: currentImage.x, y: currentImage.y, width: currentImage.width, height: currentImage.height, rotation: currentImage.rotation || 0 });
          let imageData = currentImage.assetUrl || "";
          if (imageData && !imageData.startsWith("data:image/")) { const response = await fetch(imageData); if (!response.ok) throw new Error(`图片读取失败（HTTP ${response.status}）`); const blob = await response.blob(); imageData = await new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.onerror = reject; reader.readAsDataURL(blob); }); }
          const deckForOCR = structuredClone(workingDeck);
          (deckForOCR.slides || []).forEach(s => (s.elements || []).forEach(e => { if (e.type === "image") e.assetUrl = ""; }));
          const payload = { presentation: deckForOCR, slideIndex: pageIndex, provider: ocrProvider, imageData, asset: imageData, x: 0, y: 0, width: currentImage.width, height: currentImage.height, cropWidth: currentImage.width, cropHeight: currentImage.height, imageOffsetX: currentImage.x || 0, imageOffsetY: currentImage.y || 0, scanAll: true, convertAll: true, repairBackground: true };
          const r = await fetch("/api/ppt/ocr", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) }); const out = await r.json().catch(() => ({})); if (!r.ok) throw new Error(out.error || `OCR 请求失败（HTTP ${r.status}）`); if (!out.presentation) throw new Error("OCR 未返回转换后的页面");
          workingDeck.slides[pageIndex] = structuredClone(out.presentation.slides?.[pageIndex] || page);
          setDeck(normalizeDeck(structuredClone(workingDeck)));
        }
        setDeck(normalizeDeck(workingDeck)); setSelected(null); setEditing(null);
      } catch (e) { alert("转换失败：" + e.message); } finally { setOcrBusy(false); setOcrTarget(null); setBusy(false); }
    };
    const ocrAtPoint = ev => {
      const c = ev.target?.closest?.('.canvas'); if (!c) return false;
      ev.preventDefault(); ev.stopPropagation();
      const q = c.getBoundingClientRect();
      const x = (ev.clientX - q.left) * W / q.width, y = (ev.clientY - q.top) * H / q.height;
      const image = imageAtPoint(x, y); if (!image) return false;
      const cached = cachedOCRAtPoint(image, x, y);
      if (cached.scanned) {
        materializeOCR(cached.item);
      } else runOCR({ x, y, width: 0, height: 0 }, image);
      return true;
    };
    const editImage = image => {
      if (image?.assetUrl && window.KuaimaFineEditor) {
        window.KuaimaFineEditor.open(image.assetUrl, { onApply: next => update(e => { e.assetUrl = next; }, image.id) });
      }
    };
    useEffect(() => {
      if (!deck || !ocrMode) return;
      const pick = ev => {
        const el = ev.target?.closest?.('.el');
        if (el && !el.classList.contains('image-el')) return;
        const c = ev.target?.closest?.('.canvas'); if (!c) return;
        const q = c.getBoundingClientRect(), x = (ev.clientX - q.left) * W / q.width, y = (ev.clientY - q.top) * H / q.height;
        ocrAtPoint(ev);
      };
      document.addEventListener('dblclick', pick, true);
      return () => document.removeEventListener('dblclick', pick, true);
    }, [deck, slide, ocrProvider, ocrMode]);
    useEffect(() => {
      document.querySelectorAll('.ocr-selection').forEach(n => n.remove());
      if (!ocrMode || !ocrRect) return;
      const c = document.querySelector('.canvas'); if (!c) return;
      const n = document.createElement('div'); n.className = 'ocr-selection'; Object.assign(n.style, { left: `${ocrRect.x}px`, top: `${ocrRect.y}px`, width: `${ocrRect.width}px`, height: `${ocrRect.height}px` }); c.appendChild(n);
      return () => n.remove();
    }, [ocrMode, ocrRect, slide]);
    useEffect(() => {
      const tools = document.querySelector('.tools'); if (!tools) return;
      const wrap = document.createElement('label'); wrap.className = 'ocr-engine-picker'; wrap.title = 'OCR 引擎';
      const select = document.createElement('select'); select.setAttribute('aria-label', 'OCR 引擎');
      select.innerHTML = '<option disabled>正在读取本地模型…</option>';
      wrap.appendChild(select); wrap.hidden = true; const ocrButton = tools.querySelector('.ocr-tool-button'); if (ocrButton) { ocrButton.insertAdjacentElement('afterend', wrap); wrap.style.left = `${ocrButton.offsetLeft}px`; ocrButton.addEventListener('click', () => { wrap.hidden = !wrap.hidden; }); } else tools.insertBefore(wrap, tools.firstChild);
      fetch('/api/ppt/ocr/models').then(r => r.json()).then(out => {
        select.innerHTML = '';
        (out.models || []).forEach(m => {
          const o = document.createElement('option'); o.value = 'local:' + m.id;
          o.textContent = '本地 PaddleOCR · ' + m.name;
          if (m.available === false) { o.disabled = true; o.textContent += '（本地推理暂不可用）'; }
          select.appendChild(o);
        });
        // Keep the paid/remote provider as the final option in the list.
        const remote = document.createElement('option'); remote.value = 'remote'; remote.textContent = '远程 OCR（可能收费）'; select.appendChild(remote);
        const local = [...select.options].find(option => option.value.startsWith('local:') && !option.disabled);
        select.value = local ? local.value : 'local:PaddleOCR';
        if (local) setOcrProvider(local.value);
      }).catch(() => { select.innerHTML = '<option value="local:PaddleOCR">本地 PaddleOCR（未检测到模型）</option><option value="remote">远程 OCR（可能收费）</option>'; select.value = 'local:PaddleOCR'; });
      select.onchange = () => setOcrProvider(select.value);
      return () => wrap.remove();
    }, [current?.type]);
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
    // Keyboard and modifier-drag operations share the same element model as
    // the inspector, so they remain undoable and work on the master canvas.
    const duplicateSelected = source => {
      if (!source || source.locked || !deck) return null;
      const copyId = `${source.id}-copy-${Date.now()}`;
      const copy = structuredClone(source);
      copy.id = copyId;
      copy.x = Number(copy.x) + 10;
      copy.y = Number(copy.y) + 10;
      trackedSetDeck(d => {
        const n = structuredClone(d);
        const list = masterMode ? ((n.master || (n.master = {})).elements || (n.master.elements = [])) : (n.slides?.[slide]?.elements || []);
        const maxZ = list.reduce((max, item) => Math.max(max, Number(item.zIndex) || 0), -1);
        copy.zIndex = maxZ + 1;
        list.push(structuredClone(copy));
        if (masterMode) {
          (n.slides || []).forEach(page => {
            page.elements = [...(page.elements || []).filter(item => item.id !== copy.id), { ...structuredClone(copy), locked: true }];
          });
        }
        return n;
      });
      setSelected(copyId);
      return copyId;
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
    useEffect(() => { const onEditable = async ev => { const index = Number(ev.detail); if (!deck || !Number.isInteger(index)) return; setBusy(true); try { const r = await api("/api/ppt/image-to-editable", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ presentation: deck, slideIndex: index }) }); const out = await r.json(); if (!r.ok) throw new Error(out.error || "转换失败"); setDeck(normalizeDeck(out.presentation)); setSlide(index); } catch (e) { alert("转换失败：" + e.message); } finally { setBusy(false); } }; addEventListener("ppt:image-editable", onEditable); return () => removeEventListener("ppt:image-editable", onEditable); }, [deck]);
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
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode) { n.master = n.master || {}; const list = n.master.elements || []; e.zIndex = list.reduce((max, item) => Math.max(max, Number(item.zIndex) || 0), -1) + 1; const me = { ...e, id: "master-" + e.id, locked: false }; n.master.elements = [...list, me]; (n.slides || []).forEach(s => { s.elements = [...(s.elements || []).filter(x => x.id !== me.id), { ...me, locked: true }]; }); } else { const list = n.slides[slide].elements || []; e.zIndex = list.reduce((max, item) => Math.max(max, Number(item.zIndex) || 0), -1) + 1; list.push(e); } return n });
      setSelected(masterMode ? "master-" + e.id : e.id)
    };
    const add = async type => {
      if (!deck) return;
      if (type === "ocr") { if (ocrProvider === "remote" && !window.confirm("远程 OCR 可能产生模型调用费用，继续吗？")) return; setOcrMode(v => !v); setOcrRect(null); return; }
      if (type === "undo") { undo(); return; }
      if (type === "redo") { redo(); return; }
      if (type === "image") { imageTarget.current = null; fileInput.current?.click(); return }
      const e = { id: type + "-" + Date.now(), type, x: 120, y: 120, width: 300, height: 100, content: { text: "双击编辑", shape: "", table: "A | B\n1 | 2", chart: "数据图表" }[type], style: { ...defaults, background: type === "shape" ? "#dbeafe" : "transparent", ...(type === "shape" ? { shape: "rect" } : {}) }, ...(type === "text" ? { textRuns: [{ content: "双击编辑", style: { ...defaults } }] } : {}) };
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode) { n.master = n.master || {}; const list = n.master.elements || []; e.zIndex = list.reduce((max, item) => Math.max(max, Number(item.zIndex) || 0), -1) + 1; const me = { ...e, id: "master-" + e.id, locked: false }; n.master.elements = [...list, me]; (n.slides || []).forEach(s => { s.elements = [...(s.elements || []).filter(x => x.id !== me.id), { ...me, locked: true }]; }); } else { const list = n.slides[slide].elements || []; e.zIndex = list.reduce((max, item) => Math.max(max, Number(item.zIndex) || 0), -1) + 1; list.push(e); } return n });
      setSelected(masterMode ? "master-" + e.id : e.id);
      if (type === "image") fileInput.current?.click()
    };
    const removeSelected = () => {
      if (!selected || editing) return;
      if (!window.confirm("确定要删除当前选中的元素吗？删除后无法恢复。")) return;
      trackedSetDeck(d => { const n = structuredClone(d); if (masterMode && String(selected).startsWith("master-")) { n.master.elements = (n.master.elements || []).filter(e => e.id !== selected); (n.slides || []).forEach(s => { s.elements = (s.elements || []).filter(e => e.id !== selected); }); } else n.slides[slide].elements = n.slides[slide].elements.filter(e => e.id !== selected); return n });
      setSelected(null)
    };
    const openDeck = () => { if (openFileInput.current) { openFileInput.current.accept = ".json,.pptx,image/*"; openFileInput.current.multiple = true; openFileInput.current.click(); } };
    const loadDeckFile = ev => {
      const files = [...(ev.target.files || [])]; ev.target.value = "";
      if (!files.length) return;
      const imageFiles = files.filter(file => file.type.startsWith("image/") || /\.(png|jpe?g|webp|gif|bmp|svg)$/i.test(file.name));
      if (imageFiles.length === files.length) {
        Promise.all(imageFiles.map(file => new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve({ name: file.name, data: reader.result });
          reader.onerror = reject;
          reader.readAsDataURL(file);
        }))).then(items => {
          const width = 1280, height = 720;
          const value = {
            width, height, title: "图片演示文稿", theme: { background: "#ffffff", foreground: "#111827", accent: "#2563eb" }, master: { elements: [] }, slides: items.map((item, index) => ({
              id: "slide-" + (index + 1), layout: "image", elements: [{ id: "image-" + (index + 1), type: "image", x: 0, y: 0, width, height, content: "", assetUrl: item.data, style: { ...defaults } }]
            }))
          };
          setDeck(normalizeDeck(value)); resetHistory(); setSlide(0); setSelected(null); setEditing(null); setTopic("图片演示文稿");
        }).catch(e => alert("打开图片失败：" + e.message));
        return;
      }
      if (files.length > 1 || imageFiles.length) { alert("请一次选择一个 PPT/JSON 文件，或仅选择图片文件"); return; }
      const file = files[0];
      const reader = new FileReader();
      reader.onload = async () => { const button = document.querySelector('[data-ppt-action="import"]'); button?.classList.add("ppt-io-active"); try { const isPptx = file.name.toLowerCase().endsWith(".pptx"); const response = await fetch("/api/pptx/import", { method: "POST", headers: { "Content-Type": isPptx ? "application/vnd.openxmlformats-officedocument.presentationml.presentation" : "application/json" }, body: reader.result }); if (!response.ok) throw Error((await response.text()) || ("请求失败（" + response.status + "）")); const value = await response.json(); if (!value || !Array.isArray(value.slides) || !value.slides.length) throw Error("文件不是有效的 PPT 数据"); setDeck(normalizeDeck(value)); resetHistory(); setSlide(0); setSelected(null); setEditing(null); setTopic(value.title || "产品介绍"); } catch (e) { alert("打开 PPT 文件失败：" + e.message); } finally { button?.classList.remove("ppt-io-active"); } };
      reader.readAsArrayBuffer(file);
    };
    useEffect(() => {
      const onKeyDown = ev => {
        const target = ev.target;
        const tag = target?.tagName?.toLowerCase();
        if (editing || tag === "input" || tag === "textarea" || target?.isContentEditable) return;
        if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "z") { ev.preventDefault(); ev.shiftKey ? redo() : undo(); return; }
        if ((ev.ctrlKey || ev.metaKey) && (ev.key.toLowerCase() === "y" || (ev.key.toLowerCase() === "z" && ev.shiftKey))) { ev.preventDefault(); redo(); return; }
        if (!editing) {
          const key = ev.key;
          if (key === "Tab") {
            const ordered = [...elements].filter(item => masterMode || !item.locked).sort((a, b) => (Number(a.zIndex) || 0) - (Number(b.zIndex) || 0));
            if (ordered.length) {
              ev.preventDefault();
              const at = ordered.findIndex(item => item.id === selected);
              const next = at < 0 ? (ev.shiftKey ? ordered.length - 1 : 0) : (at + (ev.shiftKey ? -1 : 1) + ordered.length) % ordered.length;
              setSelected(ordered[next].id);
            }
            return;
          }
        }
        if (selected && !editing) {
          const key = ev.key;
          if (key === "PageUp" || key === "PageDown") {
            ev.preventDefault();
            if (key === "PageUp") changeLayer("up");
            else changeLayer("down");
            return;
          }
          if ((ev.ctrlKey || ev.metaKey) && (key === "ArrowUp" || key === "ArrowDown")) {
            ev.preventDefault();
            changeLayer(ev.shiftKey ? (key === "ArrowUp" ? "front" : "back") : (key === "ArrowUp" ? "up" : "down"));
            return;
          }
          // Ctrl/Cmd+Shift+[ / ] sends to back/front, respectively.
          if ((ev.ctrlKey || ev.metaKey) && ev.shiftKey && (key === "[" || key === "]" || key === "{" || key === "}")) {
            ev.preventDefault();
            changeLayer(key === "]" || key === "}" ? "front" : "back");
            return;
          }
          if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(key)) {
            ev.preventDefault();
            const step = ev.shiftKey ? 10 : 1;
            const dx = key === "ArrowLeft" ? -step : key === "ArrowRight" ? step : 0;
            const dy = key === "ArrowUp" ? -step : key === "ArrowDown" ? step : 0;
            update(item => { item.x = Number(item.x || 0) + dx; item.y = Number(item.y || 0) + dy; });
            return;
          }
        }
        if ((ev.key === "Delete" || ev.key === "Backspace") && selected) {
          ev.preventDefault();
          removeSelected();
        }
      };
      addEventListener("keydown", onKeyDown);
      return () => removeEventListener("keydown", onKeyDown)
    }, [selected, editing, slide, deck, elements, masterMode]);
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
          const n = structuredClone(d), target = (masterMode ? (n.master?.elements || []) : (n.slides?.[slide]?.elements || [])).find(e => e.id === operation.id);
          if (!target) return d;
          Object.assign(target, operation.current);
          if (!masterMode) {
            if (!operation.duplicated) {
              const bucket = historyBucket(operation.before, 0);
              bucket.past.push(structuredClone(operation.before.slides[0]));
              if (bucket.past.length > 100) bucket.past.shift();
              bucket.future.length = 0;
            }
          } else {
            const moved = (n.master?.elements || []).find(e => e.id === operation.id);
            (n.slides || []).forEach(page => { const item = (page.elements || []).find(e => e.id === operation.id); if (item && moved) Object.assign(item, moved); });
          }
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
      const button = document.querySelector('[data-ppt-action="export"]'); button?.classList.add("ppt-io-active");
      try {
        const r = await api("/api/pptx/export", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(deck) }), a = document.createElement("a");
        a.href = URL.createObjectURL(await r.blob());
        a.download = "presentation.pptx";
        a.click()
      } catch (e) { alert("导出失败：" + e.message) } finally { button?.classList.remove("ppt-io-active"); }
    };
    const slideImages = elements.filter(e => e.type === "image");
    const onlyImagePage = !masterMode && slideImages.length === 1 && elements.length === 1;
    const tools = [["undo", "undo-2", "撤销"], ["redo", "redo-2", "重做"], ["text", "type", "添加文本"], ["image", "image-plus", "添加图片"], ["shape", "shapes", "添加形状"]];
    if (current?.type === "image") tools.push(["ocr", "scan-text", "识别文字"]);
    if (onlyImagePage) tools.push(["ocr-convert", "text-cursor-input", "一键转可编辑文字"]); const pageAction = (action, index = slide) => { if (action !== "delete" || (deck?.slides || []).length > 1) { if (action !== "delete") setSlide(index + 1); window.dispatchEvent(new CustomEvent("ppt:page", { detail: { action, index } })); } };
    const openAIDialog = (index = slide) => {
      const overlay = document.createElement("div"); overlay.className = "ppt-redesign-overlay"; overlay.innerHTML = `<div class="ppt-redesign-dialog"><h3>AI 重新设计第 ${index + 1} 页</h3><div class="ppt-redesign-current-thumb"><span>当前页面</span><img data-current-thumb alt="当前页缩略图"></div><textarea placeholder="例如：改成更简洁的科技风，突出数据结论"></textarea><label>参考图片（可选）<input type="file" accept="image/*"></label><div data-reference-preview></div><details class="ppt-redesign-model"><summary>生图模型（可选）</summary><select data-image-model><option value="">使用默认模型</option></select></details><div class="ppt-redesign-actions"><button data-cancel>取消</button><button class="primary" data-submit>开始设计</button></div></div>`; document.body.appendChild(overlay); let snapshot = ""; const submit = overlay.querySelector("[data-submit]"); submit.disabled = true; pageSnapshot(index).then(src => { const img = overlay.querySelector("[data-current-thumb]"); if (img) img.src = src; snapshot = src; submit.disabled = false; }).catch(error => { overlay.querySelector("[data-current-thumb]").alt = error.message; });
      const saved = (() => { try { return JSON.parse(localStorage.getItem("ppt-redesign-preferences") || "{}"); } catch (_) { return {}; } })(); const textInput = overlay.querySelector("textarea"); textInput.value = saved.prompt || ""; const modelSelect = overlay.querySelector("[data-image-model]"); fetch("/api/ipricing").then(r => r.json()).then(d => { const models = d?.data?.img || d?.data?.image || []; modelSelect.innerHTML = '<option value="">使用默认模型</option>' + models.filter(x => x?.model_name).map(x => `<option value="${String(x.model_name).replace(/"/g, '&quot;')}">${x.model_name}</option>`).join(""); }).catch(() => {});
      const fileInput = overlay.querySelector("input[type=file]"), preview = overlay.querySelector("[data-reference-preview]"); fileInput.onchange = () => { const f = fileInput.files[0]; preview.innerHTML = f ? `<img class="ppt-reference-thumb" src="${URL.createObjectURL(f)}" alt="参考图片缩略图">` : ""; };
      const close = () => overlay.remove(); overlay.querySelector("[data-cancel]").onclick = close; overlay.onclick = e => { if (e.target === overlay) close() }; overlay.querySelector("[data-submit]").onclick = () => { const prompt = textInput.value.trim(); const imageModel = modelSelect.value; try { localStorage.setItem("ppt-redesign-preferences", JSON.stringify({ prompt, imageModel })); } catch (_) {} const file = fileInput.files[0]; const btn = overlay.querySelector("[data-submit]"); btn.disabled = true; btn.textContent = "正在设计…"; overlay.querySelector(".ppt-redesign-dialog").classList.add("is-generating"); const send = referenceImage => { window.dispatchEvent(new CustomEvent("ppt:ai-page", { detail: { index, prompt, referenceImage, imageModel, pageImage: snapshot, redesignOverlay: overlay } })); close(); }; if (file) { const reader = new FileReader(); reader.onload = () => send(reader.result); reader.readAsDataURL(file); } else send(""); };
    };
    return h(React.Fragment, null, h("header", null, h("a", { className: "brand", onClick: () => location.href = "/" }, h("span", { className: "ppt-brand-mark", title: "返回首页" }), h("span", { id: "pptAppVersion", className: "app-version" })), h("b", null, "快马AI · PPT工作台"), h("div", { className: "bar" }, h("button", { "data-ppt-action": "import", onClick: openDeck }, icon("folder-open"), h("span", null, "打开 PPT 文件")), h("button", { onClick: generate, disabled: busy }, icon("sparkles"), h("span", null, "新建PPT")), h("button", { "data-ppt-action": "export", onClick: exportDeck, disabled: !deck }, icon("download"), h("span", null, "导出 PPTX")), h("button", { onClick: () => setShowJson(true), disabled: !current }, icon("code-2"), h("span", null, "对象 JSON")))), h("div", { className: "tools" }, ...tools.map(([t, i, l]) => h("button", { key: t, className: t === "ocr" ? `ocr-tool-button${ocrMode ? " active" : ""}` : "", "aria-pressed": t === "ocr" ? String(ocrMode) : undefined, onClick: () => t === "ocr-convert" ? convertImageToEditable(slideImages[0]) : add(t) }, icon(i), h("span", null, l))), h("button", { className: "tool-ai-redesign", onClick: () => openAIDialog(slide), disabled: !deck, title: "AI重新设计当前页" }, icon("sparkles"), h("span", null, "AI重新生成")), h("span", { className: "zoom-tools" }, h("button", { onClick: () => setZoom(z => Math.max(.25, +(z - .1).toFixed(2))), disabled: zoom <= .25 }, "−"), h("span", null, `${Math.round(zoom * 100)}%`), h("button", { onClick: () => setZoom(z => Math.min(2, +(z + .1).toFixed(2))), disabled: zoom >= 2 }, "+"), h("button", { onClick: () => setZoom(1) }, "重置")), current && h("button", {
      className: "danger", onClick: removeSelected
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
    }, h("div", { className: "thumb-preview" }, [...(s.elements || [])].sort((a, b) => (Number(a.zIndex) || 0) - (Number(b.zIndex) || 0)).map(e => { const ts = { ...defaults, ...(e.style || {}), ...visualStyle(e, e.style || {}) }; const thumbAlign = textAlign(ts.align); return h("div", { key: e.id, className: "thumb-el", style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: ts.opacity ?? e.opacity ?? 1, ...fillStyle(ts.background || (e.type === "shape" ? "#dbeafe" : "transparent")), ...shapeStyle(ts.shape, ts, e.width * .18, e.height * .18), color: ts.color || "#172033", textAlign: thumbAlign, textAlignLast: thumbAlign === "justify" ? "justify" : undefined, fontSize: Math.max(8, (Number(ts.fontSize) || 24) * 0.18), lineHeight: ts.lineHeight || undefined, letterSpacing: ts.letterSpacing ? (Number(ts.letterSpacing) * .18) + "px" : undefined, fontWeight: ts.bold ? "bold" : "normal", fontStyle: ts.italic ? "italic" : "normal", textDecoration: ts.underline ? "underline" : "none", transform: elementTransform(e), transformOrigin: "center", ...boxStyle(ts) } }, e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, style: ts }) : e.type === "image" ? icon("image") : textRuns(e, ts, .18)); })), h("span", { className: "thumb-label" }, `第${i + 1}页`), h("span", { className: "thumb-actions" }, h("button", { onClick: ev => { ev.stopPropagation(); pageAction("add", i); }, title: "新建一页" }, icon("plus")), h("button", { onClick: ev => { ev.stopPropagation(); pageAction("duplicate", i); }, title: "复制当前页" }, icon("copy")), h("button", { onClick: ev => { ev.stopPropagation(); if (confirm("确定删除当前页吗？")) pageAction("delete", i); }, title: "删除当前页" }, icon("trash-2")), h("button", { onClick: ev => { ev.stopPropagation(); openAIDialog(i); }, title: "AI重新设计当前页" }, icon("sparkles")))))), h("div", { className: "canvas-wrap", style: { width: W * zoom + "px", height: H * zoom + "px" } }, h("div", { className: "canvas", style: { width: W + "px", height: H + "px", transform: `scale(${zoom})` }, onClick: () => setSelected(null) }, !elements.length && !(deck?.slides?.length) && h("div", { className: "ppt-welcome", onClick: ev => ev.stopPropagation() }, h("img", { src: "/logo.png", alt: "快马AI" }), h("h1", null, "快马AI PPT 工作台"), h("p", null, "从灵感到演示文稿，一键完成创作"), h("div", { className: "ppt-welcome-actions" }, h("button", { className: "ppt-welcome-primary", onClick: () => window.dispatchEvent(new CustomEvent("ppt:ai-open", { detail: { mode: "create" } })) }, icon("sparkles"), h("span", null, "AI制作PPT")), h("button", { onClick: () => window.dispatchEvent(new CustomEvent("ppt:ai-open", { detail: { mode: "beautify" } })) }, icon("wand-sparkles"), h("span", null, "美化PPT")), h("button", { onClick: openDeck }, icon("file-image"), h("span", null, "图片转可编辑文字PPT")))), [...elements].sort((a, b) => (Number(a.zIndex) || 0) - (Number(b.zIndex) || 0)).map(e => {
      const st = { ...defaults, ...e.style, ...visualStyle(e, e.style || {}) }, bold = st.bold || Number(st.fontWeight) >= 600, isEditing = editing === e.id;
      const geometryOnly = e.geometry?.type === "custom" || e.geometry?.type === "line";
      return h("div", {
        key: e.id, "data-ppt-id": e.id, className: "el " + (e.type === "image" ? "image-el " : "") + (String(e.id).startsWith("ocr-") ? "ocr-el " : "") + (e.id === selected ? "sel" : "") + (isEditing ? " editing" : ""), onClick: ev => {
          ev.stopPropagation();
          setSelected(e.id)
        }, onDoubleClick: ev => {
          ev.stopPropagation();
          if (e.type === "image") { if (ocrMode) ocrAtPoint(ev); else editImage(e); }
          else if (ocrMode && e.type === "text") { ocrAtPoint(ev); }
          else if (e.type === "text") setEditing(e.id)
        }, onMouseDown: ev => {
          if (ev.button === 0 && !e.locked && !isEditing) {
            const duplicate = ev.ctrlKey || ev.metaKey;
            const id = duplicate ? duplicateSelected(e) : e.id;
            setSelected(id || e.id);
            drag.current = { id: id || e.id, duplicated: duplicate, x: ev.clientX, y: ev.clientY, ex: Number(e.x) + (duplicate ? 10 : 0), ey: Number(e.y) + (duplicate ? 10 : 0), canvas: ev.currentTarget.parentElement, before: { slides: [{ id: deck.slides[slide].id, elements: structuredClone(deck.slides[slide].elements) }] } };
            if (duplicate) ev.preventDefault();
          }
        }, style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", opacity: st.opacity ?? e.opacity ?? 1, fontSize: st.fontSize, fontFamily: st.fontFamily, textAlign: textAlign(st.align), textAlignLast: textAlign(st.align) === "justify" ? "justify" : undefined, lineHeight: st.lineHeight || undefined, letterSpacing: st.letterSpacing ? st.letterSpacing + "px" : undefined, fontWeight: bold ? "bold" : "normal", fontStyle: st.italic ? "italic" : "normal", textDecoration: st.underline ? "underline" : "none", color: st.color, paddingTop: st.paddingTop != null ? Number(st.paddingTop) : undefined, paddingLeft: st.paddingLeft != null ? Number(st.paddingLeft) : undefined, zIndex: Number(e.zIndex) || 0, transform: elementTransform(e), transformOrigin: "center" }
      }, h("div", { className: "el-content", style: { ...(geometryOnly ? {} : fillStyle(st.background || e.fill?.image?.assetUrl)), ...shapeStyle(st.shape, st, e.width, e.height), ...(geometryOnly ? {} : boxStyle(st)) } }, geometryOnly ? null : imageFill(st.background || e.fill?.image?.assetUrl), customPath(e, st), e.type === "image" && e.assetUrl ? h(ImageBlock, { src: e.assetUrl, alt: "图片", style: st }) : e.type === "image" ? h("div", { className: "image-placeholder" }, icon("image")) : e.type === "text" && isEditing ? h("div", { ref: editRef, className: "inline-editor", contentEditable: true, suppressContentEditableWarning: true, onBlur: ev => { const node = ev.currentTarget; const runs = readEditableRuns(node); update(x => { x.textRuns = runs.length ? runs : [{ content: node?.textContent || "", style: { ...x.style } }]; x.content = runs.map((r, i) => (i && r.paragraph !== runs[i - 1].paragraph ? "\n" : "") + r.content).join(""); }, e.id); setEditing(null) }, onKeyDown: ev => { if (ev.key === "Escape") { ev.preventDefault(); setEditing(null) } } }, textRuns(e, st)) : textRuns(e, st)), e.id === selected && !isEditing && ["nw", "ne", "sw", "se"].map(dir => h("span", {
        key: dir, className: "handle " + dir, onMouseDown: ev => {
          ev.stopPropagation();
          resize.current = { id: e.id, x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, ew: e.width, eh: e.height, dir, canvas: ev.currentTarget.parentElement.parentElement, before: { slides: [{ id: deck.slides[slide].id, elements: structuredClone(deck.slides[slide].elements) }] } }
        }
      })))
    })), ocrBusy && ocrTarget && h("div", { className: "ocr-scan-overlay", role: "status", "aria-label": "正在扫描图片文字", style: { left: (ocrTarget.x * zoom) + "px", top: (ocrTarget.y * zoom) + "px", width: (ocrTarget.width * zoom) + "px", height: (ocrTarget.height * zoom) + "px", transform: `rotate(${ocrTarget.rotation}deg)`, transformOrigin: "center" } }, h("div", { className: "ocr-scan-line", style: { height: Math.max(2, 3 * zoom) + "px" } }))), h("input", {
      ref: fileInput, type: "file", accept: "image/*", style: { display: "none" }, onChange: ev => {
        const f = ev.target.files?.[0];
        if (f) {
          const r = new FileReader();
          r.onload = () => { if (backgroundTarget.current === "master") { setDeck(d => { const n = structuredClone(d); n.master = n.master || {}; n.master.background = r.result; n.theme = { ...n.theme, background: r.result }; return n }); backgroundTarget.current = false; } else if (backgroundTarget.current) { const id = backgroundTarget.current; update(e => { e.style = { ...e.style, background: r.result }; e.fill = { type: "image", image: { assetUrl: r.result, rotateWithShape: true } } }, id); setSelected(id); backgroundTarget.current = false; } else if (imageTarget.current) { const id = imageTarget.current; update(e => { e.assetUrl = r.result }, id); setSelected(id); imageTarget.current = null } else insertImage(r.result) };
          r.readAsDataURL(f)
        } ev.target.value = ""
      }
    }), h("input", { ref: openFileInput, type: "file", accept: ".json,.pptx,image/*", multiple: true, style: { display: "none" }, onChange: loadDeckFile }), h(Inspector, { element: current, onChange: update, onLayer: changeLayer, master: masterMode ? (deck?.master || {}) : null, onMasterChange: changeMaster, fontFamilies, onPickBackground: () => { backgroundTarget.current = "master"; fileInput.current?.click() }, onPickElementBackground: id => { backgroundTarget.current = id; fileInput.current?.click() }, themeColors: [{ label: "主题", value: deck?.master?.accent || deck?.theme?.accent }, { label: "前景", value: deck?.master?.foreground || deck?.theme?.foreground }, { label: "背景", value: deck?.master?.background || deck?.theme?.background }], onReplaceImage: id => { imageTarget.current = id; fileInput.current?.click() }, onEditImage: id => { const target = elements.find(e => e.id === id); if (target?.assetUrl && window.KuaimaFineEditor) window.KuaimaFineEditor?.open(target.assetUrl, { onApply: next => update(e => { e.assetUrl = next; }, id) }); } }), showJson && h("div", { className: "json-overlay", onClick: () => setShowJson(false) }, h("div", { className: "json-panel", onClick: e => e.stopPropagation() }, h("div", { className: "json-head" }, h("b", null, current ? `对象 JSON · ${names[current.type] || current.type}` : "对象 JSON"), h("button", { onClick: () => setShowJson(false) }, "关闭")), current ? h("textarea", { className: "json-editor", value: jsonText, onChange: e => setJsonText(e.target.value), spellCheck: false }) : h("div", { className: "json-empty" }, "请先选择一个对象"), current && h("div", { className: "json-actions" }, h("button", { className: "btn primary", onClick: () => { try { const parsed = JSON.parse(jsonText); update(x => { Object.keys(x).forEach(k => delete x[k]); Object.assign(x, parsed); }); setShowJson(false); } catch (e) { alert("JSON 格式错误：" + e.message); } } }, "应用修改"), h("button", { onClick: () => setJsonText(JSON.stringify(jsonSnapshot || {}, null, 2)) }, "还原"))))))
  }
  ReactDOM.createRoot(document.getElementById("app")).render(h(App));
})();

























