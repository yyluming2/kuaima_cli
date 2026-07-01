function setBillingOpen(open) {
  const dropdown = $("#billingDropdown");
  dropdown.hidden = !open;
  console.log('open bill ?', open)
  $("#toggleBillingTop").setAttribute("aria-expanded", String(open));
}

function toggleBilling() {
  setBillingOpen($("#billingDropdown").hidden);
}

function selectAmount(amount) {
  selectedPayment = { amount: Number(amount) };
  $("#amount").value = amount;
  setRechargeTab("amount");
  document.querySelectorAll("[data-amount],[data-plan-id]").forEach((item) => item.classList.remove("selected"));
  const chip = document.querySelector('[data-amount="' + CSS.escape(String(amount)) + '"]');
  if (chip) {
    chip.classList.add("selected");
  }
}

function activeSubscription(subscriptions) {
  return (Array.isArray(subscriptions) ? subscriptions : []).find((item) => {
    const status = String((item && item.status) || "").toLowerCase();
    return status === "active";
  }) || null;
}

function accountIdentity(usage) {
  if (!usage || typeof usage !== "object") {
    return "default";
  }
  return usage.group || 'default';
}

function accountIdentityLabel(group) {
  const value = String(group || "default").toLowerCase();
  return value === "svip" ? "SVIP" : value === "vip" ? "VIP" : "普通用户";
}

function renderSubscriptions(subscriptions) {
  const list = Array.isArray(subscriptions) ? subscriptions : [];
  $("#subscriptionCount").textContent = list.length ? String(list.length) : "";
  $("#subscriptionList").innerHTML = list.length ? list.map((item) => {
    const status = String((item && item.status) || "subscription").toLowerCase();
    const statusClass = status.replace(/[^a-z0-9_-]/g, "-");
    const available = Number(item.available) || 0;
    const used = Number(item.used) || 0;
    const total = Number(item.total) || (available + used);
    const usedPercent = total > 0 ? Math.max(0, Math.min(100, used / total * 100)) : 0;
    return (
      '<div class="subscription-item subscription-' + esc(statusClass) + '">' +
      '<div class="subscription-head">' +
      '<strong>' + esc(item.status || "subscription") + '</strong>' +
      '<span>' + esc(item.start || "-") + ' - ' + esc(item.end || "-") + '</span>' +
      '</div>' +
      '<div class="subscription-bar" aria-hidden="true">' +
      '<div class="subscription-bar-used" style="width:' + usedPercent.toFixed(2) + '%"></div>' +
      '</div>' +
      '<div class="subscription-row"><span>可用</span><b>' + esc(item.available_text || item.available || "-") + '</b></div>' +
      '<div class="subscription-row"><span>已用</span><b>' + esc(item.used_text || item.used || "-") + '</b></div>' +
      '</div>'
    );
  }).join("") : '<span class="status">暂无订阅套餐</span>';
}

function renderBalance(usage) {
  window.KuaimaAccount = Object.assign({}, window.KuaimaAccount || {}, {
    usage,
    group: String(usage && usage.group || "default").toLowerCase(),
  });
  window.dispatchEvent(new CustomEvent("kuaima:account", { detail: window.KuaimaAccount }));
  const subscriptions = usage.subscriptions || [];
  const active = activeSubscription(subscriptions);
  $("#accountIdentityTop").textContent = accountIdentityLabel(accountIdentity(usage));
  //$("#balanceTopLabel").textContent = active ? "当前套餐" : "可用token";
  $("#balanceTop").textContent = active
    ? ("有效至 " + (active.end || "-"))
    : (usage.total_available_text || "-");
  $("#balanceTop2").textContent = usage.total_available_text || "-";
  $("#usedTop").textContent = usage.total_used_text || "-";
  $("#accountName").textContent = usage.name || "-";
  $("#accountIdentity").textContent = accountIdentity(usage);
  $("#accountBalanceSummary").hidden = !!active;
  renderSubscriptions(subscriptions);

  const available = Number(usage.total_available) || 0;
  const used = Number(usage.total_used) || 0;
  const total = available + used;
  const usedPercent = total > 0 ? Math.max(0, Math.min(100, used / total * 100)) : 0;
  const availablePercent = total > 0 ? Math.max(0, Math.min(100, available / total * 100)) : 0;
  $("#usageProgressUsed").style.width = usedPercent.toFixed(2) + "%";
  $("#usageProgressAvailableLabel").textContent = "可用 " + availablePercent.toFixed(0) + "%";
  $("#usageProgressUsedLabel").textContent = "已用 " + usedPercent.toFixed(0) + "%";
}

