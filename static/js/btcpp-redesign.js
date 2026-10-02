(function () {
  const rebrandNav = document.querySelector(".rebrand-nav");
  const rebrandToggle = document.querySelector("[data-rebrand-menu-toggle]");

  if (rebrandToggle && rebrandNav) {
    rebrandToggle.addEventListener("click", function () {
      const isOpen = rebrandNav.classList.toggle("is-open");
      rebrandToggle.setAttribute("aria-expanded", isOpen ? "true" : "false");
    });

    rebrandNav.querySelectorAll(".rebrand-nav__links a").forEach(function (link) {
      link.addEventListener("click", function () {
        rebrandNav.classList.remove("is-open");
        rebrandToggle.setAttribute("aria-expanded", "false");
      });
    });
  }

  document.querySelectorAll(".btcpp-event-page .tabs, .btcpp-rebrand-page .tabs").forEach(function (tabs) {
    const tabLinks = Array.from(tabs.querySelectorAll("[data-agenda-tab]"));
    const panels = Array.from(tabs.querySelectorAll("[data-agenda-panel]"));
    if (!tabLinks.length || !panels.length) return;

    function activateAgendaTab(id) {
      tabLinks.forEach(function (link) {
        const active = link.getAttribute("data-agenda-tab") === id;
        link.classList.toggle("w--current", active);
        link.setAttribute("aria-selected", active ? "true" : "false");
      });
      panels.forEach(function (panel) {
        panel.classList.toggle("w--tab-active", panel.getAttribute("data-agenda-panel") === id);
      });
    }

    tabLinks.forEach(function (link) {
      link.addEventListener("click", function (event) {
        event.preventDefault();
        activateAgendaTab(link.getAttribute("data-agenda-tab"));
      });
    });
  });

  const triggers = Array.from(document.querySelectorAll("[data-agenda-dialog-open]"));
  let returnFocus = null;

  function closeAgendaDialogs() {
    document.querySelectorAll(".btcpp-agenda-dialog.is-open").forEach(function (dialog) {
      dialog.classList.remove("is-open");
      dialog.setAttribute("aria-hidden", "true");
    });
    document.body.classList.remove("btcpp-agenda-dialog-open");
  }

  function dialogFromHash() {
    let anchor;
    try { anchor = decodeURIComponent(window.location.hash.slice(1)); }
    catch (_) { return null; }
    if (!anchor) return null;
    const dialog = document.getElementById("agenda-dialog-" + anchor);
    return dialog && dialog.classList.contains("btcpp-agenda-dialog") ? dialog : null;
  }

  function openAgendaDialog(dialog, scrollToTalk) {
    if (dialog.classList.contains("is-open")) return;
    closeAgendaDialogs();
    const trigger = triggers.find(function (item) {
      return item.getAttribute("data-agenda-dialog-open") === dialog.id;
    });
    if (trigger) {
      const panel = trigger.closest("[data-agenda-panel]");
      const tabs = panel && panel.closest(".tabs");
      if (tabs) {
        const tab = Array.from(tabs.querySelectorAll("[data-agenda-tab]")).find(function (item) {
          return item.getAttribute("data-agenda-tab") === panel.getAttribute("data-agenda-panel");
        });
        if (tab) tab.click();
      }
      const row = trigger.closest(".rebrand-agenda-row, .rebrand-schedule-card") || trigger;
      if (scrollToTalk) row.scrollIntoView({ block: "center" });
      returnFocus = trigger;
    }
    dialog.classList.add("is-open");
    dialog.setAttribute("aria-hidden", "false");
    document.body.classList.add("btcpp-agenda-dialog-open");
    dialog.querySelectorAll("[data-agenda-share]").forEach(function (link) { link.textContent = "Copy link"; });
    const closeButton = dialog.querySelector(".btcpp-agenda-dialog__close");
    if (closeButton) closeButton.focus({ preventScroll: true });
  }

  function syncAgendaDialog() {
    const dialog = dialogFromHash();
    if (dialog) {
      openAgendaDialog(dialog, true);
    } else {
      closeAgendaDialogs();
      const trigger = returnFocus;
      // History traversal restores document focus after popstate; return focus
      // to the talk once that native restoration has finished.
      if (trigger) window.requestAnimationFrame(function () {
        if (document.querySelector(".btcpp-agenda-dialog.is-open")) return;
        trigger.focus({ preventScroll: true });
        // Desktop agenda rows use display: contents on the button, so some
        // browsers need the containing row as the focus target instead.
        if (document.activeElement !== trigger) {
          const row = trigger.closest(".rebrand-agenda-row");
          if (row) {
            row.setAttribute("tabindex", "-1");
            row.focus({ preventScroll: true });
          }
        }
      });
      returnFocus = null;
    }
  }

  function dismissAgendaDialog() {
    if (!document.querySelector(".btcpp-agenda-dialog.is-open")) return;
    if (dialogFromHash() && history.state && history.state.agendaDialog) {
      history.back();
    } else {
      if (dialogFromHash()) {
        const url = new URL(window.location.href);
        url.hash = "agenda";
        history.replaceState(history.state, "", url);
      }
      syncAgendaDialog();
    }
  }

  triggers.forEach(function (trigger) {
    trigger.addEventListener("click", function (event) {
      const interactive = event.target.closest("a, button, input, select, textarea, label");
      if (interactive && interactive !== trigger) return;
      const dialog = document.getElementById(trigger.getAttribute("data-agenda-dialog-open"));
      if (!dialog) return;
      const url = new URL(window.location.href);
      url.hash = dialog.id.slice("agenda-dialog-".length);
      if (window.location.hash !== url.hash) {
        history.pushState(Object.assign({}, history.state, { agendaDialog: true }), "", url);
      }
      openAgendaDialog(dialog, false);
    });
    trigger.addEventListener("keydown", function (event) {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      trigger.click();
    });
  });

  document.querySelectorAll("[data-agenda-share]").forEach(function (link) {
    link.addEventListener("click", function (event) {
      // Keep a real permalink available for copying via the browser's link menu.
      if (!navigator.clipboard || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      navigator.clipboard.writeText(link.href).then(function () {
        link.textContent = "Link copied";
      }).catch(function () {
        link.textContent = "Copy link from address bar";
      });
    });
  });

  document.addEventListener("click", function (event) {
    if (event.target.closest("[data-agenda-dialog-close]")) dismissAgendaDialog();
  });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") dismissAgendaDialog();
  });
  window.addEventListener("popstate", syncAgendaDialog);
  window.addEventListener("hashchange", syncAgendaDialog);
  syncAgendaDialog();
})();
