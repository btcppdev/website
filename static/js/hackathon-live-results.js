(function () {
  "use strict";
  const root = document.querySelector("[data-live-judging-results]");
  if (!root || !root.dataset.liveResultsUrl) return;
  let source;
  let reconnectTimer;
  let revoked = false;
  function status(message) {
    const label = root.querySelector("[data-live-results-label]");
    if (label) label.textContent = message;
  }
  function stop() {
    clearTimeout(reconnectTimer);
    if (source) source.close();
    source = null;
  }
  function connect() {
    stop();
    if (document.hidden || revoked) return;
    if (!window.EventSource) {
      status("Refresh this page for updated results");
      return;
    }
    source = new EventSource(root.dataset.liveResultsUrl);
    source.addEventListener("results", function (event) {
      try {
        const payload = JSON.parse(event.data);
        if (typeof payload.html === "string") root.innerHTML = payload.html;
      } catch (_) { status("Unable to display updates"); }
    });
    source.addEventListener("reconnect", function () {
      stop();
      reconnectTimer = setTimeout(connect, 500 + Math.random() * 1000);
    });
    source.addEventListener("revoked", function () {
      revoked = true;
      stop();
      // Remove previously visible results if access or round state changes.
      root.textContent = "Results are no longer available. Reload the judging page.";
    });
    source.onerror = function () { status("Live updates paused — reconnecting"); };
  }
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) stop(); else connect();
  });
  window.addEventListener("pagehide", stop);
  window.addEventListener("pageshow", connect);
  connect();
})();
