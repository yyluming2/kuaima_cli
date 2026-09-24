(() => {
  const $ = (id) => document.getElementById(id);
  const modal = $("pptAiModal"), form = $("pptAiForm"), outline = $("pptAiOutline"), styleStep = $("pptAiStyleStep"), progress = $("pptAiProgress");
  let currentPlan = null;
  const loadTextModels = async () => {
    try {
      const payload = await fetch("/api/ipricing").then(r => r.json());
      // /api/ipricing calls text-generation models "agent". Keep the
      // legacy "text" fallback for older servers.
      const models = payload?.data?.agent || payload?.data?.text || [];
      const select = $("pptAiTextModel");
      const escAttr = value => String(value ?? "").replace(/[&<>\"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
      select.innerHTML = models.length ? models.filter(m => m && m.model_name).map(m => `<option value="${escAttr(m.model_name)}">${escAttr(m.model_name)}${m.description ? ` · ${escAttr(m.description)}` : ""}</option>`).join("") : '<option value="">使用默认模型</option>';
      const imageModels = payload?.data?.img || payload?.data?.image || [];
      const imageSelect = $("pptAiImageModel");
      imageSelect.innerHTML = imageModels.length ? imageModels.filter(m => m && m.model_name).map(m => `<option value="${escAttr(m.model_name)}">${escAttr(m.model_name)}${m.description ? ` · ${escAttr(m.description)}` : ""}</option>`).join("") : '<option value="">使用默认模型</option>';
    } catch (_) { $("pptAiTextModel").innerHTML = '<option value="">使用默认模型</option>'; $("pptAiImageModel").innerHTML = '<option value="">使用默认模型</option>'; }
  };
  const readImages = async () => [];
  const readOne = file => file ? new Promise(resolve => { const r = new FileReader(); r.onload = () => resolve(r.result || ""); r.onerror = () => resolve(""); r.readAsDataURL(file); }) : Promise.resolve("");
  const readPlanStream = async (res) => { const reader = res.body.getReader(), decoder = new TextDecoder(), chunks = []; let pending = "", done = null; while (true) { const part = await reader.read(); pending += decoder.decode(part.value || new Uint8Array(), { stream: !part.done }); const blocks = pending.split("\n\n"); pending = blocks.pop() || ""; for (const block of blocks) { const line = block.split("\n").find(x => x.startsWith("data:")); if (!line) continue; try { const value = JSON.parse(line.slice(5).trim()); if (block.includes("event: text")) { chunks.push(value.text || ""); $("pptAiStreamText").textContent = chunks.join("").slice(-3000); } if (block.includes("event: done")) done = value; if (block.includes("event: error")) throw new Error(value.error || "流式生成失败"); } catch (e) { if (e.message) throw e; } } if (part.done) break; } return done; };
  const show = (v) => { modal.hidden = !v; if (v) { form.hidden = false; outline.hidden = true; styleStep.hidden = true; progress.hidden = true; } };
  const progressState = (text) => { progress.hidden = false; form.hidden = true; outline.hidden = true; styleStep.hidden = true; $("pptAiProgressText").textContent = text; };
  const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
  const renderOutline = () => { $("pptAiOutlineList").innerHTML = (currentPlan.pages || []).map((p, i) => `<div class="ppt-ai-outline-item"><input class="outline-title" data-i="${i}" value="${esc(p.title)}"><textarea class="outline-bullets" data-i="${i}" rows="2">${esc((p.bullets || []).join("\n"))}</textarea></div>`).join(""); };
  const readOutline = () => { document.querySelectorAll(".outline-title").forEach(x => { const p=currentPlan.pages[+x.dataset.i]; p.title = x.value.trim() || `第 ${+x.dataset.i + 1} 页`; const b=(p.blocks||[]).find(v=>v.type==="text"&&v.role==="title"); if(b)b.content=p.title; }); document.querySelectorAll(".outline-bullets").forEach(x => { const p=currentPlan.pages[+x.dataset.i]; p.bullets = x.value.split(/\n+/).map(v => v.trim()).filter(Boolean); const b=(p.blocks||[]).find(v=>v.type==="text"&&v.role!=="title"); if(b)b.content=p.bullets.join("\n"); }); };
  setTimeout(() => {
    const bar = document.querySelector(".bar"), entry = document.querySelector(".ppt-ai-entry"), button = $("pptAiOpen");
    if (bar && button) { bar.appendChild(button); entry?.remove(); [...bar.querySelectorAll("input")].filter(x => x.placeholder === "主题" || x.placeholder === "风格").forEach(x => x.remove()); }
  }, 80);
  setInterval(() => {
    document.querySelectorAll("#slides .thumb").forEach(thumb => {
      if (thumb.querySelector(".thumb-actions")) return;
      const label = thumb.querySelector(".thumb-label"); if (!label) return;
      const actions = document.createElement("span"); actions.className = "thumb-actions";
      [["sparkles", "AI 重新生成本页", "ai"], ["image", "重新生成本页配图", "image"], ["type", "转成文字可编辑版本", "editable"], ["plus", "新增页面", "add"], ["copy", "复制页面", "duplicate"], ["trash-2", "删除页面（不可撤销）", "delete"]].forEach(([icon, title, action]) => { const b = document.createElement("button"); b.type = "button"; b.title = title; b.setAttribute("aria-label", title); b.innerHTML = `<i data-lucide="${icon}"></i>`; b.onclick = ev => { ev.stopPropagation(); if (action === "delete" && !window.confirm("删除此页后不可撤销，确定继续吗？")) return; const index = [...document.querySelectorAll("#slides .thumb")].indexOf(thumb); window.dispatchEvent(new CustomEvent(action === "ai" ? "ppt:ai-page" : action === "image" ? "ppt:image-page" : action === "editable" ? "ppt:image-editable" : "ppt:page", { detail: action === "ai" || action === "image" || action === "editable" ? index : action })); }; actions.appendChild(b); });
      window.lucide?.createIcons?.();
      label.appendChild(actions);
    });
  }, 250);
  $("pptAiOpen").onclick = () => show(true); $("pptAiClose").onclick = () => show(false); $("pptAiCancel").onclick = () => show(false);
  // 打开 PPT 页面时直接进入 AI 制作流程。
  setTimeout(() => show(true), 0);
  loadTextModels();
  $("pptAiStyleNext").onclick = async () => { if (!currentPlan) return; const title=$("pptAiTitle").value.trim()||"未命名主题", brief=$("pptAiBrief").value.trim(); progressState("正在生成内容大纲"); try { const refs=[brief].filter(Boolean); const pageCount=Math.min(50,Math.max(3,Number($("pptAiPages").value)||5)); const res=await fetch("/api/ppt/plan?stream=1",{method:"POST",headers:{"Content-Type":"application/json","Accept":"text/event-stream"},body:JSON.stringify({title,style:$("pptAiStyle").value.trim(),textModel:$("pptAiTextModel").value,imageModel:$("pptAiImageModel").value,pageCount,references:refs})}); if(!res.ok)throw new Error(await res.text()); currentPlan=await readPlanStream(res); renderOutline(); progress.hidden=true; styleStep.hidden=false; outline.hidden=true; $("pptAiGenerate").textContent="确认大纲，生成全部页面"; $("pptAiSample").querySelector(".sample-title").textContent="大纲已生成，确认后开始生成页面"; } catch(e){ alert("大纲生成失败："+e.message); styleStep.hidden=false; progress.hidden=true; } };
  $("pptAiStyleBack").onclick = () => { styleStep.hidden = true; outline.hidden = false; };
  const refreshSample = () => { const s = $("pptAiSample"); s.style.background = $("pptAiBackground").value; s.style.setProperty("--sample-accent", $("pptAiAccent").value); };
  ["pptAiAccent", "pptAiBackground"].forEach(id => $(id).oninput = refreshSample);
  $("pptAiStyleImage").onchange = () => { const f = $("pptAiStyleImage").files[0]; $("pptAiStyleHint").textContent = f?.name || "未选择"; };
  
  $("pptAiDeck").onchange = () => { $("pptAiDeckHint").textContent = $("pptAiDeck").files[0]?.name || "可选"; };
  $("pptAiPlan").onclick = async () => {
    const brief = $("pptAiBrief").value.trim(), title = brief.split(/[，。,.\n]/)[0].trim() || "未命名主题";
    const button = $("pptAiPlan"); button.disabled = true; button.textContent = "正在生成母版…"; progressState("正在连接图片模型并生成视觉母版");
    try {
      const res = await fetch("/api/ppt/master", {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({title,brief,style:$("pptAiStyle").value.trim(),imageModel:$("pptAiImageModel").value})});
      if (!res.ok) throw new Error(await res.text()); const master = await res.json(); if (!master.image) throw new Error("模型未返回母版图");
      currentPlan = {title, style:$("pptAiStyle").value.trim(), imageModel:$("pptAiImageModel").value, masterImage:master.image};
      const sample=$("pptAiSample"); sample.style.backgroundImage=`url("${master.image}")`; sample.style.backgroundSize="cover"; sample.querySelector(".sample-title").textContent="视觉母版已生成"; form.hidden=true; progress.hidden=true; styleStep.hidden=false;
    } catch (e) { alert("母版生成失败：" + e.message); form.hidden = false; progress.hidden = true; } finally { button.disabled = false; button.textContent = "第一步：生成母版图"; }
  };
  $("pptAiGenerate").onclick = async () => {
    if (!currentPlan) return; const button = $("pptAiGenerate"); button.disabled = true; button.textContent = "正在生成…";
    currentPlan.accentColor = $("pptAiAccent").value; currentPlan.backgroundColor = $("pptAiBackground").value; currentPlan.unifiedBackground = $("pptAiUnified").checked; currentPlan.styleReference = await readOne($("pptAiStyleImage").files[0]);
    progressState("已确认母版，正在生成全部页面图片");
    try {
      readOutline(); currentPlan.imageOnly = true;
      const res = await fetch("/api/ppt/generate", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(currentPlan) });
      if (!res.ok) throw new Error(await res.text());
      const textDeck = await res.json();
      const imgRes = await fetch("/api/ppt/images", {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({presentation:textDeck})});
      const deck = await imgRes.json(); if (!imgRes.ok) throw new Error(deck.error || "图片生成失败");
      window.dispatchEvent(new CustomEvent("ppt:generated", { detail: deck.presentation || textDeck }));
      show(false);
    } catch (e) { alert("PPT 图片生成失败：" + e.message); styleStep.hidden = false; progress.hidden = true; } finally { button.disabled = false; button.textContent = "确认母版，进入大纲"; }
  };
  // Extended master/outline flow: explicit unified and outline prompts, with
  // upload and regeneration as first-class master sources.
  $("pptAiMasterImage").onchange = () => { const f=$("pptAiMasterImage").files[0]; $("pptAiMasterHint").textContent=f?.name||"未选择，将由 AI 生成"; };
  const makeMaster = async (button) => { const title=$("pptAiTitle").value.trim()||"未命名主题", brief=$("pptAiBrief").value.trim(), unifiedPrompt=$("pptAiUnifiedPrompt").value.trim(); button.disabled=true; progressState("正在生成视觉母版"); try { const r=await fetch("/api/ppt/master",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({title,brief,style:$("pptAiStyle").value.trim(),unifiedPrompt,imageModel:$("pptAiImageModel").value})}); if(!r.ok)throw new Error(await r.text()); const m=await r.json(); currentPlan={...(currentPlan||{}),title,style:$("pptAiStyle").value.trim(),imageModel:$("pptAiImageModel").value,masterImage:m.image,unifiedPrompt}; $("pptAiSample").style.backgroundImage=`url("${m.image}")`; $("pptAiSample").style.backgroundSize="cover"; form.hidden=true; progress.hidden=true; styleStep.hidden=false; } finally { button.disabled=false; } };
  $("pptAiRegenerate").onclick=()=>makeMaster($("pptAiRegenerate"));
  const oldPlan=$("pptAiPlan").onclick; $("pptAiPlan").onclick=async()=>{ const f=$("pptAiMasterImage").files[0]; if(!f){ return oldPlan(); } const r=new FileReader(); r.onload=()=>{ currentPlan={title:$("pptAiTitle").value.trim()||"未命名主题",style:$("pptAiStyle").value.trim(),imageModel:$("pptAiImageModel").value,masterImage:r.result,unifiedPrompt:$("pptAiUnifiedPrompt").value.trim()}; $("pptAiSample").style.backgroundImage=`url("${r.result}")`; form.hidden=true; styleStep.hidden=false; }; r.readAsDataURL(f); };
  const oldNext=$("pptAiStyleNext").onclick; $("pptAiStyleNext").onclick=async()=>{ if(!currentPlan)return; const masterImage=currentPlan.masterImage, unifiedPrompt=$("pptAiUnifiedPrompt").value.trim(), outlinePrompt=$("pptAiOutlinePrompt").value.trim(); const oldFetch=window.fetch; window.fetch=(url,opt)=>{ if(String(url).includes("/api/ppt/plan")){ const body=JSON.parse(opt.body); body.unifiedPrompt=unifiedPrompt; body.outlinePrompt=outlinePrompt; opt.body=JSON.stringify(body); } return oldFetch(url,opt); }; try{ await oldNext(); } finally{ window.fetch=oldFetch; if(currentPlan) { currentPlan={...currentPlan,masterImage,unifiedPrompt,outlinePrompt}; } } };
})();
