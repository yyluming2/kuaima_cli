(function () {
  const $ = (selector) => document.querySelector(selector);
  const STORAGE_KEY = "kuaima.webui.agent.chats.v1";
  const HISTORY_LIMIT = 50;

  const state = {
    history: [],
    chats: [],
    activeChatId: "",
    attachments: [],
    activeController: null,
    pendingAssistant: null,
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
    state.activeChatId = chat ? chat.id : "";
    state.history = chat ? chat.messages : [];
    renderChatHistory(); renderChat();
  }
  function startNewChat() {
    if (state.activeController) state.activeController.abort();
    clearAttachments();
    $("#agentPrompt").value = "";
    state.activeChatId = ""; state.history = []; state.pendingAssistant = null;
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
    return (
      '<div class="chat-message ' + esc(role) + '" data-message-index="' + index + '">' +
        '<div class="chat-meta"><div class="chat-role">' + esc(role === "assistant" ? "assistant" : role) + '</div><time class="chat-time">' + esc(formatTime(message.createdAt)) + '</time></div>' +
        (textParts.length ? '<div class="chat-text">' + esc(textParts.join("\n")) + '</div>' : "") +
        (imageParts.length ? '<div class="chat-images">' + imageParts.map((part) => (
          '<button class="chat-image" type="button" data-preview-image="' + esc(part.image_url) + '">' +
          '<img src="' + esc(part.image_url) + '" alt="图片预览"></button>'
        )).join("") + '</div>' : "") +
        (outputImages.length ? '<div class="chat-images">' + outputImages.map((url) => (
          '<button class="chat-image" type="button" data-preview-image="' + esc(url) + '">' +
          '<img src="' + esc(url) + '" alt="图片预览"></button>'
        )).join("") + '</div>' : "") +
      '</div>'
    );
  }

  function appendPendingAssistant() {
    const log = $("#agentChatLog");
    if (!log) {
      return null;
    }
    const el = document.createElement("div");
    el.className = "chat-message assistant";
    el.innerHTML = '<div class="chat-meta"><div class="chat-role">assistant</div><time class="chat-time">' + esc(formatTime(Date.now())) + '</time></div><div class="chat-text"><span class="chat-waiting" aria-label="正在等待回复"><i></i><i></i><i></i></span></div>';
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
    }
    const log = $("#agentChatLog");
    if (log) {
      log.scrollTop = log.scrollHeight;
    }
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

    const userMessage = buildUserMessage(prompt);
    state.history.push(userMessage);
    persistCurrentChat();
    appendMessage(userMessage);
    $("#agentPrompt").value = "";
    clearAttachments();

    const assistantMessage = { role: "assistant", content: [], createdAt: nowISO() };
    state.pendingAssistant = appendPendingAssistant();
    const controller = new AbortController();
    state.activeController = controller;
    setStreaming(true);

    let responseText = "";
    const responseImages = [];
    try {
      await apiStream("/api/agent/stream", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ messages: state.history.map((item) => ({ role: item.role, content: item.content })) }),
        signal: controller.signal,
      }, {
        status(payload) {
          if (payload.message && state.pendingAssistant) {
            state.pendingAssistant.querySelector(".chat-waiting")?.setAttribute("aria-label", payload.message);
          }
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
        },
        error(payload) {
          throw new Error(payload.error || "agent failed");
        },
      });
      assistantMessage.content = responseText ? [{ type: "output_text", text: responseText }] : [];
      assistantMessage.images = responseImages;
      state.history.push(assistantMessage);
      persistCurrentChat();
    } catch (error) {
      if (error && error.name === "AbortError") {
        if (state.pendingAssistant) {
          state.pendingAssistant.remove();
        }
      } else {
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
    const button = event.target.closest("[data-mode-tab]");
    if (!button) {
      return;
    }
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
  loadChats();
  if (state.chats.length) activateChat(state.chats[0].id);
  renderMode("generate");
  renderAttachmentList();
  renderChatHistory();
  renderChat();
})();
