(() => {
  const $ = (id) => document.getElementById(id);
  const modal = $("pptAiModal"), form = $("pptAiForm"), outline = $("pptAiOutline");
  let currentPlan = null;
  const readImages = async () => Promise.all([...($("pptAiImages").files || [])].slice(0, 8).map(file => new Promise(resolve => { const r = new FileReader(); r.onload = () => resolve(r.result); r.onerror = () => resolve(""); r.readAsDataURL(file); })));
  const show = (v) => { modal.hidden = !v; if (v) { form.hidden = false; outline.hidden = true; } };
  setTimeout(() => {
    const bar = document.querySelector(".bar"), entry = document.querySelector(".ppt-ai-entry"), button = $("pptAiOpen");
    if (bar && button) { bar.appendChild(button); entry?.remove(); [...bar.querySelectorAll("input")].filter(x => x.placeholder === "主题" || x.placeholder === "风格").forEach(x => x.remove()); }
  }, 80);
  setInterval(() => {
    document.querySelectorAll("#slides .thumb").forEach(thumb => {
      if (thumb.querySelector(".thumb-actions")) return;
      const label = thumb.querySelector(".thumb-label"); if (!label) return;
      const actions = document.createElement("span"); actions.className = "thumb-actions";
      [["plus", "新增页面", "add"], ["copy", "复制页面", "duplicate"], ["trash-2", "删除页面（不可撤销）", "delete"]].forEach(([icon, title, action]) => { const b = document.createElement("button"); b.type = "button"; b.title = title; b.setAttribute("aria-label", title); b.innerHTML = `<i data-lucide="${icon}"></i>`; b.onclick = ev => { ev.stopPropagation(); if (action === "delete" && !window.confirm("删除此页后不可撤销，确定继续吗？")) return; window.dispatchEvent(new CustomEvent("ppt:page", { detail: action })); }; actions.appendChild(b); });
      window.lucide?.createIcons?.();
      label.appendChild(actions);
    });
  }, 250);
  $("pptAiOpen").onclick = () => show(true); $("pptAiClose").onclick = () => show(false); $("pptAiCancel").onclick = () => show(false);
  // 打开 PPT 页面时直接进入 AI 制作流程。
  setTimeout(() => show(true), 0);
  $("pptAiBack").onclick = () => { form.hidden = false; outline.hidden = true; };
  $("pptAiImages").onchange = () => { $("pptAiImageHint").textContent = `${$("pptAiImages").files.length} 张图片`; };
  $("pptAiDeck").onchange = () => { $("pptAiDeckHint").textContent = $("pptAiDeck").files[0]?.name || "可选"; };
  $("pptAiPlan").onclick = async () => {
    const title = $("pptAiTitle").value.trim() || "未命名主题", brief = $("pptAiBrief").value.trim();
    const button = $("pptAiPlan"); button.disabled = true; button.textContent = "正在整理大纲…";
    try {
      const refs = [brief].filter(Boolean); const deck = $("pptAiDeck").files[0]; if (deck) refs.push(`参考 PPT：${deck.name}`);
      const images = await readImages();
      const res = await fetch("/api/ppt/plan", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({ title, style:$("pptAiStyle").value, pageCount:Number($("pptAiPages").value), references:refs, images }) });
      if (!res.ok) throw new Error(await res.text()); currentPlan = await res.json();
      $("pptAiOutlineList").innerHTML = (currentPlan.pages || []).map((p, i) => `<div class="ppt-ai-outline-item"><strong>${i + 1}. ${p.title}</strong><span>${(p.bullets || []).join(" · ")}</span></div>`).join(""); form.hidden = true; outline.hidden = false;
    } catch (e) { alert("大纲生成失败：" + e.message); } finally { button.disabled = false; button.textContent = "生成大纲"; }
  };
  $("pptAiGenerate").onclick = async () => {
    if (!currentPlan) return; const button = $("pptAiGenerate"); button.disabled = true; button.textContent = "正在生成…";
    try { const res = await fetch("/api/ppt/generate", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(currentPlan) }); if (!res.ok) throw new Error(await res.text()); window.dispatchEvent(new CustomEvent("ppt:generated", { detail: await res.json() })); show(false); } catch (e) { alert("PPT 生成失败：" + e.message); } finally { button.disabled = false; button.textContent = "确认并生成 PPT"; }
  };
})();
