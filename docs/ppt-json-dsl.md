# PPT JSON DSL

## AI 生成流程 API

工作台提供两阶段接口，客户端可以先让用户确认方案，再生成可编辑 PPT：

- `GET /api/ipricing`：获取可用模型列表，其中 `data.text` 用于文案/大纲模型，`data.img` 用于图片模型；前端可将选中的模型名传入下方接口。
- `POST /api/ppt/plan`：请求体支持 `title`、`style`、`audience`、`textModel`、`imageModel`、`references[]`、`images[]`、`pageCount`，返回包含 `pages[]`（标题、要点、布局和插画提示词）的方案。
- `POST /api/ppt/generate`：提交确认后的方案，返回本 DSL 的完整 `Presentation`。每页会优先使用对应的 `images[]`，缺少图片时生成内置 SVG 插画占位，后续可替换为图片模型产物。

这样 UI 或第三方客户端都可以在“确认风格/大纲”和“一键生成”之间插入人工编辑，而不需要改变 PPT 编辑器数据模型。

PPT 编辑器、PPTX 导入和 PPTX 导出共用这一份 JSON 数据模型。坐标单位是逻辑像素，默认画布为 `1280 x 720`（16:9）。

## 设计约定

- `style` 是唯一可编辑、可导出的样式来源。
- `textStyle`、`fill`、`line`、`geometry`、`effects` 是导入器保留的结构化信息；编辑器打开文件时会把它们补充到 `style`，但不会覆盖已有的 `style` 字段。
- 同一个字段同时存在时，优先级为：用户编辑后的 `style` > 导入的结构化字段 > 主题默认值。
- 文本内容以 `content` 为兼容字段；存在 `textRuns` 时，`textRuns` 是字符级内容的来源，`content` 必须保持为所有 run 拼接后的结果。

## 顶层结构

```json
{
  "id": "deck-001",
  "title": "产品介绍",
  "width": 1280,
  "height": 720,
  "theme": {
    "name": "商务蓝",
    "background": "#FFFFFF",
    "foreground": "#172033",
    "accent": "#2563EB",
    "fonts": ["Aptos", "Microsoft YaHei"]
  },
  "slides": [
    {
      "id": "slide-1",
      "layout": "title",
      "notes": "演讲备注",
      "elements": []
    }
  ]
}
```

## 元素

```json
{
  "id": "title",
  "type": "text",
  "x": 80,
  "y": 160,
  "width": 1120,
  "height": 100,
  "content": "方案标题",
  "style": {
    "fontFamily": "Microsoft YaHei",
    "fontSize": 48,
    "bold": true,
    "color": "#172033",
    "align": "center",
    "lineHeight": 1.2,
    "letterSpacing": 0,
    "background": "transparent",
    "border": "1px solid #CBD5E1",
    "shadow": "0 4px 12px rgba(0,0,0,.12)"
  },
  "locked": false,
  "zIndex": 1,
  "rotation": 0,
  "opacity": 1
}
```

支持的 `type`：`text`、`image`、`shape`、`table`、`chart`。

图片使用 `assetUrl`；形状可在 `style.shape` 中使用 `rect`、`roundrect`、`ellipse`、`triangle`、`diamond`、`hexagon`、`pentagon`、`parallelogram`、`righttriangle`、`rightarrow`、`leftarrow`、`uparrow`、`downarrow`、`star5`、`heart`、`line`。未识别的形状按 `rect` 处理。表格和图表的具体数据暂放在 `content`，后续可扩展为结构化字段而不改变元素外壳。

### 导出样式字段

`style` 中以下字段由 PPTX 导出器直接读取：

- 填充：`background` 或 `fill`。支持十六进制颜色、颜色名、`rgba(r,g,b,a)`、`transparent`/`none`。
- 边框：推荐使用 CSS 字符串 `border: "1px solid #CBD5E1"`；也接受对象 `{ "width": 1, "color": "#CBD5E1", "style": "solid" }`。`style` 可取 `solid`、`dashed`/`dash`、`dotted`/`dot`、`none`。
- 透明度：元素级 `opacity` 和颜色中的 alpha 通道都会被导出，取值范围为 `0`（完全透明）到 `1`（完全不透明）。
- 形状变换：`rotation`（角度）。`flipH`、`flipV` 目前保留在模型中供编辑器使用。

PPTX 仅支持 DrawingML 原生填充。CSS 渐变在导出时使用首个渐变色作为兼容回退；编辑器中的渐变信息仍保留在模型中。

## 富文本

```json
{
  "type": "text",
  "content": "重点内容次要内容",
  "textRuns": [
    { "content": "重点内容", "paragraph": 0, "style": { "bold": true, "color": "#2563EB" } },
    { "content": "次要内容", "paragraph": 0, "style": { "fontSize": 20 } }
  ],
  "textParagraphs": [
    { "runs": [0, 1], "align": "left", "lineHeight": 1.3 }
  ]
}
```

`align` 可用 `left`、`center`、`right`、`justify`（导入 PPT 时也接受 `l`、`ctr`、`r`、`just`）。

## 结构化导入字段

导入器可以保留以下字段，供需要精确还原 PPTX 的渲染器使用：`geometry`、`fill`、`line`、`effects`、`textStyle`、`flipH`、`flipV`。这些字段不能成为第二套编辑状态；前端规范化后，用户修改统一写入 `style`，导出也读取 `style`。

## 校验规则

- `width`、`height` 必须为正数；至少有一页幻灯片。
- 每页和每个元素必须有唯一 `id`；元素必须有 `type`。
- `width`、`height` 不得为负数。
- 颜色使用十六进制 CSS 格式（如 `#2563EB`）或完整 CSS 颜色。
- 颜色透明度推荐使用 `rgba(...)` 或元素级 `opacity`；结构化 `fill.color.alpha` 取 `0` 到 `1`。
- `style.border` 的宽度单位为逻辑像素；导出时会换算为 PPT 磅值。
- 文本的 `content` 与 `textRuns` 拼接结果应保持一致。
