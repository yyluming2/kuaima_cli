(() => {
  "use strict";

  const { useEffect, useState } = React;
  const h = React.createElement;
  const { defaults, names, rewriteRunContent, shapeStyle, icon } = window.PPTEditor;
  const palette = ["#111827", "#ffffff", "#2563eb", "#0f9f8f", "#f97316", "#dc2626", "#facc15", "#8b5cf6", "#64748b"];
  const shapePresets = [
    ["rect", "矩形"], ["roundRect", "圆角矩形"], ["ellipse", "椭圆"], ["triangle", "三角形"],
    ["rightTriangle", "直角三角形"], ["diamond", "菱形"], ["parallelogram", "平行四边形"],
    ["pentagon", "五边形"], ["hexagon", "六边形"]
  ];

  function ColorField({ label, value, onChange, allowTransparent = false, themeColors = [] }) {
    const pickerValue = /^#[0-9a-f]{6}$/i.test(value || "") ? value : "#ffffff";
    const themed = themeColors.filter(item => item?.value && /^#[0-9a-f]{6}$/i.test(item.value));
    return h("div", { className: "color-field" },
      h("span", { className: "color-label" }, label),
      h("div", { className: "color-control" },
        h("label", { className: "color-picker", title: "自定义颜色" },
          h("span", { style: { background: value === "transparent" ? "transparent" : pickerValue } }),
          h("input", { type: "color", value: pickerValue, onChange: event => onChange(event.target.value), "aria-label": label })
        ),
        themed.length > 0 && h("div", { className: "theme-color-swatches", "aria-label": "主题颜色" },
          themed.map(item => h("button", { type: "button", key: item.label, title: `${item.label} ${item.value}`, className: String(value).toLowerCase() === item.value.toLowerCase() ? "selected" : "", onClick: () => onChange(item.value) },
            h("span", { style: { background: item.value } }), h("small", null, item.label)
          ))
        ),
        h("div", { className: "color-swatches" },
          palette.map(color => h("button", { type: "button", key: color, title: color, className: String(value).toLowerCase() === color ? "selected" : "", style: { background: color }, onClick: () => onChange(color) })),
          allowTransparent && h("button", { type: "button", title: "透明", className: "transparent-swatch " + (value === "transparent" ? "selected" : ""), onClick: () => onChange("transparent") })
        )
      )
    );
  }

  const TabButton = ({ id, label, active, onClick }) => h("button", { type: "button", className: active === id ? "active" : "", onClick: () => onClick(id) }, label);

  const primaryTab = element => element?.type === "text" ? "text" : element?.type === "image" ? "image" : element?.type === "shape" ? "shape" : element ? "content" : "layout";

  function Inspector({ element, onChange, onReplaceImage, onLayer, master, onMasterChange, themeColors = [] }) {
    const [activeTab, setActiveTab] = useState(primaryTab(element));
    useEffect(() => setActiveTab(primaryTab(element)), [element?.id, !!master]);
    useEffect(() => { window.lucide && window.lucide.createIcons(); }, [activeTab, element?.id]);

    if (!element && master) {
      const set = (key, value) => onMasterChange?.(target => { target[key] = value; });
      return h("aside", { className: "side master-settings" },
        h("div", { className: "inspector-heading" }, h("b", null, "母版属性"), h("p", { className: "type-hint" }, "修改后立即应用到所有页面")),
        ColorField({ label: "背景填充", value: master.background || "#ffffff", onChange: value => set("background", value) }),
        ColorField({ label: "主题色（标题与强调）", value: master.accent || "#2563eb", onChange: value => set("accent", value) }),
        ColorField({ label: "前景色（正文）", value: master.foreground || "#172033", onChange: value => set("foreground", value) }),
        h("div", { className: "geometry" },
          h("label", null, "标题字号", h("input", { type: "number", min: 10, value: master.titleSize || 40, onChange: event => set("titleSize", Number(event.target.value)) })),
          h("label", null, "正文字号", h("input", { type: "number", min: 10, value: master.bodySize || 24, onChange: event => set("bodySize", Number(event.target.value)) }))
        ),
        h("label", null, "字体", h("input", { value: master.fontFamily || "Microsoft YaHei", onChange: event => onMasterChange?.(target => { target.fontFamily = event.target.value; target.fonts = event.target.value.split(",").map(value => value.trim()).filter(Boolean); }) })),
        h("p", { className: "master-apply-hint" }, "以上设置会立即应用到全部页面的标题和正文。")
      );
    }

    if (!element) return h("aside", { className: "side" }, h("div", { className: "inspector-heading" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, "未选择元素")));

    const style = { ...defaults, ...(element.style || {}) };
    const setStyle = (target, key, value) => {
      target.style = { ...target.style, [key]: value };
      if (Array.isArray(target.textRuns) && ["fontFamily", "fontSize", "color", "bold", "italic", "underline", "lineHeight", "letterSpacing"].includes(key)) {
        target.textRuns = target.textRuns.map(run => ({ ...run, style: { ...run.style, [key]: value } }));
      }
    };
    const field = (label, key, type = "text", options = {}) => h("label", null, label, h("input", {
      type, min: options.min, max: options.max, step: options.step ?? (type === "number" ? "0.1" : undefined),
      value: type === "number" && ["x", "y", "width", "height"].includes(key) ? Number(element[key] || 0).toFixed(1) : (element[key] ?? style[key] ?? ""),
      onChange: event => onChange(target => {
        const value = type === "number" ? Number(event.target.value) : event.target.value;
        if (["x", "y", "width", "height", "rotation"].includes(key) || key in target) target[key] = value; else setStyle(target, key, value);
      })
    }));
    const cropField = (label, key) => h("label", null, label, h("input", { type: "number", min: 0, max: .99, step: .01, value: Number(style.crop?.[key] || 0), onChange: event => onChange(target => { target.style = { ...target.style, crop: { ...(target.style?.crop || {}), [key]: Number(event.target.value) } }; }) }));
    const toggle = (label, key) => h("label", { className: "check" }, h("input", { type: "checkbox", checked: !!style[key], onChange: event => onChange(target => setStyle(target, key, event.target.checked)) }), label);
    const setColor = (key, value) => onChange(target => setStyle(target, key, value));
    const hasShape = element.type === "shape" || !!style.shape;
    const border = typeof style.border === "object" && style.border ? style.border : (() => { const match = String(style.border || "").match(/([\d.]+)px\s+(solid|dashed|dotted)\s+(#[0-9a-f]{6})/i); return match ? { width: Number(match[1]), style: match[2], color: match[3] } : {}; })();
    const setBorder = (key, value) => onChange(target => {
      const next = { width: Number(border.width) || 0, style: border.style || "solid", color: border.color || "#64748b", [key]: value };
      if (key === "color" && value === "transparent") { next.width = 0; next.style = "none"; }
      else if (key === "color" && next.width <= 0) { next.width = 1; next.style = "solid"; }
      setStyle(target, "border", next);
    });
    const setShape = value => onChange(target => { setStyle(target, "shape", value); target.geometry = { type: "preset", preset: value }; });
    const tabs = element.type === "text"
      ? [{ id: "text", label: "文字" }, ...(hasShape ? [{ id: "shape", label: "形状" }] : []), { id: "appearance", label: "外观" }, { id: "layout", label: "布局" }]
      : element.type === "image"
        ? [{ id: "image", label: "图片" }, { id: "appearance", label: "外观" }, { id: "layout", label: "布局" }]
        : element.type === "shape"
          ? [{ id: "shape", label: "形状" }, { id: "layout", label: "布局" }]
          : [{ id: "content", label: "内容" }, { id: "appearance", label: "外观" }, { id: "layout", label: "布局" }];
    const shownTab = tabs.some(tab => tab.id === activeTab) ? activeTab : tabs[0].id;

    return h("aside", { className: "side" },
      h("div", { className: "inspector-heading" }, h("b", null, "元素属性"), h("div", { className: "type-hint" }, names[element.type] || element.type, style.shapeName ? ` · ${style.shapeName}` : "")),
      h("div", { className: "inspector-tabs", role: "tablist" }, tabs.map(tab => h(TabButton, { key: tab.id, ...tab, active: shownTab, onClick: setActiveTab }))),

      shownTab === "layout" && h("div", { className: "inspector-tab" },
        h("h3", null, "位置与大小"),
        h("section", { className: "geometry" }, field("X 位置", "x", "number"), field("Y 位置", "y", "number"), field("宽度", "width", "number", { min: 1 }), field("高度", "height", "number", { min: 1 })),
        h("section", { className: "geometry" }, field("旋转角度", "rotation", "number"), field("透明度", "opacity", "number", { min: 0, max: 1, step: .05 })),
        h("h3", null, "元素层级"),
        h("div", { className: "layer-actions" },
          h("button", { type: "button", onClick: () => onLayer?.("front"), title: "移到最上层" }, icon("bring-to-front"), h("span", null, "置顶")),
          h("button", { type: "button", onClick: () => onLayer?.("up"), title: "向上移动一层" }, icon("chevron-up"), h("span", null, "上移")),
          h("button", { type: "button", onClick: () => onLayer?.("down"), title: "向下移动一层" }, icon("chevron-down"), h("span", null, "下移")),
          h("button", { type: "button", onClick: () => onLayer?.("back"), title: "移到最下层" }, icon("send-to-back"), h("span", null, "置底"))
        )
      ),

      shownTab === "text" && h("div", { className: "inspector-tab" },
        h("label", null, "内容", h("textarea", { value: element.content || "", onChange: event => onChange(target => rewriteRunContent(target, event.target.value)) })),
        h("section", { className: "geometry" }, field("字号", "fontSize", "number", { min: 1 }), field("字体", "fontFamily")),
        ColorField({ label: "文字颜色", value: style.color, themeColors, onChange: value => setColor("color", value) }),
        h("label", null, "文字对齐", h("select", { value: style.align || "l", onChange: event => onChange(target => setStyle(target, "align", event.target.value)) }, h("option", { value: "l" }, "左对齐"), h("option", { value: "ctr" }, "居中"), h("option", { value: "r" }, "右对齐"), h("option", { value: "just" }, "两端对齐"))),
        h("section", { className: "geometry" }, field("行间距", "lineHeight", "number"), field("字间距", "letterSpacing", "number")),
        h("div", { className: "checks" }, toggle("加粗", "bold"), toggle("斜体", "italic"), toggle("下划线", "underline"))
      ),

      shownTab === "content" && h("div", { className: "inspector-tab" },
        h("label", null, "内容", h("textarea", { value: element.content || "", onChange: event => onChange(target => { target.content = event.target.value; }) }))
      ),

      shownTab === "shape" && h("div", { className: "inspector-tab" },
        h("h3", null, "形状类型"),
        h("div", { className: "shape-presets" }, shapePresets.map(([value, label]) => h("button", { type: "button", key: value, title: label, className: String(style.shape || "rect").toLowerCase() === value.toLowerCase() ? "active" : "", onClick: () => setShape(value) },
          h("span", { style: { ...shapeStyle(value, style), background: "currentColor" } }), h("small", null, label)
        ))),
        h("h3", null, "填充与轮廓"),
        ColorField({ label: "填充颜色", value: style.background, allowTransparent: true, themeColors, onChange: value => setColor("background", value) }),
        ColorField({ label: "轮廓颜色", value: border.color || "transparent", allowTransparent: true, themeColors, onChange: value => setBorder("color", value) }),
        h("section", { className: "geometry" },
          h("label", null, "轮廓宽度", h("input", { type: "number", min: 0, step: .5, value: Number(border.width) || 0, onChange: event => setBorder("width", Number(event.target.value)) })),
          h("label", null, "轮廓样式", h("select", { value: border.style || "solid", onChange: event => setBorder("style", event.target.value) }, h("option", { value: "solid" }, "实线"), h("option", { value: "dashed" }, "虚线"), h("option", { value: "dotted" }, "点线"), h("option", { value: "none" }, "无")))
        ),
        String(style.shape || "").toLowerCase() === "roundrect" && field("圆角大小 (%)", "radius", "number", { min: 0, max: 50 }),
        h("h3", null, "形状文字与效果"),
        h("label", null, "形状文字", h("textarea", { value: element.content || "", onChange: event => onChange(target => { target.content = event.target.value; }) })),
        field("阴影（CSS）", "shadow")
      ),

      shownTab === "appearance" && h("div", { className: "inspector-tab" },
        h("h3", null, "填充与轮廓"),
        ColorField({ label: element.type === "text" ? "背景颜色" : "填充颜色", value: style.background, allowTransparent: true, themeColors, onChange: value => setColor("background", value) }),
        hasShape && field("形状类型", "shape"), hasShape && field("形状名称", "shapeName"),
        String(style.shape || "").toLowerCase() === "roundrect" && field("圆角大小 (%)", "radius", "number", { min: 0, max: 50 }),
        h("h3", null, "边框与效果"),
        field("边框（CSS）", "border"), field("阴影（CSS）", "shadow")
      ),

      shownTab === "image" && h("div", { className: "inspector-tab" },
        h("button", { className: "replace-image", type: "button", onClick: () => onReplaceImage?.(element.id) }, "替换图片"),
        h("h3", null, "裁剪"),
        h("section", { className: "geometry" }, cropField("左", "left"), cropField("上", "top"), cropField("右", "right"), cropField("下", "bottom"))
      )
    );
  }

  window.PPTEditor = { ...window.PPTEditor, ColorField, Inspector };
})();
