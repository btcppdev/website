(function () {
  "use strict";

  var checkout = document.querySelector("[data-shop-checkout]");
  var form = document.querySelector("[data-shop-checkout-form]");
  if (!checkout || !form) return;

  var detailsPanel = form.querySelector('[data-shop-checkout-step="details"]');
  var shippingPanel = form.querySelector('[data-shop-checkout-step="shipping"]');
  var confirmPanel = form.querySelector('[data-shop-checkout-step="confirm"]');
  var detailsButton = form.querySelector("[data-shop-continue-shipping]");
  var shippingReviewButton = form.querySelector("[data-shop-review-order]");
  var createButton = form.querySelector("[data-shop-create-order]");
  var editDetailsButton = form.querySelector("[data-shop-edit-details]");
  var reviewBackButton = form.querySelector("[data-shop-review-back]");
  var reviewKicker = form.querySelector("[data-shop-review-kicker]");
  var summaryButton = checkout.querySelector("[data-shop-summary-action]");
  var shippingFields = form.querySelector("[data-shipping-address-fields]");
  var shippingRequired = form.querySelectorAll("[data-shipping-required]");
  var shippingRateStatus = form.querySelector("[data-shipping-rates-status]");
  var shippingRateOptions = form.querySelector("[data-shipping-rate-options]");
  var taxStatus = form.querySelector("[data-tax-status]");
  var summaryShipping = checkout.querySelector("[data-summary-shipping]");
  var summaryTax = checkout.querySelector("[data-summary-tax]");
  var summaryTotal = checkout.querySelector("[data-summary-total]");
  var currentStep = "details";
  var quotedAddressKey = "";
  var ratesLoading = false;
  var quotedTaxKey = "";
  var taxLoading = false;

  function selected(name) {
    return form.querySelector('[name="' + name + '"]:checked');
  }

  function field(name) {
    return form.elements[name] ? String(form.elements[name].value || "").trim() : "";
  }

  function setText(selector, value) {
    var target = form.querySelector(selector);
    if (target) target.textContent = value;
  }

  function reportStepValidity(panel) {
    var controls = panel.querySelectorAll("input, select, textarea");
    for (var i = 0; i < controls.length; i += 1) {
      if (!controls[i].checkValidity()) {
        controls[i].reportValidity();
        return false;
      }
    }
    return true;
  }

  function fulfillmentIsShipping() {
    var fulfillment = selected("fulfillment");
    return !fulfillment || fulfillment.value === "ship";
  }

  function detailsActionLabel() {
    return fulfillmentIsShipping() ? "Shipping →" : "Review order →";
  }

  function updateDetailsActions() {
    detailsButton.textContent = detailsActionLabel();
    if (currentStep === "details") summaryButton.textContent = detailsActionLabel();
  }

  function updateShippingFields() {
    var shipping = fulfillmentIsShipping();
    if (shippingFields) shippingFields.hidden = !shipping;
    Array.prototype.forEach.call(shippingRequired, function (input) {
      input.required = shipping;
    });
    if (!shipping) {
      updateSummary(0);
    }
  }

  function money(cents) {
    return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }).format(cents / 100);
  }

  function updateSummary(shippingCents, taxCents) {
    var subtotal = summaryTotal ? Number(summaryTotal.getAttribute("data-subtotal-cents") || 0) : 0;
    var tax = taxCents === undefined ? Number(summaryTotal.getAttribute("data-tax-cents") || 0) : Number(taxCents || 0);
    if (summaryTotal) summaryTotal.setAttribute("data-tax-cents", String(tax));
    if (summaryShipping) summaryShipping.textContent = shippingCents === 0 ? "free" : money(shippingCents);
    if (summaryTax) summaryTax.textContent = money(tax);
    if (summaryTotal) summaryTotal.textContent = money(subtotal + tax + shippingCents);
  }

  function shippingAddressKey() {
    return ["address1", "address2", "city", "region", "postal_code", "country"].map(field).join("|");
  }

  function taxQuoteKey() {
    var rate = selectedShippingRate();
    return [
      fulfillmentIsShipping() ? shippingAddressKey() : "pickup",
      rate ? rate.value : "",
      field("shipping_rate_amount_cents")
    ].join("|");
  }

  function selectedShippingRate() {
    return selected("shipping_rate_id");
  }

  function shippingRateLabel(input) {
    if (!input) return "";
    return input.getAttribute("data-rate-label") || "";
  }

  function setShippingStatus(message, loading) {
    if (!shippingRateStatus) return;
    shippingRateStatus.hidden = false;
    shippingRateStatus.textContent = message;
    shippingRateStatus.classList.toggle("is-loading", Boolean(loading));
  }

  function setTaxStatus(message, loading) {
    if (!taxStatus) return;
    taxStatus.hidden = !message;
    taxStatus.textContent = message || "";
    taxStatus.classList.toggle("is-loading", Boolean(loading));
  }

  function invalidateTaxQuote() {
    quotedTaxKey = "";
    setTaxStatus("", false);
    updateSummary(
      fulfillmentIsShipping() ? Number(field("shipping_rate_amount_cents") || 0) : 0,
      0
    );
  }

  function invalidateShippingRates() {
    quotedAddressKey = "";
    if (shippingRateOptions) shippingRateOptions.replaceChildren();
    if (form.elements.shipping_rate_amount_cents) form.elements.shipping_rate_amount_cents.value = "";
    if (summaryShipping) summaryShipping.textContent = "select service";
    if (summaryTotal) {
      var subtotal = Number(summaryTotal.getAttribute("data-subtotal-cents") || 0);
      var tax = Number(summaryTotal.getAttribute("data-tax-cents") || 0);
      summaryTotal.textContent = money(subtotal + tax);
    }
    invalidateTaxQuote();
    setShippingStatus(
      fulfillmentIsShipping()
        ? "Continue to load available shipping services."
        : "Event pickup selected. No shipping service is needed.",
      false
    );
  }

  function renderShippingRates(rates) {
    shippingRateOptions.replaceChildren();
    rates.forEach(function (rate) {
      var label = document.createElement("label");
      var input = document.createElement("input");
      var title = document.createTextNode(" " + (rate.courier || rate.service || "Shipping service") + " ");
      var detail = document.createElement("span");
      var service = rate.service && rate.service !== rate.courier ? rate.service + " · " : "";
      var delivery = "";
      if (rate.min_days && rate.max_days) delivery = rate.min_days + "–" + rate.max_days + " business days · ";
      else if (rate.max_days) delivery = "up to " + rate.max_days + " business days · ";

      input.type = "radio";
      input.name = "shipping_rate_id";
      input.value = rate.id;
      input.required = true;
      input.setAttribute("data-rate-label", (rate.courier || rate.service || "Shipping service") + " · " + delivery + money(rate.amount_cents));
      detail.textContent = service + delivery + money(rate.amount_cents);
      input.addEventListener("change", function () {
        if (form.elements.shipping_rate_amount_cents) form.elements.shipping_rate_amount_cents.value = String(rate.amount_cents || 0);
        invalidateTaxQuote();
        updateSummary(Number(rate.amount_cents || 0), 0);
        loadTaxQuote();
      });
      label.appendChild(input);
      label.appendChild(title);
      label.appendChild(detail);
      shippingRateOptions.appendChild(label);
    });
  }

  function loadShippingRates() {
    if (ratesLoading) return Promise.resolve(false);
    ratesLoading = true;
    setShippingStatus("Loading shipping quotes…", true);
    shippingRateOptions.replaceChildren();
    detailsButton.disabled = true;
    shippingReviewButton.disabled = true;
    summaryButton.disabled = true;

    return fetch("/shop/shipping-rates", {
      method: "POST",
      body: new FormData(form),
      credentials: "same-origin"
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) throw new Error(body.error || "Shipping services could not be loaded.");
        return body;
      });
    }).then(function (body) {
      if (!body.rates || body.rates.length === 0) throw new Error("No shipping services are available for that address.");
      quotedAddressKey = shippingAddressKey();
      renderShippingRates(body.rates);
      setShippingStatus("Select a shipping service, then review your order.", false);
      shippingRateStatus.scrollIntoView({ behavior: "smooth", block: "center" });
      return false;
    }).catch(function (error) {
      quotedAddressKey = "";
      setShippingStatus(error.message, false);
      shippingRateStatus.scrollIntoView({ behavior: "smooth", block: "center" });
      return false;
    }).finally(function () {
      ratesLoading = false;
      detailsButton.disabled = false;
      shippingReviewButton.disabled = false;
      summaryButton.disabled = false;
    });
  }

  function loadTaxQuote() {
    if (taxLoading) return Promise.resolve(false);
    if (quotedTaxKey === taxQuoteKey()) return Promise.resolve(true);
    taxLoading = true;
    if (fulfillmentIsShipping()) setShippingStatus("Calculating tax…", true);
    else setTaxStatus("Calculating tax…", true);
    detailsButton.disabled = true;
    shippingReviewButton.disabled = true;
    summaryButton.disabled = true;

    return fetch("/shop/tax-quote", {
      method: "POST",
      body: new FormData(form),
      credentials: "same-origin"
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) {
          var error = new Error(body.error || "Tax could not be calculated.");
          error.code = body.code || "";
          throw error;
        }
        return body;
      });
    }).then(function (body) {
      quotedTaxKey = taxQuoteKey();
      updateSummary(
        fulfillmentIsShipping() ? Number(field("shipping_rate_amount_cents") || 0) : 0,
        Number(body.tax_cents || 0)
      );
      if (fulfillmentIsShipping()) setShippingStatus("Tax calculated. Review your order when ready.", false);
      else setTaxStatus("Tax calculated.", false);
      return true;
    }).catch(function (error) {
      quotedTaxKey = "";
      if (error.code === "shipping_rates_expired") {
        quotedAddressKey = "";
        setShippingStatus("Shipping services changed. Loading current options…", true);
        window.setTimeout(loadShippingRates, 0);
        return false;
      }
      if (fulfillmentIsShipping()) setShippingStatus(error.message, false);
      else setTaxStatus(error.message, false);
      return false;
    }).finally(function () {
      taxLoading = false;
      detailsButton.disabled = false;
      shippingReviewButton.disabled = false;
      summaryButton.disabled = false;
    });
  }

  function confirmationAddress() {
    return [
      field("address1"),
      field("address2"),
      [field("city"), field("region"), field("postal_code")].filter(Boolean).join(", "),
      field("country"),
      field("phone")
    ].filter(Boolean).join("\n");
  }

  function populateConfirmation() {
    var fulfillment = selected("fulfillment");
    var fulfillmentLabel = fulfillment && fulfillment.closest("label");
    var addressRow = form.querySelector("[data-confirm-address-row]");
    var rateRow = form.querySelector("[data-confirm-shipping-rate-row]");
    var rate = selectedShippingRate();

    setText("[data-confirm-contact]", field("name") + "\n" + field("email"));
    setText("[data-confirm-fulfillment]", fulfillmentLabel ? fulfillmentLabel.getAttribute("data-fulfillment-label") : "Ship it to me");
    if (addressRow) addressRow.hidden = !fulfillmentIsShipping();
    if (rateRow) rateRow.hidden = !fulfillmentIsShipping();
    if (fulfillmentIsShipping()) setText("[data-confirm-address]", confirmationAddress());
    if (fulfillmentIsShipping()) setText("[data-confirm-shipping-rate]", shippingRateLabel(rate));
    if (reviewKicker) reviewKicker.textContent = fulfillmentIsShipping() ? "04 / review" : "03 / review";
    if (reviewBackButton) reviewBackButton.textContent = fulfillmentIsShipping() ? "← Shipping" : "← Edit details";
  }

  function populateShippingStep() {
    var fulfillment = selected("fulfillment");
    var fulfillmentLabel = fulfillment && fulfillment.closest("label");
    var addressRow = form.querySelector("[data-shipping-address-row]");
    setText("[data-shipping-fulfillment]", fulfillmentLabel ? fulfillmentLabel.getAttribute("data-fulfillment-label") : "Ship it to me");
    if (addressRow) addressRow.hidden = !fulfillmentIsShipping();
    if (fulfillmentIsShipping()) {
      setText("[data-shipping-address]", confirmationAddress());
    } else {
      updateSummary(0);
      setShippingStatus("Event pickup selected. No shipping service is needed.", false);
    }
  }

  function showStep(step) {
    currentStep = step;
    detailsPanel.hidden = step !== "details";
    shippingPanel.hidden = step !== "shipping";
    confirmPanel.hidden = step !== "confirm";
    if (step === "details") summaryButton.textContent = detailsActionLabel();
    else if (step === "shipping") summaryButton.textContent = "Review order →";
    else summaryButton.textContent = "Make payment →";
    checkout.classList.toggle("is-confirming", step === "confirm");

    var panel = step === "confirm" ? confirmPanel : (step === "shipping" ? shippingPanel : detailsPanel);
    var heading = panel.querySelector("h1");
    if (heading) {
      heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
    }
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function continueToShipping() {
    updateShippingFields();
    if (!reportStepValidity(detailsPanel)) return;
    if (!fulfillmentIsShipping()) {
      loadTaxQuote().then(function (ready) {
        if (!ready) return;
        populateConfirmation();
        showStep("confirm");
      });
      return;
    }
    populateShippingStep();
    showStep("shipping");
    if (fulfillmentIsShipping()) {
      if (quotedAddressKey !== shippingAddressKey() || !shippingRateOptions.children.length) {
        loadShippingRates();
      }
    }
  }

  function reviewOrder() {
    if (ratesLoading || taxLoading) return;
    if (fulfillmentIsShipping()) {
      if (quotedAddressKey !== shippingAddressKey() || !shippingRateOptions.children.length) {
        loadShippingRates();
        return;
      }
      if (!selectedShippingRate()) {
        setShippingStatus("Select a shipping service before reviewing the order.", false);
        shippingRateStatus.scrollIntoView({ behavior: "smooth", block: "center" });
        return;
      }
    }
    if (quotedTaxKey !== taxQuoteKey()) {
      loadTaxQuote().then(function (ready) {
        if (ready) reviewOrder();
      });
      return;
    }
    populateConfirmation();
    showStep("confirm");
  }

  form.querySelectorAll('[name="fulfillment"]').forEach(function (input) {
    input.addEventListener("change", function () {
      updateShippingFields();
      invalidateShippingRates();
      updateDetailsActions();
    });
  });

  ["address1", "address2", "city", "region", "postal_code", "country"].forEach(function (name) {
    if (form.elements[name]) form.elements[name].addEventListener("change", invalidateShippingRates);
  });

  form.addEventListener("submit", function (event) {
    if (currentStep === "details") {
      event.preventDefault();
      continueToShipping();
      return;
    }
    if (currentStep === "shipping") {
      event.preventDefault();
      reviewOrder();
      return;
    }
    createButton.disabled = true;
    summaryButton.disabled = true;
    createButton.textContent = "Starting payment…";
    summaryButton.textContent = "Starting payment…";
  });

  function resetPaymentButtons() {
    createButton.disabled = false;
    summaryButton.disabled = false;
    createButton.textContent = "Make payment →";
    if (currentStep === "details") summaryButton.textContent = detailsActionLabel();
    else if (currentStep === "shipping") summaryButton.textContent = "Review order →";
    else summaryButton.textContent = "Make payment →";
  }

  window.addEventListener("pagehide", resetPaymentButtons);
  window.addEventListener("pageshow", function (event) {
    resetPaymentButtons();
    if (event.persisted) window.location.replace("/shop/checkout");
  });

  detailsButton.addEventListener("click", function (event) {
    event.preventDefault();
    continueToShipping();
  });

  shippingReviewButton.addEventListener("click", function (event) {
    event.preventDefault();
    reviewOrder();
  });

  editDetailsButton.addEventListener("click", function () {
    showStep("details");
  });

  reviewBackButton.addEventListener("click", function () {
    showStep(fulfillmentIsShipping() ? "shipping" : "details");
  });

  updateShippingFields();
  invalidateShippingRates();
  updateDetailsActions();
  checkout.classList.add("is-enhanced");
}());