if (!window.KuaimaBilling) window.KuaimaBilling = {};
window.KuaimaBilling.openPlans = function () {
  setBillingOpen(true);
  setRechargeTab("plan");
  window.requestAnimationFrame(() => {
    loadRechargeInfo();
  });
};

async function loadBalance() {
  try {
    const usage = await api("/api/balance");
    renderBalance(usage);
  } catch (error) {
    $("#balanceTop").textContent = "读取失败";
    $("#usedTop").textContent = error.message;
  }
}

async function loadAccountLink() {
  const result = await api("/api/account/link");
  return result.url || "";
}

function renderAmountOptions(info) {
  const discounts = info.discount || {};
  $("#amountOptions").innerHTML = (info.amount_options || []).map((amount) => {
    const key = formatAmount(amount);
    const label = discountText(discounts[key]);
    return '<button class="chip" type="button" data-amount="' + esc(amount) + '">' +
      '<strong>￥' + esc(key) + '</strong>' +
      (label ? '<span>' + esc(label) + '</span>' : '<span>原价</span>') +
      '</button>';
  }).join("") || '<span class="status">暂无充值档位</span>';
}

function renderPlans(info) {
  $("#plans").innerHTML = (info.plans || []).filter((plan) => plan.enabled).map((plan) => (
    '<div class="plan-card">' +
    '<div><b>' + esc(plan.title) + '</b><p>' + esc(plan.subtitle || "订阅套餐") + '</p></div>' +
    '<div class="plan-quota"><span>额度</span><strong>' + esc(formatQuota(plan.total_amount)) + '</strong></div>' +
    '<div class="plan-price"><strong>￥' + esc(formatAmount(plan.price_amount)) + '</strong><span>' +
    esc(plan.duration_value) + unit(plan.duration_unit) + '</span></div>' +
    '<button class="btn small primary" type="button" data-plan-id="' + Number(plan.id) +
    '" data-plan-title="' + esc(plan.title) + '">' + iconHTML("badge-plus") + '<span>订阅</span></button>' +
    '</div>'
  )).join("") || '<span class="status">暂无套餐</span>';
}

async function loadRechargeInfo() {
  const status = $("#rechargeStatus");
  status.textContent = "读取中...";
  status.className = "status";
  try {
    const info = await api("/api/recharge/info");
    renderAmountOptions(info);
    renderPlans(info);
    setRechargeTab(activeRechargeTab);
    status.textContent = "";
  } catch (error) {
    status.textContent = error.message;
    status.className = "status error";
  }
}

async function pay(body) {
  openPaymentModal('<span class="status">正在创建支付订单...</span>');
  const payment = await api("/api/recharge/pay", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  setBillingOpen(false);
  openPaymentModal('<a class="paybox modal-paybox" href="' + esc(payment.url) +
    '" target="_blank"><img src="' + esc(payment.qr) +
    '" alt="支付二维码"><div><span>微信扫一扫支付</span><strong>订单号 ' +
    esc(payment.trade_no || "-") + '</strong></div></a>');
}

async function exchangeCode() {
  const input = $("#exchangeCode");
  const button = $("#exchangeCodeSubmit");
  const status = $("#exchangeCodeStatus");
  const key = String(input.value || "").trim();
  if (!key) {
    status.textContent = "请输入兑换码";
    status.className = "status error";
    input.focus();
    return;
  }
  const originalHTML = button.innerHTML;
  button.disabled = true;
  button.innerHTML = iconHTML("loader-circle") + "<span>兑换中...</span>";
  status.textContent = "";
  status.className = "status";
  try {
    await api("/api/recharge/exchange", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key }),
    });
    input.value = "";
    status.textContent = "兑换成功，余额已刷新";
    status.className = "status success";
    await loadBalance();
  } catch (error) {
    status.textContent = error.message;
    status.className = "status error";
  } finally {
    button.disabled = false;
    button.innerHTML = originalHTML;
    if (window.KuaimaIcons) {
      window.KuaimaIcons.render();
    }
  }
}

function openPaymentModal(content) {
  $("#paymentModalBody").innerHTML = content;
  $("#paymentModal").hidden = false;
}

function closePaymentModal() {
  $("#paymentModal").hidden = true;
}
