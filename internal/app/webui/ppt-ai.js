(() => {
  const $ = id => document.getElementById(id), modal = $("pptAiModal");
  fetch("/api/config").then(r => r.json()).then(c => { const v = $("pptAppVersion"); if (v && c?.version) v.textContent = "v" + c.version; }).catch(() => {});
  const views = { form: $("pptAiForm"), outline: $("pptAiOutline"), progress: $("pptAiProgress"), generating: $("pptAiGenerating") };
  let currentPlan = null, generatedDeck = null, visualMode = "auto", contentMode = "create";
  const setContentMode = mode => {
    contentMode = mode === "beautify" ? "beautify" : "create";
    document.querySelectorAll("[data-content-mode]").forEach(tab => { const selected = tab.dataset.contentMode === contentMode; tab.setAttribute("aria-selected", String(selected)); tab.tabIndex = selected ? 0 : -1; });
    $("pptAiCreatePanel").hidden = contentMode !== "create";
    $("pptAiBeautifyPanel").hidden = contentMode !== "beautify";
    $("pptAiPagesField").hidden = contentMode === "beautify";
    $("pptAiContentHint").textContent = contentMode === "beautify" ? "上传 PPT，保留原页内容和顺序进行美化" : "输入主题和内容，创建全新 PPT";
  };
  document.querySelectorAll("[data-content-mode]").forEach(tab => {
    tab.onclick = () => setContentMode(tab.dataset.contentMode);
    tab.onkeydown = event => { if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return; event.preventDefault(); setContentMode(event.key === "Home" ? "create" : event.key === "End" ? "beautify" : contentMode === "create" ? "beautify" : "create"); $(contentMode === "create" ? "pptAiCreateTab" : "pptAiBeautifyTab").focus(); };
  });
  window.addEventListener("ppt:ai-open", event => { setContentMode(event.detail?.mode); show(true); });
  const esc = v => String(v ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
  const readOne = file => file ? new Promise((ok, bad) => { const r = new FileReader(); r.onload = () => ok(r.result || ""); r.onerror = () => bad(new Error("读取文件失败")); r.readAsDataURL(file); }) : Promise.resolve("");
  let materials = [];
  const materialImages = () => materials.flatMap(m => m.image ? [{name:m.name,image:m.image}] : m.pageImages || []);
  const renderPDFPages = async (file, pages) => {
    if(pages.length > 50 || materialImages().length + pages.length > 50) throw new Error("参考图片最多 50 张，请拆分 PDF");
    const pdfjs = window.pdfjsLib;
    if(!pdfjs) throw new Error("PDF 渲染组件未加载，请刷新页面");
    pdfjs.GlobalWorkerOptions.workerSrc="/pdf.worker.min.js";
    const task=pdfjs.getDocument({data:await file.arrayBuffer(),isEvalSupported:false});
    try {
      const pdfDocument=await task.promise, images=[];
      for(const number of pages) {
        $("pptAiReferenceStatus").textContent=`正在转换 ${file.name} 第 ${number} 页…`;
        const page=await pdfDocument.getPage(number), original=page.getViewport({scale:1});
        const viewport=page.getViewport({scale:Math.min(2,1600/Math.max(original.width,original.height))});
        const canvas=document.createElement("canvas");
        canvas.width=Math.ceil(viewport.width); canvas.height=Math.ceil(viewport.height);
        const context=canvas.getContext("2d");
        await page.render({canvasContext:context,viewport,background:"rgb(255,255,255)"}).promise;
        images.push({name:`${file.name} · 第 ${number} 页`,image:canvas.toDataURL("image/jpeg",0.85)});
        canvas.width=canvas.height=0; page.cleanup();
      }
      if([...materialImages(),...images].reduce((total,m)=>total+m.image.length,0)>48*1024*1024) throw new Error("参考图片总大小过大，请拆分 PDF");
      return images;
    } finally { await task.destroy(); }
  };
  const renderMaterials = () => {
    $("pptAiReferenceList").innerHTML = materials.map((m,i) => `<div class="ppt-ai-material">${m.image ? `<img src="${esc(m.image)}" alt="${esc(m.name)}">` : ""}<span>${esc(m.name)}${m.text ? `<small>已读取 ${m.text.length} 字符</small>` : ""}${m.pageImages?.length ? `<small>已转换 ${m.pageImages.length} 页图片</small>` : ""}</span><button type="button" data-remove-material="${i}" aria-label="移除 ${esc(m.name)}">×</button></div>`).join("") + materials.flatMap(m=>(m.pageImages || []).map(p=>`<div class="ppt-ai-material"><img src="${esc(p.image)}" alt="${esc(p.name)}"><span>${esc(p.name)}</span></div>`)).join("");
  };
  $("pptAiReferenceList").onclick = e => { const b=e.target.closest("[data-remove-material]"); if(b) { materials.splice(Number(b.dataset.removeMaterial),1); renderMaterials(); } };
  let readingMaterials = false;
  $("pptAiReferenceFiles").onchange = async () => {
    readingMaterials = true; $("pptAiReferenceFiles").disabled=true; $("pptAiReferenceStatus").textContent="正在读取参考素材…";
    try { for(const file of $("pptAiReferenceFiles").files) {
      if(file.size > 10*1024*1024) throw new Error(`${file.name} 超过 10 MB`);
      if(materials.length >= 12) throw new Error("最多添加 12 个参考素材");
      if(/^image\/(png|jpeg|webp)$/.test(file.type)) { if(materialImages().length>=50) throw new Error("参考图片最多 50 张"); materials.push({name:file.name,image:await readOne(file)}); }
      else if(/\.(doc|docx|pdf)$/i.test(file.name)) {
        const response=await fetch("/api/ppt/reference/parse?name="+encodeURIComponent(file.name), {method:"POST",body:await file.arrayBuffer()});
        if(!response.ok) throw new Error(await apiError(response));
        const parsed=await response.json();
        const pageImages=parsed.imagePages?.length ? await renderPDFPages(file,parsed.imagePages) : [];
        materials.push({name:file.name,text:parsed.text,pageImages});
      }
      else if(/\.pptx?$/i.test(file.name)) { const deck=await importDeck(file); materials.push({name:file.name,text:JSON.stringify(deck)}); }
      else if(/\.(txt|md|csv|json)$/i.test(file.name)) materials.push({name:file.name,text:await file.text()});
      else throw new Error(`不支持的文件格式：${file.name}`);
    } } catch(e) { alert("添加素材失败："+e.message); }
    finally { readingMaterials=false; $("pptAiReferenceFiles").disabled=false; $("pptAiReferenceFiles").value=""; $("pptAiReferenceStatus").textContent=""; renderMaterials(); }
  };
  const apiError = async r => { const t = await r.text(); try { return JSON.parse(t).error || t; } catch (_) { return t || `请求失败（${r.status}）`; } };
  const setStep = n => document.querySelectorAll(".ppt-ai-steps [data-step]").forEach(x => { const v = Number(x.dataset.step); x.classList.toggle("active", v === n); x.classList.toggle("done", v < n); });
  const showView = (name, step) => { Object.entries(views).forEach(([k, v]) => v.hidden = k !== name); setStep(step); };
  let minimized = false, generating = false;
  const show = on => { modal.hidden = !on; if (on) { minimized = false; $("pptAiRestore").hidden = true; showView("form", 1); } };
  const close = () => { if (generating) { if (!confirm("PPT 仍在生成中，确定关闭并停止查看进度吗？")) return; } generating = false; minimized = false; modal.hidden = true; $("pptAiRestore").hidden = true; };
  const updateFloatingProgress = (status = "正在生成") => {
    const count = $("pptAiGeneratingCount").textContent;
    $("pptAiRestore").textContent = `✦ ${status} · ${count} 页`;
    $("pptAiRestore").title = "点击查看 PPT 生成详情";
  };
  const minimize = () => { if (!generating) return; minimized = true; modal.hidden = true; $("pptAiRestore").hidden = false; };
  const showProgress = (title, text) => { showView("progress", 1); $("pptAiProgress").querySelector(".ppt-ai-progress-title").textContent = title; $("pptAiProgressText").textContent = text; $("pptAiStreamText").textContent = ""; };
  const loadModels = async () => { try { const d = await fetch("/api/ipricing").then(r => r.json()); const fill = (id, a) => { const v = (a || []).filter(x => x?.model_name); $(id).innerHTML = v.length ? v.map(x => `<option value="${esc(x.model_name)}">${esc(x.model_name)}${x.description ? ` · ${esc(x.description)}` : ""}</option>`).join("") : '<option value="">使用默认模型</option>'; }; fill("pptAiTextModel", d?.data?.agent || d?.data?.text); fill("pptAiImageModel", d?.data?.img || d?.data?.image); } catch (_) { $("pptAiTextModel").innerHTML = $("pptAiImageModel").innerHTML = '<option value="">使用默认模型</option>'; } };
  const readSSE = async response => { const reader = response.body.getReader(), decoder = new TextDecoder(); let pending = "", result = null, text = ""; while (true) { const part = await reader.read(); pending += decoder.decode(part.value || new Uint8Array(), { stream: !part.done }); const blocks = pending.split("\n\n"); pending = blocks.pop() || ""; for (const block of blocks) { const line = block.split("\n").find(x => x.startsWith("data:")); if (!line) continue; let val; try { val = JSON.parse(line.slice(5).trim()); } catch (_) { continue; } if (block.includes("event: text")) { text += val.text || ""; const preview=$("pptAiStreamText"); renderLiveOutline(text); } if (block.includes("event: error")) throw new Error(val.error || "大纲生成失败"); if (block.includes("event: done")) result = val; } if (part.done) break; } if (!result) throw new Error("模型未返回有效方案"); return result; };
  const importDeck = async file => { if (!file) return null; const name = file.name.toLowerCase(); const r = await fetch("/api/pptx/import", { method:"POST", headers:{"Content-Type":name.endsWith(".pptx") || name.endsWith(".ppt") ? "application/vnd.ms-powerpoint" : "application/json"}, body:await file.arrayBuffer() }); if (!r.ok) throw new Error(await apiError(r)); return r.json(); };
    const titleOf = (brief, deck) => brief.split(/[\n，。,.]/)[0].trim() || deck?.title || "未命名演示文稿";
  const renderOutline = () => {
    const ps = currentPlan.pages || [], imgs = currentPlan.images || [], reference = !!currentPlan.sourcePresentation;
    $("pptAiOutlineCount").textContent = ps.length;
    $("pptAiOutlineList").innerHTML = ps.map((p, i) => {
      let content;
      if (reference) {
        const src = imgs[i] || "";
        content = src ? `<img src="${esc(src)}" alt="第${i + 1}页参考图" style="display:block;width:320px;max-width:100%;aspect-ratio:16/9;object-fit:contain;border-radius:8px;background:#f1f5f9">` : "<span>暂无页面图片</span>";
      } else {
        const blocks = (p.blocks || []).map((block, index) => ({block, index})).filter(({block}) => block.type === "text" && block.role !== "title");
        content = `<label>页面标题<input data-outline-title="${i}" value="${esc(p.title)}"></label>`;
        content += blocks.length ? blocks.map(({block, index}) => `<label>大纲内容<textarea rows="3" data-outline-page="${i}" data-outline-block="${index}">${esc(block.content)}</textarea></label>`).join("") : `<label>大纲内容<textarea rows="4" data-outline-bullets="${i}">${esc((p.bullets || []).join("\n"))}</textarea></label>`;
      }
      if (!reference) {
        const selected = p.referenceImage || "";
        content += `<div class="ppt-ai-page-reference"><label>参考图片 <small>选填，用于本页生成</small><select data-page-reference="${i}"><option value="">不使用参考图片</option>${(currentPlan.materialImages || []).map((m,j)=>`<option value="${j}" ${m.image===selected ? "selected" : ""}>${esc(m.name)}</option>`).join("")}${selected && !(currentPlan.materialImages || []).some(m=>m.image===selected) ? '<option value="custom" selected>本页上传图片</option>' : ""}</select></label><label>添加本页图片<input type="file" accept="image/png,image/jpeg,image/webp" data-page-image="${i}"></label>${selected ? `<img src="${esc(selected)}" alt="第 ${i+1} 页参考图片">` : ""}</div>`;
      }
      return `<div class="ppt-ai-outline-item"><b>${String(i + 1).padStart(2,"0")}</b><div>${content}</div></div>`;
    }).join("");
  };
  $("pptAiOutlineList").onchange = async e => {
    if(e.target.matches("[data-page-reference]")) { readOutline(); const i=Number(e.target.dataset.pageReference), v=e.target.value; if(v!=="custom") currentPlan.pages[i].referenceImage=v==="" ? "" : currentPlan.materialImages[Number(v)].image; renderOutline(); }
    if(e.target.matches("[data-page-image]")) { const file=e.target.files[0]; if(!file) return; if(file.size>10*1024*1024) { alert("图片不能超过 10 MB"); e.target.value=""; return; } try { const image=await readOne(file); readOutline(); currentPlan.pages[Number(e.target.dataset.pageImage)].referenceImage=image; renderOutline(); } catch(error) { alert(error.message); } }
  };
  const readOutline = () => {
    currentPlan.unifiedPrompt = $("pptAiUnifiedPrompt").value.trim();
    if (currentPlan.sourcePresentation) return;
    const list = $("pptAiOutlineList");
    list.querySelectorAll("[data-outline-title]").forEach(input => {
      const page = currentPlan.pages[Number(input.dataset.outlineTitle)];
      page.title = input.value.trim();
      (page.blocks || []).filter(block => block.type === "text" && block.role === "title").forEach(block => { block.content = page.title; });
    });
    list.querySelectorAll("[data-outline-block]").forEach(input => { currentPlan.pages[Number(input.dataset.outlinePage)].blocks[Number(input.dataset.outlineBlock)].content = input.value.trim(); });
    list.querySelectorAll("[data-outline-bullets]").forEach(input => { currentPlan.pages[Number(input.dataset.outlineBullets)].bullets = input.value.split("\n").map(line => line.trim()).filter(Boolean); });
    currentPlan.pages.forEach(page => {
      const blocks = (page.blocks || []).filter(block => block.type === "text" && block.role !== "title");
      if (blocks.length) page.bullets = blocks.flatMap(block => block.content.split("\n")).filter(Boolean);
    });
  };
  const renderMaster = () => {
    const sample = $("pptAiSample"), image = currentPlan.masterImage || "";
    $("pptAiOutline").querySelector(".ppt-ai-master-card").hidden = !!currentPlan.sourcePresentation;
    sample.hidden = !image;
    if (image) sample.src = image;
    else sample.removeAttribute("src");
  };
  const setMasterBusy = busy => {
    const card = $("pptAiOutline").querySelector(".ppt-ai-master-card"), back = $("pptAiStyleBack"), generateButton = $("pptAiGenerate"), regenerateButton = $("pptAiRegenerate");
    card?.classList.toggle("is-busy", busy);
    if (regenerateButton) regenerateButton.disabled = busy;
    if (back) back.disabled = busy;
    if (generateButton) generateButton.disabled = busy;
  };
  const openMasterPreview = () => {
    const sample = $("pptAiSample");
    if (sample.hidden) return;
    const image = sample.src;
    if (!image) return;
    let overlay = $("pptAiMasterImagePreview");
    if (!overlay) { overlay = document.createElement("div"); overlay.id = "pptAiMasterImagePreview"; overlay.className = "ppt-ai-image-preview-overlay"; overlay.innerHTML = '<div class="ppt-ai-image-preview-dialog"><button type="button" class="ppt-ai-image-preview-close" aria-label="关闭">×</button><img alt="视觉参考母版大图"></div>'; document.body.appendChild(overlay); overlay.onclick = e => { if (e.target === overlay || e.target.closest(".ppt-ai-image-preview-close")) overlay.hidden = true; }; }
    overlay.querySelector("img").src = image; overlay.hidden = false;
  };
  const showPlan = () => {
    const reference = !!currentPlan.sourcePresentation, view = $("pptAiOutline");
    $("pptAiUnifiedPrompt").value = currentPlan.unifiedPrompt || "";
    $("pptAiPlanSummary").textContent = `${currentPlan.pages.length} 页 · ${currentPlan.style || "AI 自动设计"}`;
    view.querySelector(".ppt-ai-confirm-head h3").textContent = reference ? "参考页面" : "文字大纲";
    view.querySelector(".ppt-ai-confirm-head p").textContent = reference ? "逐页使用 Canvas 画面作为视觉参考" : "确认每页标题、内容和参考图片，可直接修改后生成";
    view.querySelector(".ppt-ai-outline-head b").textContent = reference ? "页面预览" : "页面大纲";
    renderMaster();
    $("pptAiMasterPrompt").value = "";
    renderOutline(); showView("outline", 2);
  };
  const visual = () => { const style = $("pptAiStyle").value.trim(); if (visualMode === "color") return { style: style || document.querySelector(".ppt-color-palettes button.selected span")?.textContent || "主题色系", accentColor:$("pptAiAccent").value, secondaryColor:$("pptAiSecondary").value, backgroundColor:$("pptAiBackground").value }; if (visualMode === "image") return { style:style || "参考图片风格", accentColor:"", backgroundColor:"" }; return { style:style || "AI 自动设计", accentColor:"", backgroundColor:"" }; };
  const colorPrompt = vs => vs.accentColor ? `主题配色：主色 ${vs.accentColor}，辅助色 ${vs.secondaryColor}，背景色 ${vs.backgroundColor}。所有页面统一使用以上色值，主色用于重点和标题，辅助色用于图表和装饰，背景使用指定背景色，并确保文字对比度清晰。` : "";
  const withRetry = async (task, label, retries = 3) => { let last; for (let attempt = 0; attempt <= retries; attempt++) { try { return await task(); } catch (e) { last = e; if (attempt < retries) $("pptAiProgressText").textContent = `${label}失败，正在重试 ${attempt + 1}/${retries}…`; } } throw last; };
  const validImageRef = value => { const v = String(value || "").trim(); return !v || v.startsWith("/outputs/") || v.startsWith("data:image/"); };
  const master = async (title, brief, prompts, vs) => { const f = visualMode === "image" ? $("pptAiStyleImage").files[0] : null; if (visualMode === "image" && !f) throw new Error("请上传视觉参考图片"); if (f) return readOne(f); const r = await fetch("/api/ppt/master", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({title, brief, style:vs.style, unifiedPrompt:prompts.unifiedPrompt, accentColor:vs.accentColor, backgroundColor:vs.backgroundColor, imageModel:$("pptAiImageModel").value}) }); if (!r.ok) throw new Error(await apiError(r)); const v = await r.json(); if (!v.image) throw new Error("未获取到母版图"); return v.image; };
  const build = async () => { if(readingMaterials) return alert("参考素材正在读取，请稍后再生成"); const brief = $(contentMode === "beautify" ? "pptAiBeautifyBrief" : "pptAiBrief").value.trim(), file = contentMode === "beautify" ? $("pptAiDeck").files[0] : null; if (contentMode === "create" && !brief) return alert("请输入主题和内容"); if (contentMode === "beautify" && !file) return alert("请上传需要美化的 PPT 文件"); if (visualMode === "image" && !$("pptAiStyleImage").files[0]) return alert("请上传视觉参考图片"); const btn = $("pptAiPlan"); btn.disabled = true; showProgress("正在读取参考内容…", file ? "正在渲染参考 PPT Canvas" : "正在理解主题内容"); try { const deck = await importDeck(file); if (file && !deck?.slides?.length) throw new Error("参考 PPT 中没有可美化的页面"); const refs = [brief, $("pptAiReferenceText").value.trim(), ...materials.filter(m=>m.text).map(m=>`参考文件：${m.name}\n${m.text}`)].filter(Boolean).join("\n\n"), title = titleOf(brief, deck), n = Math.min(50, Math.max(0, Number($("pptAiPages").value) || deck?.slides?.length || 0)), vs = visual(); if (file && deck?.slides?.length) { const canvasImages = window.PPTRenderDeckSnapshots ? await window.PPTRenderDeckSnapshots(deck) : deck.slides.map(s => s.canvasImage || s.previewImage || ""); currentPlan={title,style:vs.style,textModel:$("pptAiTextModel").value,imageModel:$("pptAiImageModel").value,images:canvasImages,sourcePresentation:deck,references:[],pages:deck.slides.map((_,i)=>({title:`参考第 ${i+1} 页`,layout:"content",bullets:[],imagePrompt:"美化这一页演示文稿，依据参考图准确保留原页文字、数据和主题，重新设计版式与视觉。"})),unifiedPrompt:["请对参考页面进行明显的视觉美化和重新设计。保留原页全部文字、数据、事实和核心图片信息，优化排版、字体层级、配色、背景、留白和图文关系，使信息更清晰、画面更精致。允许调整元素位置、尺寸和装饰，输出完整单页演示文稿。", brief ? "用户美化要求：" + brief : "", "目标风格：" + vs.style, colorPrompt(vs)].filter(Boolean).join("\n"),outlinePrompt:"",imageOnly:true}; showPlan(); if ($("pptAiDirect").classList.contains("is-on")) setTimeout(generate, 0); return; } $("pptAiProgressText").textContent = "正在生成统一提示词"; const pr = await withRetry(() => fetch("/api/ppt/prompts", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({style:[vs.style, colorPrompt(vs)].filter(Boolean).join("\n")}) }).then(async r => { if (!r.ok) throw new Error(await apiError(r)); return r; }), "统一提示词生成"); const prompts = await pr.json(); prompts.unifiedPrompt = [prompts.unifiedPrompt, colorPrompt(vs)].filter(Boolean).join("\n"); $("pptAiProgressText").textContent = "正在生成每页大纲和参考母版"; const planReq = withRetry(() => fetch("/api/ppt/plan?stream=1", { method:"POST", headers:{"Content-Type":"application/json",Accept:"text/event-stream"}, body:JSON.stringify({title,style:vs.style,pageCount:n,references:[refs],images:materialImages().map(m=>m.image),textModel:$("pptAiTextModel").value,imageModel:$("pptAiImageModel").value,unifiedPrompt:prompts.unifiedPrompt,outlinePrompt:prompts.outlinePrompt,accentColor:vs.accentColor,backgroundColor:vs.backgroundColor,unifiedBackground:true}) }).then(async r => { if (!r.ok) throw new Error(await apiError(r)); return readSSE(r); }), "方案生成"); let masterError = ""; const [plan, image] = await Promise.all([planReq, master(title, brief, prompts, vs).catch(error => { masterError = error.message; return ""; })]); currentPlan = {...plan, images:[], materialImages:materialImages().map(m=>({...m})), masterImage:validImageRef(image) ? image : "", unifiedPrompt:prompts.unifiedPrompt, outlinePrompt:prompts.outlinePrompt, accentColor:vs.accentColor, backgroundColor:vs.backgroundColor, unifiedBackground:true}; showPlan(); if ($("pptAiDirect").classList.contains("is-on")) setTimeout(generate, 0); } catch (e) { alert("生成方案失败：" + e.message); showView("form",1); } finally { btn.disabled = false; } };
  const regenerate = async () => { if (!currentPlan || currentPlan.sourcePresentation) return; const b = $("pptAiRegenerate"); setMasterBusy(true); b.textContent = "生成中…"; try { readOutline(); const editPrompt = $("pptAiMasterPrompt").value.trim(); currentPlan.masterImage = await master(currentPlan.title, $("pptAiBrief").value.trim(), {unifiedPrompt:[currentPlan.unifiedPrompt, editPrompt ? "母版修改要求：" + editPrompt : ""].filter(Boolean).join("\n")}, visual()); renderMaster(); } catch (e) { alert("重新生成母版失败：" + e.message); } finally { setMasterBusy(false); b.textContent = "重新生成"; } };
  const normalizeSlideImages = async (deck, slide) => { if (!deck || !slide) return; const width = Number(deck.width) || 1280, height = Number(deck.height) || 720; const targetRatio = width / height; const fallbackCrop = targetRatio >= 1 ? {left: (1 - (1 / targetRatio)) / 2, right: (1 - (1 / targetRatio)) / 2, top: 0, bottom: 0} : {left: 0, right: 0, top: (1 - targetRatio) / 2, bottom: (1 - targetRatio) / 2}; (slide.elements || []).filter(e => e.type === "image" && e.assetUrl).forEach(e => { e.x = 0; e.y = 0; e.width = width; e.height = height; e.style = {...(e.style || {}), crop: {...fallbackCrop}}; }); await Promise.all((slide.elements || []).filter(e => e.type === "image" && e.assetUrl).map(e => new Promise(resolve => { const img = new Image(); img.onload = () => { const ratio = img.naturalWidth / img.naturalHeight; const crop = {left:0, top:0, right:0, bottom:0}; const defaultSide = targetRatio > 1 ? (1 - (1 / targetRatio)) / 2 : 0; const defaultVert = targetRatio < 1 ? (1 - targetRatio) / 2 : 0; e.style = {...(e.style || {}), crop: {left: defaultSide, right: defaultSide, top: defaultVert, bottom: defaultVert}}; if (ratio > targetRatio) { const keep = targetRatio / ratio; const side = (1 - keep) / 2; crop.left = crop.right = side; } else if (ratio < targetRatio) { const keep = ratio / targetRatio; const side = (1 - keep) / 2; crop.top = crop.bottom = side; } e.x = 0; e.y = 0; e.width = width; e.height = height; e.rotation = 0; e.style = {...(e.style || {}), crop}; resolve(); }; img.onerror = () => { const side = targetRatio > 1 ? (1 - (1 / targetRatio)) / 2 : 0; const vert = targetRatio < 1 ? (1 - targetRatio) / 2 : 0; e.style = {...(e.style || {}), crop: {left: side, right: side, top: vert, bottom: vert}}; resolve(); }; img.src = e.assetUrl; }))); };
  const imageOf = (d, i) => (d?.slides?.[i]?.elements || []).find(e => e.type === "image")?.assetUrl || "";
  const openGeneratedImage = (url, index) => { if (!url) return; let o = $("pptAiImagePreview"); if (!o) { o = document.createElement("div"); o.id = "pptAiImagePreview"; o.className = "ppt-ai-image-preview-overlay"; o.innerHTML = '<div class="ppt-ai-image-preview-dialog"><button type="button" class="ppt-ai-image-preview-close" aria-label="关闭">×</button><button type="button" class="ppt-ai-image-preview-nav prev" aria-label="上一张">‹</button><img alt="生成页面预览"><button type="button" class="ppt-ai-image-preview-nav next" aria-label="下一张">›</button></div>'; document.body.appendChild(o); o.onclick = e => { if (e.target === o || e.target.closest(".ppt-ai-image-preview-close")) o.hidden = true; }; o.querySelector(".prev").onclick = () => movePreview(-1); o.querySelector(".next").onclick = () => movePreview(1); } o.dataset.index = String(Number(index) || 0); o.querySelector("img").src = url; o.querySelector("img").alt = `第 ${Number(index)+1} 页生成预览`; o.hidden = false; updatePreviewNav(); };
  const previewURLAt = index => document.querySelector(`.ppt-ai-page-card[data-page="${index}"] .ppt-ai-page-preview.is-preview`)?.dataset.previewImage || imageOf(generatedDeck, index);
  const updatePreviewNav = () => { const o = $("pptAiImagePreview"); if (!o || o.hidden) return; const i = Number(o.dataset.index) || 0, total = currentPlan?.pages?.length || 0; o.querySelector(".prev").disabled = i <= 0; o.querySelector(".next").disabled = i >= total - 1; };
  const movePreview = delta => { const o = $("pptAiImagePreview"); if (!o || o.hidden) return; const i = (Number(o.dataset.index) || 0) + delta, url = previewURLAt(i); if (i < 0 || i >= (currentPlan?.pages?.length || 0) || !url) return; openGeneratedImage(url, i); };
  const queue = ps => { $("pptAiPageList").innerHTML = ps.map((p,i) => `<div class="ppt-ai-page-card waiting" data-page="${i}"><div class="ppt-ai-page-preview"><span>${String(i+1).padStart(2,"0")}</span></div><div><b>${esc(p.title)}</b><small><i></i><span>等待生成</span></small><button class="ppt-ai-retry" type="button" data-retry-page="${i}" hidden title="重试此页" aria-label="重试第 ${i+1} 页">↻</button></div></div>`).join(""); };
  const card = (i, state, image, text) => { const x = document.querySelector(`.ppt-ai-page-card[data-page="${i}"]`); if (!x) return; x.className = `ppt-ai-page-card ${state}`; x.querySelector("small span").textContent = text; const retry = x.querySelector(".ppt-ai-retry"); if (retry) retry.hidden = state !== "error"; if (image) { const p=x.querySelector(".ppt-ai-page-preview"); p.style.backgroundImage = `url("${image}")`; p.dataset.previewImage=image; p.classList.add("is-preview"); p.title="点击放大预览"; p.querySelector("span").textContent = ""; } };
  const initializeDeck = async () => {
    const { images, sourcePresentation, materialImages, ...plan } = currentPlan;
    const response = await fetch("/api/ppt/generate", {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(plan)});
    if (!response.ok) throw new Error(await apiError(response));
    generatedDeck = await response.json(); await Promise.all((generatedDeck.slides || []).map(slide => normalizeSlideImages(generatedDeck, slide)));
  };
  // Send only this page and its original reference; merge only its result.
  const generatePage = async i => {
    if (!generatedDeck) await initializeDeck();
    const presentation = {...generatedDeck, slides:[generatedDeck.slides[i]]};
    const reference = currentPlan.sourcePresentation ? currentPlan.images?.[i] : currentPlan.pages[i].referenceImage;
    if (reference) delete presentation.master;
    const response = await fetch("/api/ppt/images", {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({presentation, slideIndex:0, referenceImage:reference || "", referenceMode:currentPlan.sourcePresentation ? "beautify" : "material"})});
    if (!response.ok) throw new Error(await apiError(response));
    const result = await response.json();
    if (result.errors?.length) throw new Error(result.errors.join("；"));
    if (!result.presentation?.slides?.[0]) throw new Error("未返回页面结果");
    generatedDeck.slides[i] = result.presentation.slides[0]; await normalizeSlideImages(generatedDeck, generatedDeck.slides[i]);
  };
  const retryPage = async i => {
    card(i, "working", imageOf(generatedDeck, i), "正在重试…");
    try { await generatePage(i); card(i, "done", imageOf(generatedDeck, i), "生成完成"); window.dispatchEvent(new CustomEvent("ppt:generated", {detail:generatedDeck})); }
    catch (error) { card(i, "error", imageOf(generatedDeck, i), "生成失败，可重试"); }
  };
  const generate = async () => { if (!currentPlan) return; generating = true; readOutline(); currentPlan.imageOnly = true; showView("generating",3); $("pptAiDoneActions").hidden = true; queue(currentPlan.pages); const total = currentPlan.pages.length, maxRetries = Math.min(5, Math.max(0, Number($("pptAiRetryCount").value) || 0)); $("pptAiGeneratingCount").textContent = `0 / ${total}`; $("pptAiPageProgressBar").style.width = "0%"; updateFloatingProgress(); const errors = []; try { generatedDeck = null; await initializeDeck(); for (let i=0;i<total;i++) { card(i,"working","", "AI 正在生成…"); let lastError = ""; for (let attempt=0; attempt<=maxRetries; attempt++) { try { await generatePage(i); lastError = ""; break; } catch(e) { lastError = e.message; if (attempt < maxRetries) card(i,"working","",`生成失败，自动重试 ${attempt + 1}/${maxRetries}…`); } } if (lastError) { errors.push(`第 ${i+1} 页生成失败`); card(i,"error",imageOf(generatedDeck,i),"生成失败，可重试"); } else card(i,"done",imageOf(generatedDeck,i),"生成完成"); $("pptAiGeneratingCount").textContent = `${i+1} / ${total}`; $("pptAiPageProgressBar").style.width = `${(i+1)/total*100}%`; updateFloatingProgress(); } window.dispatchEvent(new CustomEvent("ppt:generated",{detail:generatedDeck})); if ($("pptAiDirect")?.classList.contains("is-on")) { generating = false; close(); } $("pptAiGeneratingTitle").textContent = errors.length ? "PPT 已生成，部分页面失败" : "PPT 生成完成"; updateFloatingProgress(errors.length ? "部分页面失败" : "生成完成"); $("pptAiGeneratingText").textContent = errors.length ? errors.join("；") : "所有页面已生成，可以进入编辑器继续调整。"; $("pptAiDoneActions").hidden = false; } catch(e) { currentPlan.pages.forEach((_, i) => card(i, "error", imageOf(generatedDeck, i), "生成中断，可重试")); $("pptAiGeneratingTitle").textContent = "生成中断"; updateFloatingProgress("生成中断"); $("pptAiGeneratingText").textContent = e.message; $("pptAiDoneActions").hidden = false; } finally { generating = false; } };
  $("pptAiPageList").addEventListener("click", e => { const b=e.target.closest("[data-retry-page]"); if (b) return retryPage(Number(b.dataset.retryPage)); const p=e.target.closest(".ppt-ai-page-preview.is-preview"); if (p) openGeneratedImage(p.dataset.previewImage, p.closest(".ppt-ai-page-card")?.dataset.page || 0); }); document.addEventListener("keydown", e => { const o=$("pptAiImagePreview"); if (e.key === "Escape") o?.setAttribute("hidden", ""); else if (!o?.hidden && e.key === "ArrowLeft") { e.preventDefault(); movePreview(-1); } else if (!o?.hidden && e.key === "ArrowRight") { e.preventDefault(); movePreview(1); } });
  document.querySelectorAll('input[name="pptVisualMode"]').forEach(x => x.onchange = () => { visualMode = x.value; document.querySelectorAll(".ppt-visual-modes label").forEach(l => l.classList.toggle("selected", l.contains(x))); $("pptAiColorPanel").hidden = visualMode !== "color"; $("pptAiImagePanel").hidden = visualMode !== "image"; });
  document.querySelectorAll(".ppt-color-palettes button").forEach(x => x.onclick = () => { document.querySelectorAll(".ppt-color-palettes button").forEach(y => y.classList.toggle("selected", x===y)); $("pptAiAccent").value=x.dataset.accent; $("pptAiSecondary").value=x.querySelector("i")?.style.getPropertyValue("--c2").trim() || x.dataset.accent; $("pptAiBackground").value=x.dataset.background; });
  $("pptAiDeck").onchange=()=>$("pptAiDeckHint").textContent=$("pptAiDeck").files[0]?.name||"支持 .pptx 或 .json；.ppt 文件请先另存为 .pptx"; $("pptAiStyleImage").onchange=()=>$("pptAiStyleHint").textContent=$("pptAiStyleImage").files[0]?.name||"支持 JPG、PNG、WebP";
  $("pptAiDirect").onclick=()=>{ const on=$("pptAiDirect").classList.toggle("is-on"); $("pptAiDirect").setAttribute("aria-pressed",String(on)); $("pptAiDirect").title=on ? "免确认直出：已开启" : "免确认直出：已关闭"; }; $("pptAiPlan").onclick=build; $("pptAiRegenerate").onclick=regenerate; $("pptAiGenerate").onclick=generate; $("pptAiStyleBack").onclick=()=>showView("form",1); $("pptAiOpen").onclick=()=>show(true); $("pptAiClose").onclick=close; $("pptAiCancel").onclick=close; $("pptAiMinimize").onclick=minimize; $("pptAiRestore").onclick=()=>{ minimized=false; modal.hidden=false; $("pptAiRestore").hidden=true; }; $("pptAiDone").onclick=()=>{ close(); setTimeout(()=>document.querySelector('[data-ppt-action="export"]')?.click(), 150); };
  $("pptAiStyleImage").addEventListener("change", () => { const f=$("pptAiStyleImage").files[0], p=$("pptAiStylePreview"); $("pptAiStyleHint").textContent=f?.name||"支持 JPG、PNG、WebP"; p.hidden=!f; if(f) p.querySelector("img").src=URL.createObjectURL(f); });
  $("pptAiSample").onclick = openMasterPreview;
  function renderLiveOutline(raw){const box=$("pptAiStreamText"), m=[...raw.matchAll(/"title"\s*:\s*"([^"]*)"[\s\S]{0,900}?"bullets"\s*:\s*\[([\s\S]*?)(?:\]|$)/g)]; if(!m.length){box.textContent=raw.slice(-5000);return} box.innerHTML=m.map((x,i)=>"<div class=\"ppt-ai-live-card\"><b>"+String(i+1).padStart(2,"0")+" "+esc(x[1])+"</b><ul>"+[...x[2].matchAll(/"((?:\\\\.|[^"\\\\])*)"/g)].map(b=>{let v=b[1];try{v=JSON.parse('"'+v+'"')}catch(_){}return "<li>"+esc(v)+"</li>"}).join("")+"</ul></div>").join(""); box.scrollTop=box.scrollHeight; }
  setTimeout(()=>show(true),0); loadModels();
})();









































