(function () {
  const $ = (selector) => document.querySelector(selector);
  const STORAGE_KEY = "kuaima.webui.agent.chats.v1";
  const MODE_STORAGE_KEY = "kuaima.webui.workflow-tab.v1";
  const MODEL_STORAGE_KEY = "kuaima.webui.agent.model.v1";
  const HISTORY_LIMIT = 50;

  async function loadAgentModels() {
    const select = $("#agentModel");
    const status = $("#agentModelStatus");
    try {
      const payload = await api("/api/ipricing");
      const models = payload && payload.data && payload.data.agent;
      const available = Array.isArray(models) ? models.filter((item) => item && typeof item.model_name === "string" && item.model_name.trim()) : [];
      if (!available.length) throw new Error("暂无可用的 agent 模型");
      select.innerHTML = available.map((item) => '<option value="' + esc(item.model_name) + '" title="' + esc(item.description || "") + '">' + esc(item.model_name) + '</option>').join("");
      let previous = "";
      try { previous = localStorage.getItem(MODEL_STORAGE_KEY) || ""; } catch (_) {}
      if (available.some((item) => item.model_name === previous)) select.value = previous;
      select.disabled = false;
      status.hidden = true;
    } catch (error) {
      select.innerHTML = '<option value="">模型不可用</option>';
      status.textContent = (error.message || "模型加载失败") + "，请刷新页面重试。";
      status.hidden = false;
    }
  }

  const state = {
    history: [],
    chats: [],
    activeChatId: "",
    attachments: [],
    activeController: null,
    pendingAssistant: null,
    editingIndex: -1,
  };

  function nowISO() { return new Date().toISOString(); }
  function newChat() {
    const now = nowISO();
    return { id: "chat-" + Date.now() + "-" + Math.random().toString(36).slice(2, 8), title: "新对话", createdAt: now, updatedAt: now, messages: [] };
  }
  function loadChats() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]");
      state.chats = Array.isArray(saved) ? saved.filter((chat) => chat && Array.isArray(chat.messages)).slice(0, HISTORY_LIMIT) : [];
    } catch (_) { state.chats = []; }
  }
  function saveChats() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state.chats.slice(0, HISTORY_LIMIT))); } catch (_) {}
  }
  function currentChat() { return state.chats.find((chat) => chat.id === state.activeChatId) || null; }
  function formatTime(value, withDate) {
    const date = new Date(value || Date.now());
    if (Number.isNaN(date.getTime())) return "";
    return new Intl.DateTimeFormat("zh-CN", withDate ? { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit" }).format(date);
  }
  function deriveTitle(messages) {
    const first = messages.find((item) => item.role === "user");
    const part = first && (first.content || []).find((item) => item.type === "input_text" && item.text);
    return part ? part.text.trim().replace(/\s+/g, " ").slice(0, 24) : "图片对话";
  }
  function renderChatHistory() {
    const list = $("#agentHistoryList");
    if (!list) return;
    if (!state.chats.length) { list.innerHTML = '<div class="agent-history-empty">暂无对话</div>'; return; }
    list.innerHTML = state.chats.map((chat) => '<div class="agent-history-item' + (chat.id === state.activeChatId ? " active" : "") + '">' +
      '<button class="agent-history-open" type="button" data-agent-chat-id="' + esc(chat.id) + '"><strong>' + esc(chat.title || "新对话") + '</strong><small>' + esc(formatTime(chat.updatedAt, true)) + '</small></button>' +
      '<button class="icon-btn" type="button" data-agent-delete-chat="' + esc(chat.id) + '" aria-label="删除对话">' + iconHTML("trash-2") + '</button></div>').join("");
  }
  function activateChat(id) {
    const chat = state.chats.find((item) => item.id === id);
    state.editingIndex = -1;
    clearAttachments();
    state.activeChatId = chat ? chat.id : "";
    state.history = chat ? chat.messages : [];
    renderChatHistory(); renderChat();
  }
  function startNewChat() {
    if (state.activeController) state.activeController.abort();
    clearAttachments();
    $("#agentPrompt").value = "";
    state.activeChatId = ""; state.history = []; state.pendingAssistant = null; state.editingIndex = -1;
    renderChatHistory(); renderChat();
  }
  function persistCurrentChat() {
    let chat = currentChat();
    if (!chat) {
      chat = newChat(); state.activeChatId = chat.id; state.chats.unshift(chat);
    }
    chat.messages = state.history;
    chat.title = deriveTitle(chat.messages);
    chat.updatedAt = nowISO();
    state.chats = [chat].concat(state.chats.filter((item) => item.id !== chat.id)).slice(0, HISTORY_LIMIT);
    saveChats(); renderChatHistory();
  }

  function renderMode(mode) {
    mode = ["generate", "video", "agent"].includes(mode) ? mode : "generate";
    document.querySelectorAll("[data-mode-tab]").forEach((button) => {
      let active = button.dataset.modeTab === mode;
      if (active && mode === "generate" && button.dataset.workflowId != null) {
        const workflowId = $("#workflowId") ? $("#workflowId").value : "single";
        active = String(button.dataset.workflowId || "single") === String(workflowId || "single");
      }
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", String(active));
    });
    document.querySelectorAll("[data-mode-pane]").forEach((pane) => {
      pane.hidden = pane.dataset.modePane !== mode;
    });
    const generationStatus = $("#genStatus");
    if (generationStatus) {
      generationStatus.textContent = mode === "video" ? "准备生成视频" : mode === "agent" ? "对话模式" : "准备生成图片";
      generationStatus.className = "status gen-status";
    }
    try { localStorage.setItem(MODE_STORAGE_KEY, mode); } catch (_) {}
  }

  function renderAttachmentList() {
    const list = $("#agentAttachmentList");
    if (!list) {
      return;
    }
    if (state.attachments.length === 0) {
      list.innerHTML = "";
      return;
    }
    list.innerHTML = state.attachments.map((item, index) => (
      '<div class="agent-attachment">' +
        '<img src="' + esc(item.url) + '" alt="' + esc(item.name || "image") + '">' +
        '<button type="button" class="icon-btn" data-agent-remove-attachment="' + index + '" aria-label="移除图片">' +
        iconHTML("x") + '</button>' +
      '</div>'
    )).join("");
  }

  function renderChat() {
    const log = $("#agentChatLog");
    if (!log) {
      return;
    }
    if (state.history.length === 0) {
      log.innerHTML = "";
      return;
    }
    log.innerHTML = state.history.map((item, index) => renderMessageHTML(item, index)).join("");
    log.scrollTop = log.scrollHeight;
  }

  function renderMessageHTML(message, index) {
    const role = String(message.role || "assistant");
    const parts = Array.isArray(message.content) ? message.content : [];
    const textParts = parts.filter((part) => part && (part.type === "input_text" || part.type === "output_text") && part.text).map((part) => part.text);
    const imageParts = parts.filter((part) => part && part.type === "input_image" && part.image_url);
    const outputImages = (Array.isArray(message.images) ? message.images : []).filter(Boolean);
	const outputVideos = (Array.isArray(message.videos) ? message.videos : []).filter((item) => item && (item.url || item.task_id));
    return (
      '<div class="chat-message ' + esc(role) + '" data-message-index="' + index + '">' +
        '<div class="chat-meta"><div class="chat-role">' + esc(role === "assistant" ? "assistant" : role) + '</div><time class="chat-time">' + esc(formatTime(message.createdAt)) + '</time>' +
        (role === "user" ? '<button class="chat-edit-button" type="button" data-agent-edit-message="' + index + '" aria-label="编辑并重新发送" title="编辑并重新发送">' + iconHTML("pencil") + '</button>' : '') + '</div>' +
        (textParts.length ? '<div class="chat-text">' + esc(textParts.join("\n")) + '</div>' : "") +
        (imageParts.length ? '<div class="chat-images">' + imageParts.map((part) => (
          '<button class="chat-image" type="button" data-preview-image="' + esc(part.image_url) + '">' +
          '<img src="' + esc(part.image_url) + '" alt="图片预览"></button>'
        )).join("") + '</div>' : "") +
        (outputImages.length ? '<div class="chat-images">' + outputImages.map((url) => (
          '<button class="chat-image" type="button" data-preview-image="' + esc(url) + '">' +
          '<img src="' + esc(url) + '" alt="图片预览"></button>'
        )).join("") + '</div>' : "") +
        (outputVideos.length ? '<div class="chat-videos">' + outputVideos.map((item) => item.url ?
          '<video controls src="' + esc(item.url) + '"></video>' :
          '<div class="chat-video-task" data-video-task-id="' + esc(item.task_id) + '">' + esc(videoTaskLabel(item)) + '</div>').join("") + '</div>' : "") +
      '</div>'
    );
  }

  function videoTaskLabel(task) {
    const status = String(task && task.status || "").toLowerCase();
    if (["failed", "cancelled", "canceled"].includes(status)) {
      return "视频生成失败：" + (task.error && task.error.message || "服务未提供详细原因");
    }
    if (["processing", "in_progress"].includes(status)) return "视频生成中…";
    return "视频任务已提交：" + (task.task_id || "等待任务编号");
  }

  function appendPendingAssistant() {
    const log = $("#agentChatLog");
    if (!log) {
      return null;
    }
    const el = document.createElement("div");
    el.className = "chat-message assistant";
    el.innerHTML = '<div class="chat-meta"><div class="chat-role">assistant</div><time class="chat-time">' + esc(formatTime(Date.now())) + '</time></div>' +
      '<div class="chat-text" hidden></div>' +
      '<div class="chat-stream-status"><span class="chat-waiting" aria-label="正在等待回复"><i></i><i></i><i></i></span><span class="chat-stream-status-text">正在等待回复...</span></div>';
    log.appendChild(el);
    log.scrollTop = log.scrollHeight;
    return el;
  }

  function setPendingAssistantText(text) {
    if (!state.pendingAssistant) {
      state.pendingAssistant = appendPendingAssistant();
    }
    if (!state.pendingAssistant) {
      return;
    }
    const textEl = state.pendingAssistant.querySelector(".chat-text");
    if (textEl) {
      textEl.textContent = text || "";
      textEl.hidden = !text;
    }
    const log = $("#agentChatLog");
    if (log) {
      log.scrollTop = log.scrollHeight;
    }
  }

  function setPendingAssistantStatus(text) {
    if (!state.pendingAssistant) state.pendingAssistant = appendPendingAssistant();
    if (!state.pendingAssistant) return;
    const status = state.pendingAssistant.querySelector(".chat-stream-status");
    const label = state.pendingAssistant.querySelector(".chat-stream-status-text");
    if (status) status.hidden = !text;
    if (label) label.textContent = text || "";
    state.pendingAssistant.querySelector(".chat-waiting")?.setAttribute("aria-label", text || "");
  }

  function appendPendingAssistantImage(url) {
    if (!url) {
      return;
    }
    if (!state.pendingAssistant) {
      state.pendingAssistant = appendPendingAssistant();
    }
    if (!state.pendingAssistant) {
      return;
    }
    let imagesEl = state.pendingAssistant.querySelector(".chat-images");
    if (!imagesEl) {
      imagesEl = document.createElement("div");
      imagesEl.className = "chat-images";
      state.pendingAssistant.appendChild(imagesEl);
    }
    if (Array.from(imagesEl.querySelectorAll("img")).some((img) => img.getAttribute("src") === url)) {
      return;
    }
    const holder = document.createElement("button");
    holder.className = "chat-image";
    holder.type = "button";
    holder.dataset.previewImage = url;
    holder.innerHTML = '<img src="' + esc(url) + '" alt="图片预览">';
    imagesEl.appendChild(holder);
    const log = $("#agentChatLog");
    if (log) {
      log.scrollTop = log.scrollHeight;
    }
  }

  function appendPendingAssistantVideo(task) {
    if (!task || (!task.task_id && !task.url)) return;
    if (!state.pendingAssistant) state.pendingAssistant = appendPendingAssistant();
    let holder = state.pendingAssistant.querySelector(".chat-videos");
    if (!holder) { holder = document.createElement("div"); holder.className = "chat-videos"; state.pendingAssistant.appendChild(holder); }
    const item = document.createElement(task.url ? "video" : "div");
    if (task.url) { item.controls = true; item.src = task.url; }
    else { item.className = "chat-video-task"; item.dataset.videoTaskId = task.task_id; item.textContent = "视频任务已提交：" + task.task_id; }
    holder.appendChild(item);
  }

  async function pollAgentVideo(task) {
    if (!task || !task.task_id || task.url) return;
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, 10000));
      const next = await api("/api/video/status/" + encodeURIComponent(task.task_id));
      Object.assign(task, next);
      const status = String(task.status || "").toLowerCase();
	  const pending = document.querySelector('[data-video-task-id="' + CSS.escape(task.task_id) + '"]');
      if (pending) pending.textContent = status === "processing" || status === "in_progress" ? "视频生成中…" : "视频任务状态：" + (task.status || "queued");
      if (["completed", "succeeded"].includes(status)) {
        if (pending && task.url) { const video = document.createElement("video"); video.controls = true; video.src = task.url; pending.replaceWith(video); }
		persistCurrentChat();
        return;
      }
      if (["failed", "cancelled", "canceled"].includes(status)) throw new Error(task.error && task.error.message || "视频生成失败");
    }
  }

  function clearAttachments() {
    state.attachments = [];
    const input = $("#agentImageInput");
    if (input) {
      input.value = "";
    }
    renderAttachmentList();
  }

  async function addAttachments(files) {
    const list = Array.from(files || []);
    if (list.length === 0) {
      return;
    }
    const form = new FormData();
    for (const file of list) {
      if (!file || !file.type || !file.type.startsWith("image/")) {
        continue;
      }
      form.append("images", file, file.name || "image");
    }
    if (!Array.from(form.keys()).length) {
      return;
    }
    const resp = await fetch("/api/agent/upload", {
      method: "POST",
      body: form,
    });
    const payload = await resp.json().catch(() => ({}));
    if (!resp.ok) {
      throw new Error(payload.error || "upload failed");
    }
    const uploaded = Array.isArray(payload.images) ? payload.images : [];
    state.attachments = state.attachments.concat(uploaded.map((item, index) => ({
      name: list[index] && list[index].name || "image",
      url: item.url,
    })).filter((item) => item.url));
    renderAttachmentList();
  }

  function buildUserMessage(text) {
    const content = [];
    if (text) {
      content.push({ type: "input_text", text });
    }
    state.attachments.forEach((item) => {
      content.push({ type: "input_image", image_url: item.url });
    });
    return { role: "user", content, createdAt: nowISO() };
  }

  function requestMessages() {
    return state.history.map((item) => ({
      role: item && item.role || "user",
      content: Array.isArray(item && item.content) ? item.content.filter((part) => part && part.type) : [],
    })).filter((item) => item.content.length > 0);
  }

  function appendMessage(message) {
    const log = $("#agentChatLog");
    if (!log) {
      return;
    }
    const holder = document.createElement("div");
    holder.innerHTML = renderMessageHTML(message, Math.max(state.history.length - 1, 0));
    log.appendChild(holder.firstElementChild);
    log.scrollTop = log.scrollHeight;
  }

  function setStreaming(streaming) {
    const cancel = $("#cancelAgentSend");
    const send = $("#agentForm button[type=\"submit\"]");
    if (cancel) {
      cancel.hidden = !streaming;
    }
    if (send) {
      send.disabled = streaming;
    }
  }

  async function submitAgentMessage(event) {
    event.preventDefault();
    if (state.activeController) {
      return;
    }
    const prompt = ($("#agentPrompt") && $("#agentPrompt").value || "").trim();
    if (!prompt && state.attachments.length === 0) {
      return;
    }
    const model = $("#agentModel").value;
    if (!model) {
      const status = $("#agentModelStatus");
      status.textContent = "请等待 agent 模型加载完成；加载失败时请刷新页面重试。";
      status.hidden = false;
      return;
    }

    const isEditing = state.editingIndex >= 0 && state.editingIndex < state.history.length;
    if (isEditing) {
      state.history = state.history.slice(0, state.editingIndex);
    }
    state.editingIndex = -1;
    const userMessage = buildUserMessage(prompt);
    state.history.push(userMessage);
    persistCurrentChat();
    if (isEditing) renderChat(); else appendMessage(userMessage);
    $("#agentPrompt").value = "";
    clearAttachments();

    const assistantMessage = { role: "assistant", content: [], createdAt: nowISO() };
    state.pendingAssistant = appendPendingAssistant();
    const controller = new AbortController();
    state.activeController = controller;
    setStreaming(true);

    let responseText = "";
    const responseImages = [];
	const responseVideos = [];
    try {
      await apiStream("/api/agent/stream", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ model, messages: requestMessages() }),
        signal: controller.signal,
      }, {
        status(payload) {
          if (payload.message) setPendingAssistantStatus(payload.message);
        },
        text(payload) {
          if (payload.text) {
            responseText += payload.text;
            setPendingAssistantText(responseText);
          }
        },
        image(payload) {
          if (payload.url && !responseImages.includes(payload.url)) {
            responseImages.push(payload.url);
            appendPendingAssistantImage(payload.url);
          }
        },
		video(payload) {
		  if (payload && payload.task_id && !responseVideos.some((item) => item.task_id === payload.task_id)) {
			responseVideos.push(payload); appendPendingAssistantVideo(payload);
			pollAgentVideo(payload).catch((error) => {
			  const pending = document.querySelector('[data-video-task-id="' + CSS.escape(payload.task_id) + '"]');
			  if (pending) pending.textContent = "视频生成失败: " + error.message;
			  persistCurrentChat();
			});
		  }
		},
        done(payload) {
          if (payload && payload.text) {
            responseText = payload.text;
            setPendingAssistantText(responseText);
          }
          (payload && payload.images || []).forEach((url) => {
            if (url && !responseImages.includes(url)) {
              responseImages.push(url);
              appendPendingAssistantImage(url);
            }
          });
		  (payload && payload.videos || []).forEach((task) => {
			if (task && !responseVideos.some((item) => item.task_id === task.task_id)) { responseVideos.push(task); appendPendingAssistantVideo(task); }
		  });
          setPendingAssistantStatus("");
        },
        error(payload) {
          throw new Error(payload.error || "agent failed");
        },
      });
      assistantMessage.content = responseText ? [{ type: "output_text", text: responseText }] : [];
      assistantMessage.images = responseImages;
	  assistantMessage.videos = responseVideos;
      if (!responseText && responseImages.length === 0 && responseVideos.length === 0) {
        responseText = "本次请求已结束，但服务没有返回可显示的内容。";
        assistantMessage.content = [{ type: "output_text", text: responseText }];
        setPendingAssistantText(responseText);
      }
      state.history.push(assistantMessage);
      persistCurrentChat();
    } catch (error) {
      if (error && error.name === "AbortError") {
        if (state.pendingAssistant) {
          state.pendingAssistant.remove();
        }
      } else {
        setPendingAssistantStatus("");
        setPendingAssistantText("错误: " + (error && error.message ? error.message : "agent failed"));
        assistantMessage.content = [{ type: "output_text", text: "错误: " + (error && error.message ? error.message : "agent failed") }];
        state.history.push(assistantMessage);
        persistCurrentChat();
      }
    } finally {
      state.pendingAssistant = null;
      state.activeController = null;
      setStreaming(false);
    }
  }

  document.addEventListener("click", (event) => {
    const cancelEdit = event.target.closest("[data-agent-cancel-edit]");
    if (cancelEdit) {
      state.editingIndex = -1;
      renderChat();
      return;
    }
    const resendEdit = event.target.closest("[data-agent-resend-edit]");
    if (resendEdit) {
      if (state.activeController) return;
      const index = Number(resendEdit.dataset.agentResendEdit);
      const message = state.history[index];
      const editor = resendEdit.closest(".chat-message")?.querySelector(".chat-inline-editor");
      if (!message || message.role !== "user" || !editor) return;
      state.editingIndex = index;
      state.attachments = (message.content || []).filter((part) => part && part.type === "input_image" && part.image_url).map((part) => ({ name: "已附加图片", url: part.image_url }));
      $("#agentPrompt").value = editor.value;
      $("#agentForm").requestSubmit();
      return;
    }
    const edit = event.target.closest("[data-agent-edit-message]");
    if (edit) {
      if (state.activeController) return;
      const index = Number(edit.dataset.agentEditMessage);
      const message = state.history[index];
      if (!message || message.role !== "user") return;
      const parts = Array.isArray(message.content) ? message.content : [];
      const text = parts.filter((part) => part && part.type === "input_text").map((part) => part.text || "").join("\n");
      renderChat();
      const holder = $("#agentChatLog").querySelector('[data-message-index="' + index + '"]');
      const textEl = holder && holder.querySelector(".chat-text");
      if (!holder) return;
      state.editingIndex = index;
      const editor = document.createElement("textarea");
      editor.className = "chat-inline-editor";
      editor.value = text;
      editor.rows = Math.max(2, Math.min(10, text.split("\n").length + 1));
      if (textEl) textEl.replaceWith(editor); else holder.querySelector(".chat-meta").insertAdjacentElement("afterend", editor);
      const actions = document.createElement("div");
      actions.className = "chat-inline-actions";
      actions.innerHTML = '<button class="btn" type="button" data-agent-cancel-edit>取消</button><button class="btn primary" type="button" data-agent-resend-edit="' + index + '">重新发送</button>';
      holder.appendChild(actions);
      editor.focus();
      editor.setSelectionRange(text.length, text.length);
      return;
    }
    const button = event.target.closest("[data-mode-tab]");
    if (!button) {
      return;
    }
    try { localStorage.setItem(MODE_STORAGE_KEY, button.dataset.modeTab); } catch (_) {}
    renderMode(button.dataset.modeTab);
  });

  $("#agentImageInput").addEventListener("change", async (event) => {
    try {
      await addAttachments(Array.from(event.target.files || []));
    } catch (error) {
      setPendingAssistantText("错误: " + (error && error.message ? error.message : "upload failed"));
    } finally {
      event.target.value = "";
    }
  });

  $("#agentAttachmentList").addEventListener("click", (event) => {
    const button = event.target.closest("[data-agent-remove-attachment]");
    if (!button) {
      return;
    }
    const index = Number(button.dataset.agentRemoveAttachment);
    if (!Number.isFinite(index) || index < 0 || index >= state.attachments.length) {
      return;
    }
    state.attachments.splice(index, 1);
    renderAttachmentList();
  });

  $("#agentForm").addEventListener("submit", submitAgentMessage);

  $("#clearAgentChat").addEventListener("click", () => {
    if ((state.history.length > 0 || state.activeChatId) && !window.confirm("确定要清空当前对话吗？清空后无法恢复。")) {
      return;
    }
    if (state.activeController) {
      state.activeController.abort();
    }
    const chat = currentChat();
    if (chat) state.chats = state.chats.filter((item) => item.id !== chat.id);
    state.activeChatId = "";
    state.history = [];
    state.editingIndex = -1;
    state.pendingAssistant = null;
    state.activeController = null;
    setStreaming(false);
    clearAttachments();
    $("#agentPrompt").value = "";
    renderChat();
    saveChats(); renderChatHistory();
  });

  $("#newAgentChat").addEventListener("click", startNewChat);
  $("#agentHistoryList").addEventListener("click", (event) => {
    const open = event.target.closest("[data-agent-chat-id]");
    if (open) { activateChat(open.dataset.agentChatId); return; }
    const remove = event.target.closest("[data-agent-delete-chat]");
    if (!remove) return;
    const id = remove.dataset.agentDeleteChat;
    const chat = state.chats.find((item) => item.id === id);
    const title = chat && chat.title ? chat.title : "这条对话";
    if (!window.confirm("确定要删除“" + title + "”吗？删除后无法恢复。")) return;
    state.chats = state.chats.filter((chat) => chat.id !== id);
    saveChats();
    if (id === state.activeChatId) startNewChat(); else renderChatHistory();
  });

  $("#cancelAgentSend").addEventListener("click", () => {
    if (state.activeController) {
      state.activeController.abort();
    }
  });

  $("#agentPrompt").addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      $("#agentForm").requestSubmit();
    }
  });

  window.KuaimaAgent = { addAttachments };
  $("#agentModel").addEventListener("change", (event) => {
    try { localStorage.setItem(MODEL_STORAGE_KEY, event.target.value); } catch (_) {}
  });
  loadAgentModels();
  loadChats();
  if (state.chats.length) activateChat(state.chats[0].id);
  let initialMode = "generate";
  try { initialMode = localStorage.getItem(MODE_STORAGE_KEY) || initialMode; } catch (_) {}
  renderMode(initialMode);
  renderAttachmentList();
  renderChatHistory();
  renderChat();
})();
