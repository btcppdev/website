(function () {
  "use strict";

  var form = document.querySelector("[data-checkout-flow]");
  if (!form) return;

  var discountInput = form.querySelector('[name="Discount"]');

  function syncSpecialOffers() {
    var hasCode = Boolean(discountInput && discountInput.value.trim());
    form.querySelectorAll('[name="OfferCode"]').forEach(function (input) {
      if (hasCode) input.checked = false;
      input.disabled = hasCode;
    });
  }

  function syncDiscountInput(event) {
    if (event.target === discountInput) syncSpecialOffers();
  }

  // Disable eligibility offers before HTMX reads the form so a typed code
  // cannot be submitted alongside a previously selected offer.
  form.addEventListener(
    "change",
    function (event) {
      if (event.target.name !== "OfferCode" || !event.target.checked) return;
      form.querySelectorAll('[name="OfferCode"]').forEach(function (input) {
        if (input !== event.target) input.checked = false;
      });
      if (discountInput) discountInput.value = "";
      syncSpecialOffers();
    },
    true,
  );
  form.addEventListener("input", syncDiscountInput, true);
  form.addEventListener("change", syncDiscountInput, true);
  form.addEventListener("search", syncDiscountInput, true);
  window.addEventListener("pageshow", syncSpecialOffers);
  syncSpecialOffers();

  var messages = JSON.parse(document.getElementById("checkout-messages").textContent);
  function message(key, values) {
    return messages[key].replace(/\{(\w+)\}/g, function (_, name) { return String(values[name]); });
  }
  function ticketLabel(count) {
    var noun = form.getAttribute("data-sponsored") === "true" ? "pass" : "ticket";
    return message(noun + (count === 1 ? "_one" : "_many"), { Count: count });
  }

  var panels = form.querySelectorAll("[data-checkout-step]");
  var hasAddOns = Boolean(form.querySelector('[data-checkout-step="addons"]'));
  var nav = document.querySelector(".checkout-steps");
  var navItems = nav ? nav.querySelectorAll("[data-checkout-nav]") : [];
  var ticketRows = Array.from(form.querySelectorAll("[data-ticket-option]"));
  var isCart = ticketRows.length > 0;
  var quotePending = false;
  var quoteFailed = false;
  var taxReady = true;
  var countInput = form.querySelector('[name="Count"]');
  var methodInputs = form.querySelectorAll('[name="PaymentMethod"]');
  var submitButton = form.querySelector("[data-checkout-submit]");
  var taxElement = form.querySelector("[data-addon-tax]");
  var taxStatus = form.querySelector("[data-addon-tax-status]");
  var taxCents = 0;
  var taxRequestSequence = 0;
  var checkoutCurrency = (form.getAttribute("data-currency") || "USD").trim().toUpperCase();

  form.classList.add("is-enhanced");

  function money(cents) {
    return new Intl.NumberFormat(document.documentElement.lang || "en", {
      style: "currency",
      currency: checkoutCurrency
    }).format(cents / 100);
  }

  function showStep(step) {
    if (step === "addons" && !hasAddOns) step = "register";
    panels.forEach(function (panel) {
      panel.hidden = panel.getAttribute("data-checkout-step") !== step;
    });
    Array.prototype.forEach.call(navItems, function (item) {
      var itemStep = item.getAttribute("data-checkout-nav");
      item.classList.toggle("is-active", itemStep === step);
      item.classList.toggle("is-complete", step === "addons" && itemStep === "register");
    });
    if (nav) {
      nav.setAttribute("data-step", (step === "addons" ? "02" : "01") + "/0" + navItems.length);
      nav.setAttribute("data-current", step);
      nav.setAttribute("data-current-label", messages[step]);
    }
    var heading = form.querySelector('[data-checkout-step="' + step + '"] h2');
    if (heading) {
      heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
    }
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function registrationIsValid() {
    var required = form.querySelectorAll('[data-checkout-step="register"] input[required]');
    for (var i = 0; i < required.length; i++) {
      if (!required[i].checkValidity()) {
        required[i].reportValidity();
        return false;
      }
    }
    return true;
  }

  function updateAddon(addon, delta, absolute) {
    var input = addon.querySelector('input[type="number"]');
    var next = absolute == null ? Number(input.value || 0) + delta : absolute;
    next = Math.max(0, Math.min(4, next));
    input.value = String(next);
    addon.classList.toggle("is-selected", next > 0);
    addon.querySelector("[data-addon-add]").hidden = next > 0;
    addon.querySelector(".checkout-addon__stepper").hidden = next === 0;
    updateSummary();
  }

  function selectedTicketCount() {
    return isCart ? ticketRows.reduce(function (sum, row) {
      return sum + Number(row.querySelector('input[type="number"]').value || 0);
    }, 0) : Math.max(1, Number(countInput ? countInput.value || 1 : 1));
  }

  function updateTicketCart() {
    if (!isCart) return 0;
    var card = form.querySelector('[name="PaymentMethod"]:checked');
    var attribute = card && card.value === "card" ? "data-card" : "data-btc";
    var details = form.querySelector("[data-ticket-cart-summary]");
    var lines = form.querySelector("[data-ticket-cart-lines]");
    var count = selectedTicketCount();
    var validCount = Number.isInteger(count) && count >= 1 && count <= 8;
    var total = 0;
    if (lines) lines.textContent = "";
    var prices = new Map();
    if (details) details.querySelectorAll("[data-ticket-quote]").forEach(function (item) {
      prices.set(item.getAttribute("data-ticket-quote"), item);
    });
    ticketRows.forEach(function (row, index) {
      var input = row.querySelector('input[type="number"]');
      var qty = Number(input.value || 0);
      var quote = prices.get(row.getAttribute("data-ticket-option"));
      var price = quote ? Number(quote.getAttribute(attribute)) : 0;
      total += price * qty;
      row.querySelector("[data-ticket-unit-price]").textContent = money(price);
      row.classList.toggle("is-selected", qty > 0);
      row.querySelector("[data-ticket-minus]").disabled = qty <= 0;
      row.querySelector("[data-ticket-plus]").disabled = qty >= Number(input.max) || count >= 8;
      input.setCustomValidity(index === 0 && !validCount ? messages.max_tickets : "");
      if (qty && lines) {
        var line = document.createElement("div");
        var label = document.createElement("span");
        var amount = document.createElement("strong");
        label.textContent = row.getAttribute("data-ticket-name") + " × " + qty;
        amount.textContent = money(price * qty);
        line.append(label, amount);
        lines.appendChild(line);
      }
    });
    var error = form.querySelector("[data-ticket-selection-error]");
    if (error) {
      error.hidden = validCount && !quoteFailed;
      error.textContent = quoteFailed ? messages.quote_error : messages.max_tickets;
    }
    var subtotal = form.querySelector("[data-ticket-cart-total]");
    if (subtotal) subtotal.textContent = money(total);
    submitButton.disabled = !taxReady || quotePending || quoteFailed || !validCount || Boolean(details && details.getAttribute("data-cart-error"));
    return total;
  }

  function ticketTotalCents() {
    if (isCart) return updateTicketCart();
    var details = form.querySelector("#discount_result");
    if (!details) return 0;
    var card = form.querySelector('[name="PaymentMethod"]:checked');
    var priceAttribute = card && card.value === "card"
      ? "data-ticket-card-price-cents"
      : "data-ticket-btc-price-cents";
    return Number(details.getAttribute(priceAttribute) || 0) * Number(countInput ? countInput.value || 1 : 1);
  }

  function updatePaymentPrice() {
    var price = form.querySelector("[data-selected-payment-price]");
    var selected = form.querySelector('[name="PaymentMethod"]:checked');
    var cardSelected = selected && selected.value === "card";
    form.querySelectorAll("[data-payment-instructions]").forEach(function (note) {
      note.hidden = note.getAttribute("data-payment-instructions") !== (cardSelected ? "card" : "btc");
    });
    if (!price) return;
    price.querySelector("[data-payment-price-label]").textContent = cardSelected ? messages.card_price : messages.bitcoin_price;
    price.querySelector("[data-payment-price-amount]").textContent = price.getAttribute(
      cardSelected ? "data-card-price" : "data-bitcoin-price"
    );
  }

  function updateSummary() {
    updatePaymentPrice();
    var lines = form.querySelector("[data-addon-summary-lines]");
    var totalElement = form.querySelector("[data-addon-total]");
    var countLabel = form.querySelector("[data-ticket-count-label]");
    var ticketCount = selectedTicketCount();
    var merchTotal = 0;
    var selected = [];

    if (countLabel) {
      countLabel.textContent = ticketLabel(ticketCount);
    }

    form.querySelectorAll("[data-checkout-addon]").forEach(function (addon) {
      var quantity = Number(addon.querySelector('input[type="number"]').value || 0);
      if (!quantity) return;
      var price = Number(addon.getAttribute("data-price-cents") || 0);
      merchTotal += price * quantity;
      selected.push({
        name: addon.getAttribute("data-name"),
        quantity: quantity,
        total: price * quantity
      });
    });

    if (lines) {
      lines.textContent = "";
      if (!selected.length) {
        var empty = document.createElement("div");
        empty.className = "is-empty";
        empty.innerHTML = "<span></span><strong></strong>";
        empty.querySelector("span").textContent = messages.no_addons;
        empty.querySelector("strong").textContent = money(0);
        lines.appendChild(empty);
      } else {
        selected.forEach(function (item) {
          var line = document.createElement("div");
          var label = document.createElement("span");
          var amount = document.createElement("strong");
          label.textContent = item.name + " × " + item.quantity;
          amount.textContent = money(item.total);
          line.appendChild(label);
          line.appendChild(amount);
          lines.appendChild(line);
        });
      }
    }

    if (totalElement) totalElement.textContent = money(ticketTotalCents() + merchTotal + taxCents);
    submitButton.textContent = message("continue", { Tickets: ticketLabel(ticketCount), Total: money(ticketTotalCents() + merchTotal + taxCents) });
  }

  function selectedAddOnQuantity() {
    var quantity = 0;
    form.querySelectorAll("[data-checkout-addon]").forEach(function (addon) {
      quantity += Number(addon.querySelector('input[type="number"]').value || 0);
    });
    return quantity;
  }

  function loadTaxQuote() {
    var sequence = ++taxRequestSequence;
    if (!selectedAddOnQuantity()) {
      taxCents = 0;
      if (taxElement) taxElement.textContent = money(0);
      if (taxStatus) taxStatus.textContent = messages.tax_none;
      taxReady = true;
      submitButton.disabled = false;
      updateSummary();
      return;
    }

    taxReady = false;
    submitButton.disabled = true;
    if (taxStatus) taxStatus.textContent = messages.tax_loading;
    fetch(form.getAttribute("data-tax-url"), {
      method: "POST",
      body: new FormData(form),
      credentials: "same-origin"
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) throw new Error(messages.tax_error);
        return body;
      });
    }).then(function (body) {
      if (sequence !== taxRequestSequence) return;
      taxCents = Number(body.tax_cents || 0);
      if (taxElement) taxElement.textContent = money(taxCents);
      if (taxStatus) taxStatus.textContent = messages.tax_done;
      taxReady = true;
      submitButton.disabled = false;
      updateSummary();
    }).catch(function (error) {
      if (sequence !== taxRequestSequence) return;
      taxCents = 0;
      if (taxElement) taxElement.textContent = "—";
      if (taxStatus) taxStatus.textContent = messages.tax_error;
      taxReady = false;
    submitButton.disabled = true;
      updateSummary();
    });
  }

  var nextButton = form.querySelector("[data-checkout-next]");
  if (nextButton) nextButton.addEventListener("click", function (event) {
    event.preventDefault();
    if (registrationIsValid()) showStep("addons");
  });
  var backButton = form.querySelector("[data-checkout-back]");
  if (backButton) backButton.addEventListener("click", function () {
    showStep("register");
  });

  form.querySelectorAll("[data-checkout-addon]").forEach(function (addon) {
    addon.querySelector("[data-addon-add]").hidden = false;
    addon.querySelector(".checkout-addon__stepper").hidden = true;
    addon.querySelector("[data-addon-add]").addEventListener("click", function () {
      updateAddon(addon, 0, 1);
      loadTaxQuote();
    });
    addon.querySelector("[data-addon-minus]").addEventListener("click", function () {
      updateAddon(addon, -1);
      loadTaxQuote();
    });
    addon.querySelector("[data-addon-plus]").addEventListener("click", function () {
      updateAddon(addon, 1);
      loadTaxQuote();
    });
    addon.querySelector('input[type="number"]').addEventListener("change", function (event) {
      updateAddon(addon, 0, Number(event.target.value || 0));
      loadTaxQuote();
    });
  });

  ticketRows.forEach(function (row) {
    var input = row.querySelector('input[type="number"]');
    function adjust(delta) {
      input.value = String(Math.max(0, Math.min(Number(input.max), Number(input.value || 0) + delta)));
      updateSummary();
    }
    row.querySelector("[data-ticket-minus]").addEventListener("click", function () { adjust(-1); });
    row.querySelector("[data-ticket-plus]").addEventListener("click", function () { adjust(1); });
    input.addEventListener("input", updateSummary);
  });
  function waitForQuote(event) {
    if (!isCart || (event.target.name !== "Discount" && event.target.name !== "OfferCode")) return;
    quotePending = true;
    quoteFailed = false;
    updateSummary();
  }
  form.addEventListener("input", function (event) { if (event.target.name === "Discount") waitForQuote(event); });
  form.addEventListener("change", function (event) { if (event.target.name === "OfferCode") waitForQuote(event); });
  form.addEventListener("htmx:afterRequest", function (event) {
    if (!isCart || !event.detail.elt || !["Discount", "OfferCode"].includes(event.detail.elt.name)) return;
    quotePending = false;
    quoteFailed = !event.detail.successful;
    updateSummary();
  });
  form.addEventListener("submit", function (event) {
    if (!registrationIsValid() || (isCart && (submitButton.disabled || quotePending))) event.preventDefault();
  });

  if (countInput) countInput.addEventListener("input", updateSummary);
  methodInputs.forEach(function (input) { input.addEventListener("change", updateSummary); });
  document.body.addEventListener("htmx:afterSwap", updateSummary);

  showStep(hasAddOns && window.location.hash === "#checkout-addons" && registrationIsValid() ? "addons" : "register");
  updateSummary();
  loadTaxQuote();
})();
