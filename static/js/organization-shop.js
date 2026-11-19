(() => {
  "use strict";
  const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
  const cents = (raw) => {
    if (!/^-?\d+(?:\.\d{0,2})?$/.test(raw)) return null;
    const negative = raw.startsWith("-");
    const [whole, fraction = ""] = raw.replace(/^-/, "").split(".");
    const value = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
    return Number.isSafeInteger(value) ? (negative ? -value : value) : null;
  };
  for (const widget of document.querySelectorAll("[data-earnings]")) {
    const form = widget.closest("form");
    const price = form.querySelector('[name="base_price"]');
    const adjustment = form.querySelector('[name="price_adjustment"]');
    const update = () => {
      const base = price ? cents(price.value) : Number(widget.dataset.basePriceCents);
      const delta = adjustment ? cents(adjustment.value) : 0;
      const amount = base === null || delta === null ? null : base + delta;
      const valid = Number.isSafeInteger(amount) && amount > 0 && amount <= 100000000;
      const fee = valid ? Math.floor((amount * Number(widget.dataset.feeBps) + 5000) / 10000) : null;
      widget.querySelector("[data-earnings-price]").textContent = valid ? money.format(amount / 100) : "—";
      widget.querySelector("[data-earnings-fee]").textContent = valid ? money.format(fee / 100) : "—";
      widget.querySelector("[data-earnings-net]").textContent = valid ? money.format((amount - fee) / 100) : "—";
      widget.querySelector("[data-earnings-note]").textContent = valid
        ? "Before discounts. Tax and shipping are separate. Rounded per order line."
        : "Enter a valid item price to see your earnings.";
    };
    price?.addEventListener("input", update);
    adjustment?.addEventListener("input", update);
    update();
  }
  for (const form of document.querySelectorAll("[data-payout-form]")) {
    const method = form.querySelector("[data-payout-method]");
    const update = () => {
      for (const group of form.querySelectorAll("[data-payout-method-fields]")) {
        const active = group.dataset.payoutMethodFields === method.value;
        group.hidden = !active;
        for (const input of group.querySelectorAll("input")) {
          input.disabled = !active;
          input.required = active && input.hasAttribute("data-payout-required");
        }
      }
      const currency = form.querySelector('[name="payout_currency"]');
      if (method.value === "bank_transfer" && currency.value === "BTC") currency.value = "USD";
    };
    method.addEventListener("change", update);
    update();
  }
  for (const form of document.querySelectorAll("[data-shop-form]")) {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (form.dataset.busy === "1" || !form.reportValidity()) return;
      const message = form.querySelector("[data-form-message]");
      const submit = event.submitter;
      const body = new FormData(form);
      form.dataset.busy = "1";
      if (submit) submit.disabled = true;
      message.textContent = "Saving…";
      message.className = "";
      try {
        const response = await fetch(form.getAttribute("action"), {
          method: "POST",
          headers: { "X-CSRF-Token": body.get("csrf") },
          body,
        });
        if (!response.headers.get("content-type")?.includes("application/json"))
          throw Error("Please sign in again, then reload this page.");
        const result = await response.json();
        if (!response.ok)
          throw Error(result.error || "Could not save these changes.");
        location.assign(result.url);
      } catch (error) {
        message.textContent = error.message;
        message.className = "ew-error";
        message.scrollIntoView({ block: "nearest" });
      } finally {
        form.dataset.busy = "0";
        if (submit) submit.disabled = false;
      }
    });
  }
})();
