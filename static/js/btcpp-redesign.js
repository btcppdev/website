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

  function closeAgendaDialogs() {
    document.querySelectorAll(".btcpp-agenda-dialog.is-open").forEach(function (dialog) {
      dialog.classList.remove("is-open");
      dialog.setAttribute("aria-hidden", "true");
    });
    document.body.classList.remove("btcpp-agenda-dialog-open");
  }

  document.querySelectorAll("[data-agenda-dialog-open]").forEach(function (trigger) {
    trigger.addEventListener("click", function (event) {
      const interactive = event.target.closest("a, button, input, select, textarea, label");
      if (interactive && interactive !== trigger) return;
      const id = trigger.getAttribute("data-agenda-dialog-open");
      const dialog = document.getElementById(id);
      if (!dialog) return;
      closeAgendaDialogs();
      dialog.classList.add("is-open");
      dialog.setAttribute("aria-hidden", "false");
      document.body.classList.add("btcpp-agenda-dialog-open");
      const closeButton = dialog.querySelector("[data-agenda-dialog-close]");
      if (closeButton) closeButton.focus({ preventScroll: true });
    });
    trigger.addEventListener("keydown", function (event) {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      trigger.click();
    });
  });

  document.addEventListener("click", function (event) {
    if (event.target.closest("[data-agenda-dialog-close]")) {
      closeAgendaDialogs();
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") {
      closeAgendaDialogs();
    }
  });
})();
