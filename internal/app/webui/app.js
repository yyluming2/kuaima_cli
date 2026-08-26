(function () {
  const generationStatus = $("#genStatus");
  if (generationStatus) {
    const collapsedKey = "kuaima.webui.gen-status-collapsed.v1";
    let collapsed = false;
    try { collapsed = localStorage.getItem(collapsedKey) === "1"; } catch (_) {}
    const applyCollapsed = () => {
      generationStatus.classList.toggle("collapsed", collapsed);
      generationStatus.setAttribute("aria-expanded", String(!collapsed));
      generationStatus.title = collapsed ? "点击展开状态信息" : "点击收起状态信息";
    };
    const toggleCollapsed = () => {
      collapsed = !collapsed;
      try { localStorage.setItem(collapsedKey, collapsed ? "1" : "0"); } catch (_) {}
      applyCollapsed();
    };
    generationStatus.addEventListener("click", toggleCollapsed);
    generationStatus.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); toggleCollapsed(); }
    });
    new MutationObserver(applyCollapsed).observe(generationStatus, { attributes: true, attributeFilter: ["class"] });
    applyCollapsed();
  }
  $("#ratioSelect").onchange = updateSize;
  $("#resolutionSelect").onchange = (event) => {
    const select = event.target;
    const group = String((window.KuaimaAccount && window.KuaimaAccount.group) || "default").toLowerCase();
    const next = String(select.value || "").toLowerCase();
    if (next === "4k" && !resolutionCanUse4k(group)) {
      select.value = select.dataset.previousResolution || "2k";
      alert(resolutionUpgradeMessage());
      if (window.KuaimaBilling && typeof window.KuaimaBilling.openPlans === "function") {
        window.setTimeout(() => window.KuaimaBilling.openPlans(), 0);
      }
      updateSize();
      return;
    }
    select.dataset.previousResolution = select.value;
    updateSize();
  };
  document.querySelectorAll("[data-ratio]").forEach((button) => {
    button.onclick = () => {
      $("#ratioSelect").value = button.dataset.ratio;
      updateSize();
    };
  });

  $("#resolutionSelect").dataset.previousResolution = $("#resolutionSelect").value;
  syncSingleGenerationControls();

  $('[name="images"]').onclick = (event) => {
    event.target.value = "";
    setTimeout(syncImageInput, 1000);
  };

  $('[name="images"]').onchange = async (event) => {
    await addReferenceFiles(Array.from(event.target.files || []), "添加");
  };

  $("#genForm").onsubmit = async (event) => {
    event.preventDefault();
    updateSize();
    syncImageInput();
    saveSettings();
    const submitButton = event.target.querySelector('button[type="submit"]');
    const cancelButton = $("#cancelGenerate");
    activeGenerationController = new AbortController();
    if (submitButton) {
      submitButton.disabled = true;
      submitButton.innerHTML = '<span class="spinner small"></span><span>生成中...</span>';
    }
    cancelButton.hidden = false;
    $("#genStatus").textContent = "生成中...";
    $("#genStatus").className = "status gen-status loading";
    try {
      const result = await generateStream(event.target, activeGenerationController.signal);
      const historyError = await saveHistorySafely(result);
      $("#genStatus").textContent = "已保存 " + result.saved.length + " 张" +
        (historyError ? "，历史保存失败：" + historyError : "");
      $("#genStatus").className = "status gen-status";
      loadBalance();
    } catch (error) {
      const canceled = error && error.name === "AbortError";
      if ($("#gallery").querySelector(".generation-placeholder")) {
        renderGalleryMessage(canceled ? "已取消生成" : "生成失败：" + error.message, canceled ? "status" : "status error");
      }
      if (canceled) {
        $("#genStatus").textContent = "已取消生成";
        $("#genStatus").className = "status gen-status";
        return;
      }
      await saveHistorySafely({ images: [] }, { status: "failed", error: error.message });
      handleOperationError(error, $("#genStatus"));
    } finally {
      activeGenerationController = null;
      cancelButton.hidden = true;
      if (submitButton) {
        submitButton.disabled = false;
        const workflowId = $("#workflowId") ? $("#workflowId").value : "single";
        submitButton.innerHTML = iconHTML(String(workflowId || "") === "single" ? "image-plus" : "workflow") +
          '<span>' + (String(workflowId || "") === "single" ? "生成图片" : "运行工作流") + '</span>';
      }
    }
  };

  $("#cancelGenerate").onclick = () => {
    if (activeGenerationController && window.confirm("确定要取消当前运行吗？")) {
      activeGenerationController.abort();
    }
  };

  ["change", "input"].forEach((eventName) => {
    $("#genForm").addEventListener(eventName, (event) => {
      if (event.target.matches("#useOriginalImages")) {
        useOriginalImages = event.target.checked;
        saveSettings();
        return;
      }
      if (event.target.matches("select,input[type='number']")) {
        saveSettings();
      }
    });
  });

  $("#amountPay").onclick = async () => {
    try {
      const typedAmount = Number($("#amount").value || 0);
      const body = typedAmount > 0 ? { amount: typedAmount } : selectedPayment;
      await pay(body || {});
    } catch (error) {
      openPaymentModal('<span class="status error">' + esc(error.message) + '</span>');
    }
  };

  $("#exchangeCodeSubmit").onclick = exchangeCode;
  $("#exchangeCode").addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      exchangeCode();
    }
  });

  document.querySelectorAll("[data-billing-tab]").forEach((button) => {
    button.onclick = () => setRechargeTab(button.dataset.billingTab);
  });

  document.addEventListener("dragstart", (event) => {
    const item = event.target.closest(".image-preview-item");
    if (!item) {
      const historyPreview = event.target.closest(".history-image-preview");
      if (!historyPreview) {
        return;
      }
      draggedHistoryImageURL = historyPreview.dataset.historyDragUrl || historyPreview.dataset.previewImage || "";
      if (event.dataTransfer && draggedHistoryImageURL) {
        event.dataTransfer.effectAllowed = "copy";
        event.dataTransfer.setData("text/plain", draggedHistoryImageURL);
      }
      historyPreview.classList.add("dragging");
      return;
    }
    draggedImageIndex = Number(item.dataset.imageIndex);
    item.classList.add("dragging");
    event.dataTransfer.effectAllowed = "move";
  });

  document.addEventListener("dragend", (event) => {
    const item = event.target.closest(".image-preview-item");
    if (item) {
      item.classList.remove("dragging");
    } else {
      const historyPreview = event.target.closest(".history-image-preview");
      if (historyPreview) {
        historyPreview.classList.remove("dragging");
      }
    }
    draggedImageIndex = -1;
    draggedHistoryImageURL = "";
    $("#imagePreviewList").classList.remove("drag-over");
  });

  document.addEventListener("dragover", (event) => {
    const previewList = event.target.closest("#imagePreviewList");
    if (previewList && draggedHistoryImageURL) {
      event.preventDefault();
      previewList.classList.add("drag-over");
      if (event.dataTransfer) {
        event.dataTransfer.dropEffect = "copy";
      }
      return;
    }
    if (event.target.closest(".image-preview-item")) {
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      $("#imagePreviewList").classList.remove("drag-over");
    }
  });

  document.addEventListener("drop", (event) => {
    const previewList = event.target.closest("#imagePreviewList");
    if (previewList && draggedHistoryImageURL) {
      event.preventDefault();
      previewList.classList.remove("drag-over");
      addImageURLAsReference(draggedHistoryImageURL).catch((error) => {
        $("#genStatus").textContent = error.message;
        $("#genStatus").className = "status gen-status error";
      });
      return;
    }
    const item = event.target.closest(".image-preview-item");
    if (!item || draggedImageIndex < 0) {
      $("#imagePreviewList").classList.remove("drag-over");
      return;
    }
    event.preventDefault();
    moveImage(draggedImageIndex, Number(item.dataset.imageIndex));
    $("#imagePreviewList").classList.remove("drag-over");
  });

  document.addEventListener("paste", async (event) => {
    const files = clipboardImageFiles(event);
    if (files.length === 0) {
      return;
    }
    event.preventDefault();
    const agentPane = $("#agentPane");
    if (agentPane && !agentPane.hidden && window.KuaimaAgent) {
      await window.KuaimaAgent.addAttachments(files);
      return;
    }
    await addReferenceFiles(files, "粘贴");
  });

  document.addEventListener("load", (event) => {
    if (event.target.matches(".gallery-preview img")) {
      updateLoadedImageSize(event.target);
    }
  }, true);

  document.addEventListener("kuaima-mask-change", () => {
    renderImagePreviews();
  });

  window.addEventListener("kuaima:account", () => {
    syncSingleGenerationControls();
  });

  document.addEventListener("click", async (event) => {
    const previewButton = event.target.closest("[data-preview-image]");
    if (previewButton) {
      openImagePreview(previewButton);
      return;
    }

    const imageButton = event.target.closest("[data-image-action]");
    if (imageButton) {
      const index = Number(imageButton.dataset.imageIndex);
      if (imageButton.dataset.imageAction === "remove") {
        selectedImages.splice(index, 1);
        syncImageInput();
        syncMaskAfterImageChange();
        renderImagePreviews();
      }
      return;
    }

    const usageLink = event.target.closest("#usageDetailLink");
    if (usageLink) {
      event.preventDefault();
      const originalText = usageLink.textContent;
      usageLink.textContent = "正在刷新链接...";
      usageLink.classList.remove("error");
      try {
        const url = await loadAccountLink();
        if (!url) {
          throw new Error("使用详情链接为空");
        }
        window.open(url, "_blank", "noopener");
        usageLink.textContent = originalText;
      } catch (error) {
        usageLink.textContent = "使用详情链接获取失败";
        usageLink.classList.add("error");
      }
      return;
    }

    const promoContact = event.target.closest("#promoContact");
    if (promoContact) {
      const modal = $("#workflowContactModal");
      const close = $("#closeWorkflowContactModal");
      if (modal) {
        modal.hidden = false;
        if (close) {
          close.focus();
        }
      }
      return;
    }

    const historyButton = event.target.closest("[data-history-action]");
    if (historyButton) {
      const action = historyButton.dataset.historyAction;
      try {
        if (action === "restore") {
          await restoreHistory(historyButton.dataset.historyId);
        } else if (action === "delete") {
          await deleteHistoryItem(historyButton.dataset.historyId);
          await renderHistory();
        } else if (action === "use-image") {
          await addImageURLAsReference(historyButton.dataset.imageUrl);
        }
      } catch (error) {
        $("#genStatus").textContent = error.message;
        $("#genStatus").className = "status gen-status error";
      }
      return;
    }

    const amountButton = event.target.closest("[data-amount]");
    const planButton = event.target.closest("[data-plan-id]");
    if (!amountButton && !planButton) {
      return;
    }
    if (amountButton) {
      selectAmount(amountButton.dataset.amount);
    } else {
      try {
        setRechargeTab("plan");
        await pay({ plan_id: Number(planButton.dataset.planId) });
      } catch (error) {
        openPaymentModal('<span class="status error">' + esc(error.message) + '</span>');
      }
    }
  });

  document.addEventListener("click", (event) => {
    if (!event.target.closest(".billing-menu")) {
      setBillingOpen(false);
    }
  });

  $("#amount").oninput = () => {
    const amount = Number($("#amount").value || 0);
    if (amount > 0) {
      selectedPayment = { amount };
      document.querySelectorAll("[data-amount],[data-plan-id]").forEach((item) => item.classList.remove("selected"));
    }
  };

  $("#toggleBillingTop").onclick = toggleBilling;
  $("#toggleHistory").onclick = () => {
    const sidebar = $("#historySidebar");
    const collapsed = !sidebar.classList.contains("collapsed");
    sidebar.classList.toggle("collapsed", collapsed);
    $("#toggleHistory").setAttribute("aria-expanded", String(!collapsed));
    $("#toggleHistory").setAttribute("aria-label", collapsed ? "展开历史记录" : "收起历史记录");
    $("#toggleHistory").innerHTML = iconHTML(collapsed ? "panel-left-open" : "panel-left-close");
  };
  $("#chooseOutputDir").onclick = chooseOutputDir;
  $("#openDefaultOutputDir").onclick = openDefaultOutputDir;
  $("#closePaymentModal").onclick = closePaymentModal;
  $("#closeImageModal").onclick = () => {
    $("#imageModal").hidden = true;
    $("#imageModalImg").removeAttribute("src");
    $("#imageModal").dataset.previewUrl = "";
  };
  $("#paymentModal").onclick = (event) => {
    if (event.target === $("#paymentModal")) {
      closePaymentModal();
    }
  };
  $("#imageModal").onclick = (event) => {
    if (event.target === $("#imageModal")) {
      $("#closeImageModal").click();
    }
  };

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    if (!$("#imageModal").hidden) {
      $("#closeImageModal").click();
    } else if (window.KuaimaFineEditor && !$("#fineEditor").hidden) {
      window.KuaimaFineEditor.close();
    } else if (window.KuaimaMaskEditor && !$("#maskEditor").hidden) {
      window.KuaimaMaskEditor.close();
    } else if (!$("#paymentModal").hidden) {
      closePaymentModal();
    }
  });

  setRechargeTab("amount");
  initConfig().catch((error) => {
    $("#genStatus").textContent = error.message;
    $("#genStatus").className = "status gen-status error";
  });
  requestPersistentHistoryStorage();
  cleanupHistoryImageData().then(renderHistory).catch((error) => {
    $("#historyPanel").innerHTML = '<div class="history-empty">历史读取失败：' + esc(error.message) + '</div>';
  });
  loadBalance();
  loadRechargeInfo();
})();
