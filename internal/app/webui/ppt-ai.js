(() => {
  const $ = (id) => document.getElementById(id);
  const modal = $("pptAiModal"), form = $("pptAiForm"), outline = $("pptAiOutline"), styleStep = $("pptAiStyleStep"), progress = $("pptAiProgress");
  let currentPlan = null;
  const loadTextModels = async () => {
    try {
      const payload = await fetch("/api/ipricing").then(r => r.json());
      const models = payload?.data?.text || [];
      const select = $("pptAiTextModel");
      if (!models.length) { select.innerHTML = '<option value="">使用默认模型</option>'; return; }
      select.innerHTML = models.filter(m => m && m.model_name).map(m => `<option value="${String(m.model_name).replace(/[&<>\"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]))}">${m.model_name}</option>`).join("");
    } catch (_) { $("pptAiTextModel").innerHTML = '<option value="">使用默认模型</option>'; }
  };
  const readImages = async () => Promise.all([...($("pptAiImages").files || [])].slice(0, 8).map(file => new Promise(resolve => { const r = new FileReader(); r.onload = () => resolve(r.result); r.onerror = () => resolve(""); r.readAsDataURL(file); })));
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
      [["sparkles", "AI 重新生成本页", "ai"], ["image", "重新生成本页配图", "image"], ["plus", "新增页面", "add"], ["copy", "复制页面", "duplicate"], ["trash-2", "删除页面（不可撤销）", "delete"]].forEach(([icon, title, action]) => { const b = document.createElement("button"); b.type = "button"; b.title = title; b.setAttribute("aria-label", title); b.innerHTML = `<i data-lucide="${icon}"></i>`; b.onclick = ev => { ev.stopPropagation(); if (action === "delete" && !window.confirm("删除此页后不可撤销，确定继续吗？")) return; const index = [...document.querySelectorAll("#slides .thumb")].indexOf(thumb); window.dispatchEvent(new CustomEvent(action === "ai" ? "ppt:ai-page" : action === "image" ? "ppt:image-page" : "ppt:page", { detail: action === "ai" || action === "image" ? index : action })); }; actions.appendChild(b); });
      window.lucide?.createIcons?.();
      label.appendChild(actions);
    });
  }, 250);
  $("pptAiOpen").onclick = () => show(true); $("pptAiClose").onclick = () => show(false); $("pptAiCancel").onclick = () => show(false);
  // 打开 PPT 页面时直接进入 AI 制作流程。
  setTimeout(() => show(true), 0);
  loadTextModels();
  $("pptAiStyleNext").onclick = () => { readOutline(); styleStep.hidden = false; outline.hidden = true; const p = currentPlan.pages[0] || {}; $("pptAiSample").querySelector(".sample-title").textContent = p.title || currentPlan.title; };
  $("pptAiStyleBack").onclick = () => { styleStep.hidden = true; outline.hidden = false; };
  const refreshSample = () => { const s = $("pptAiSample"); s.style.background = $("pptAiBackground").value; s.style.setProperty("--sample-accent", $("pptAiAccent").value); };
  ["pptAiAccent", "pptAiBackground"].forEach(id => $(id).oninput = refreshSample);
  $("pptAiStyleImage").onchange = () => { const f = $("pptAiStyleImage").files[0]; $("pptAiStyleHint").textContent = f?.name || "未选择"; };
  $("pptAiImages").onchange = () => { $("pptAiImageHint").textContent = `${$("pptAiImages").files.length} 张图片`; };
  $("pptAiDeck").onchange = () => { $("pptAiDeckHint").textContent = $("pptAiDeck").files[0]?.name || "可选"; };
  $("pptAiPlan").onclick = async () => {
    const title = $("pptAiTitle").value.trim() || "未命名主题", brief = $("pptAiBrief").value.trim();
    const button = $("pptAiPlan"); button.disabled = true; button.textContent = "正在整理大纲…"; progressState("正在连接文本模型并生成大纲");
    try {
      const refs = [brief].filter(Boolean); const deck = $("pptAiDeck").files[0]; if (deck) refs.push(`参考 PPT：${deck.name}`);
      const images = await readImages();
      const pageCount = Math.min(50, Math.max(3, Number($("pptAiPages").value) || 5)); $("pptAiPages").value = pageCount;
      const res = await fetch("/api/ppt/plan?stream=1", { method:"POST", headers:{"Content-Type":"application/json", "Accept":"text/event-stream"}, body:JSON.stringify({ title, style:$("pptAiStyle").value.trim(), textModel:$("pptAiTextModel").value, pageCount, references:refs, images }) });
      if (!res.ok) throw new Error(await res.text()); currentPlan = await readPlanStream(res); if (!currentPlan) throw new Error("模型未返回完整大纲"); renderOutline(); form.hidden = true; progress.hidden = true; outline.hidden = false;
    } catch (e) { alert("大纲生成失败：" + e.message); form.hidden = false; progress.hidden = true; } finally { button.disabled = false; button.textContent = "生成大纲"; }
  };
  $("pptAiGenerate").onclick = async () => {
    if (!currentPlan) return; readOutline(); const button = $("pptAiGenerate"); button.disabled = true; button.textContent = "正在生成…";
    currentPlan.accentColor = $("pptAiAccent").value; currentPlan.backgroundColor = $("pptAiBackground").value; currentPlan.unifiedBackground = $("pptAiUnified").checked; currentPlan.styleReference = await readOne($("pptAiStyleImage").files[0]);
    progressState("已确认风格，正在生成全部页面");
    try {
      const res = await fetch("/api/ppt/generate", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(currentPlan) });
      if (!res.ok) throw new Error(await res.text());
      const textDeck = await res.json();
      // Commit text/layout only. Image generation is intentionally manual:
      // users can retry a single slide from its "重新生成本页配图" action.
      window.dispatchEvent(new CustomEvent("ppt:generated", { detail: textDeck }));
      show(false);
    } catch (e) { alert("PPT 文字生成失败：" + e.message); styleStep.hidden = false; progress.hidden = true; } finally { button.disabled = false; button.textContent = "下一步"; }
  };
})();
