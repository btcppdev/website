(() => {
  "use strict";
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
