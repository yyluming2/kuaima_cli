(() => {
  "use strict";
  const { useState, useEffect, useRef } = React, h = React.createElement, W = 1280, H = 720, names = { text: "文本", shape: "形状", table: "表格", chart: "图表", image: "图片" }, defaults = { fontSize: 24, fontFamily: "Microsoft YaHei", bold: false, italic: false, underline: false, color: "#111827", background: "transparent" };
  const api = async (url, o) => {
    const r = await fetch(url, o);
    if (!r.ok) throw Error("请求失败（" + r.status + "）");
    return r
  }, icon = n => h("i", { "data-lucide": n });
  function App() {
    const [deck, setDeck] = useState(null), [slide, setSlide] = useState(0), [selected, setSelected] = useState(null), [editing, setEditing] = useState(null), [topic, setTopic] = useState("产品介绍"), [theme, setTheme] = useState(""), [busy, setBusy] = useState(false), drag = useRef(null), resize = useRef(null), fileInput = useRef(null), openFileInput = useRef(null), imageTarget = useRef(null), editRef = useRef(null), elements = deck?.slides?.[slide]?.elements || [], current = elements.find(e => e.id === selected), update = fn => setDeck(d => {
      const n = structuredClone(d), e = n.slides[slide].elements.find(e => e.id === selected);
      if (e) fn(e);
      return n
    });
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
        const r = await api("/api/ppt/draft", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title: topic, theme }) });
        setDeck(await r.json());
        setSlide(0);
        setSelected(null)
      } catch (e) { alert("生成草稿失败：" + e.message) } finally { setBusy(false) }
    };
    useEffect(() => { generate() }, []);
    useEffect(() => { window.lucide && window.lucide.createIcons() });
    const insertImage = data => {
      if (!deck || !data) return;
      const e = { id: "image-" + Date.now(), type: "image", x: 120, y: 120, width: 420, height: 260, content: "", assetUrl: data, style: { ...defaults } };
      setDeck(d => {
        const n = structuredClone(d);
        n.slides[slide].elements.push(e);
        return n
      });
      setSelected(e.id)
    };
    const add = type => {
      if (!deck) return;
      if (type === "image") { imageTarget.current = null; fileInput.current?.click(); return }
      const e = { id: type + "-" + Date.now(), type, x: 120, y: 120, width: 300, height: 100, content: { text: "双击编辑", shape: "形状", table: "A | B\n1 | 2", chart: "数据图表" }[type], style: { ...defaults, background: type === "shape" ? "#dbeafe" : "transparent" } };
      setDeck(d => {
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
      setDeck(d => {
        const n = structuredClone(d);
        n.slides[slide].elements = n.slides[slide].elements.filter(e => e.id !== selected);
        return n
      });
      setSelected(null)
    };
    const openDeck = () => openFileInput.current?.click();
    const loadDeckFile = ev => {
      const file = ev.target.files?.[0]; ev.target.value = "";
      if (!file) return;
      if (!file.name.toLowerCase().endsWith(".json")) { alert("当前仅支持打开 PPT 工作台 JSON 文件；二进制 .pptx 导入暂未支持。"); return; }
      const reader = new FileReader();
      reader.onload = () => { try { const value = JSON.parse(reader.result); if (!value || !Array.isArray(value.slides) || !value.slides.length) throw Error("文件不是有效的 PPT 数据"); setDeck(value); setSlide(0); setSelected(null); setEditing(null); setTopic(value.title || "产品介绍"); } catch (e) { alert("打开 PPT 文件失败：" + e.message); } };
      reader.readAsText(file);
    };
    useEffect(() => {
      const onKeyDown = ev => {
        const target = ev.target;
        const tag = target?.tagName?.toLowerCase();
        if (editing || tag === "input" || tag === "textarea" || target?.isContentEditable) return;
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
    const tools = [["text", "type", "文本"], ["shape", "shapes", "形状"], ["table", "table-2", "表格"], ["chart", "chart-column", "图表"], ["image", "image-plus", "图片"]];
    return h(React.Fragment, null, h("header", null, h("button", { className: "btn small", onClick: () => location.href = "/" }, icon("arrow-left"), h("span", null, "返回首页")), h("b", null, "快马 AI · PPT 工作台")), h("div", { className: "bar" }, h("input", { value: topic, onChange: e => setTopic(e.target.value), placeholder: "主题" }), h("input", { value: theme, onChange: e => setTheme(e.target.value), placeholder: "风格" }), h("button", { onClick: openDeck }, icon("folder-open"), h("span", null, "打开 PPT 文件")), h("button", { onClick: generate, disabled: busy }, icon("sparkles"), h("span", null, busy ? "生成中…" : "生成草稿")), h("button", { onClick: exportDeck, disabled: !deck }, icon("download"), h("span", null, "导出 PPTX"))), h("div", { className: "tools" }, ...tools.map(([t, i, l]) => h("button", { key: t, onClick: () => add(t) }, icon(i), h("span", null, l))), h("button", {
      className: "danger", disabled: !current, onClick: removeSelected
    }, icon("trash-2"), h("span", null, "删除"))), h("main", null, h("div", { id: "slides" }, (deck?.slides || []).map((s, i) => h("div", {
      key: i, className: "thumb " + (i === slide ? "active" : ""), onClick: () => {
        setSlide(i);
        setSelected(null)
      }
      }, h("div", { className: "thumb-preview" }, (s.elements || []).map(e => { const ts = { ...defaults, ...(e.style || {}) }; return h("div", { key: e.id, className: "thumb-el", style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", background: e.type === "shape" ? (ts.background || "#dbeafe") : "transparent", color: ts.color || "#172033", fontSize: Math.max(8, (Number(ts.fontSize) || 24) * 0.18), fontWeight: ts.bold ? "bold" : "normal", fontStyle: ts.italic ? "italic" : "normal", textDecoration: ts.underline ? "underline" : "none" } }, e.type === "image" && e.assetUrl ? h("img", { src: e.assetUrl, alt: "" }) : e.type === "image" ? icon("image") : (e.content || "")); })), h("span", { className: "thumb-label" }, `第${i + 1}页`)))), h("div", { className: "canvas", onClick: () => setSelected(null) }, elements.map(e => {
      const st = { ...defaults, ...e.style }, bold = st.bold || Number(st.fontWeight) >= 600, isEditing = editing === e.id;
      return h("div", {
        key: e.id, className: "el " + (e.id === selected ? "sel" : "") + (isEditing ? " editing" : ""), onClick: ev => {
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
        }, style: { left: e.x / W * 100 + "%", top: e.y / H * 100 + "%", width: e.width / W * 100 + "%", height: e.height / H * 100 + "%", fontSize: st.fontSize, fontFamily: st.fontFamily, fontWeight: bold ? "bold" : "normal", fontStyle: st.italic ? "italic" : "normal", textDecoration: st.underline ? "underline" : "none", color: st.color, background: st.background }
      }, e.type === "image" && e.assetUrl ? h("img", { src: e.assetUrl, alt: "图片" }) : e.type === "image" ? h("div", { className: "image-placeholder" }, icon("image")) : e.type === "text" && isEditing ? h("div", { ref: editRef, className: "inline-editor", contentEditable: true, suppressContentEditableWarning: true, onBlur: ev => { const node = ev.currentTarget; const content = node ? node.textContent || "" : e.content || ""; update(x => x.content = content); setEditing(null) }, onKeyDown: ev => { if (ev.key === "Escape") setEditing(null) } }, e.content || "") : e.content || "", e.id === selected && !isEditing && ["nw", "ne", "sw", "se"].map(dir => h("span", {
        key: dir, className: "handle " + dir, onMouseDown: ev => {
          ev.stopPropagation();
          resize.current = { x: ev.clientX, y: ev.clientY, ex: e.x, ey: e.y, ew: e.width, eh: e.height, dir, canvas: ev.currentTarget.parentElement.parentElement }
        }
      })))
    })), h("input", {
      ref: fileInput, type: "file", accept: "image/*", style: { display: "none" }, onChange: ev => {
        const f = ev.target.files?.[0];
        if (f) {
          const r = new FileReader();
          r.onload = () => { if (imageTarget.current) { const id = imageTarget.current; update(e => { if (e.id === id) e.assetUrl = r.result }); setSelected(id); imageTarget.current = null } else insertImage(r.result) };
          r.readAsDataURL(f)
        } ev.target.value = ""
      }
    }), h("input", { ref: openFileInput, type: "file", accept: ".json,application/json", style: { display: "none" }, onChange: loadDeckFile }), h(Inspector, { element: current, onChange: update, onReplaceImage: id => { imageTarget.current = id; fileInput.current?.click() } })))
  }
  function Inspector({ element, onChange, onReplaceImage }) {
    const st = { ...defaults, ...(element?.style || {}) };
    if (!element) return h("aside", { className: "side" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, "未选择元素"));
    const field = (label, key, type = "text") => h("label", null, label, h("input", {
      type, step: type === "number" ? "0.1" : undefined, value: type === "number" && ["x", "y", "width", "height"].includes(key) ? Number(element[key] || 0).toFixed(1) : (element[key] ?? st[key] ?? ""), onChange: e => onChange(x => {
        if (key in x) x[key] = type === "number" ? Number(e.target.value) : e.target.value;
        else x.style = { ...x.style, [key]: type === "number" ? Number(e.target.value) : e.target.value }
      })
    })), toggle = (label, key) => h("label", { className: "check" }, h("input", { type: "checkbox", checked: !!st[key], onChange: e => onChange(x => x.style = { ...x.style, [key]: e.target.checked }) }), label);
    return h("aside", { className: "side" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, names[element.type] || element.type), element.type === "image" && h("button", { className: "replace-image", type: "button", onClick: () => onReplaceImage?.(element.id) }, "替换图片"), element.type !== "image" && h("label", null, "内容", h("textarea", { value: element.content || "", onChange: e => onChange(x => x.content = e.target.value) })), h("section", { className: "geometry" }, field("X 位置", "x", "number"), field("Y 位置", "y", "number"), field("宽度", "width", "number"), field("高度", "height", "number")), element.type === "text" && h("section", null, field("字号", "fontSize", "number"), field("字体", "fontFamily"), field("文字颜色", "color", "color"), h("div", { className: "checks" }, toggle("加粗", "bold"), toggle("斜体", "italic"), toggle("下划线", "underline"))), (element.type === "shape" || element.type === "text") && field("背景色", "background", "color"), field("旋转角度", "rotation", "number"))
  }
  ReactDOM.createRoot(document.getElementById("app")).render(h(App));
})();
