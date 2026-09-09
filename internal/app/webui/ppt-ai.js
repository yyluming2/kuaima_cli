(() => {
  const $ = (id) => document.getElementById(id);
  const modal = $("pptAiModal"), form = $("pptAiForm"), outline = $("pptAiOutline");
  let currentPlan = null;
  const readImages = async () => Promise.all([...($("pptAiImages").files || [])].slice(0, 8).map(file => new Promise(resolve => { const r = new FileReader(); r.onload = () => resolve(r.result); r.onerror = () => resolve(""); r.readAsDataURL(file); })));
  const show = (v) => { modal.hidden = !v; if (v) { form.hidden = false; outline.hidden = true; } };
  setTimeout(() => {
    const bar = document.querySelector(".bar"), entry = document.querySelector(".ppt-ai-entry"), button = $("pptAiOpen");
    if (bar && button) { bar.appendChild(button); entry?.remove(); [...bar.querySelectorAll("input")].filter(x => x.placeholder === "主题" || x.placeholder === "风格").forEach(x => x.remove()); }
    const slides = document.querySelector("#slides");
    if (slides && !document.querySelector(".ppt-page-actions")) { const actions = document.createElement("div"); actions.className = "ppt-page-actions"; actions.innerHTML = '<button data-page-action="add">＋ 新增</button><button data-page-action="duplicate">复制</button><button data-page-action="delete">删除</button>'; actions.onclick = ev => { const action = ev.target.closest("[data-page-action]")?.dataset.pageAction; if (action) window.dispatchEvent(new CustomEvent("ppt:page", { detail: action })); }; slides.parentElement?.insertBefore(actions, slides); }
  }, 80);
  $("pptAiOpen").onclick = () => show(true); $("pptAiClose").onclick = () => show(false); $("pptAiCancel").onclick = () => show(false);
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
