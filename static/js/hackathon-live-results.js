(function () {
  "use strict";

  const root = document.querySelector("[data-live-judging-results]");
  if (!root || !root.dataset.liveResultsUrl || !window.fetch) return;

  let lastResponse = "";
  let refreshing = false;

  function setStatus(message, paused) {
    const status = root.querySelector("[data-live-results-status]");
    const label = root.querySelector("[data-live-results-label]");
    if (!status || !label) return;
    label.textContent = message;
    status.classList.toggle("hack-live-results-status--paused", Boolean(paused));
  }

  async function refresh() {
    if (document.hidden || refreshing) return;
    refreshing = true;
    try {
      const response = await fetch(root.dataset.liveResultsUrl, {
        credentials: "same-origin",
        cache: "no-store",
        headers: { "X-Requested-With": "fetch" }
      });
      if (!response.ok) throw new Error("refresh failed");
      const html = await response.text();
      if (html !== lastResponse) {
        root.innerHTML = html;
        lastResponse = html;
      }
      setStatus("Live results", false);
    } catch (_) {
      setStatus("Live updates paused", true);
    } finally {
      refreshing = false;
    }
  }

  window.setInterval(refresh, 2000);
  document.addEventListener("visibilitychange", function () {
    if (!document.hidden) refresh();
  });
})();
