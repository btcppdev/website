(function () {
  "use strict";

  function projectRows(list) {
    return Array.from(list.querySelectorAll("tr[data-deliberation-project]"));
  }

  function scoredRows(list) {
    return projectRows(list).filter(function (row) {
      return row.dataset.scored === "true";
    });
  }

  function removeCutoff(list) {
    list.querySelectorAll("[data-deliberation-cutoff]").forEach(function (row) {
      row.remove();
    });
  }

  function renderCutoff(root, list, advanceCount) {
    removeCutoff(list);
    if (root.dataset.hasNext !== "true" || advanceCount < 1) return;

    var scored = scoredRows(list);
    var target = scored[advanceCount] || projectRows(list).find(function (row) {
      return row.dataset.scored !== "true";
    });
    if (!target) return;

    var cutoff = document.createElement("tr");
    cutoff.className = "hack-deliberation-cutoff";
    cutoff.dataset.deliberationCutoff = "";
    var cell = document.createElement("td");
    cell.colSpan = Number(root.dataset.columnCount || 1);
    var label = document.createElement("span");
    label.textContent = "Eliminated below";
    cell.appendChild(label);
    cutoff.appendChild(cell);
    list.insertBefore(cutoff, target);
  }

  function applyProjectOrder(root, list, order, advanceCount) {
    removeCutoff(list);
    var rows = projectRows(list);
    var byID = new Map(rows.map(function (row) {
      return [row.dataset.deliberationProject, row];
    }));
    var placed = new Set();

    order.forEach(function (projectID) {
      var row = byID.get(projectID);
      if (!row || row.dataset.scored !== "true" || placed.has(projectID)) return;
      list.appendChild(row);
      placed.add(projectID);
    });
    rows.forEach(function (row) {
      if (row.dataset.scored === "true" && !placed.has(row.dataset.deliberationProject)) {
        list.appendChild(row);
        placed.add(row.dataset.deliberationProject);
      }
    });
    rows.forEach(function (row) {
      if (row.dataset.scored !== "true") list.appendChild(row);
    });
    renderCutoff(root, list, advanceCount);
  }

  function initAdmin(root) {
    var list = root.querySelector("[data-deliberation-list]");
    if (!list) return;
    var countInput = root.querySelector("[data-deliberation-count]");
    var revisionInput = root.querySelector("[data-deliberation-revision-input]");
    var orderInputs = root.querySelector("[data-deliberation-order-inputs]");
    var status = root.querySelector("[data-deliberation-status]");
    var advanceForm = root.querySelector("[data-deliberation-advance-form]");
    var conflictAlert = root.querySelector("[data-deliberation-conflict]");
    var conflictMessage = root.querySelector("[data-deliberation-conflict-message]");
    var reloadButton = root.querySelector("[data-deliberation-reload]");
    var initialScoreOrder = scoredRows(list)
      .slice()
      .sort(function (a, b) {
        return Number(a.dataset.scorePosition) - Number(b.dataset.scorePosition);
      })
      .map(function (row) {
        return row.dataset.deliberationProject;
      });
    var revision = Number(root.dataset.revision || 0);
    var draggedRow = null;
    var moved = false;
    var saveTimer = null;
    var saveQueue = Promise.resolve(true);
    var blocked = false;
    var submitting = false;

    function currentOrder() {
      return scoredRows(list).map(function (row) {
        return row.dataset.deliberationProject;
      });
    }

    function currentCount() {
      if (!countInput) return 0;
      var count = Number(countInput.value || 0);
      var maximum = scoredRows(list).length;
      count = Math.max(1, Math.min(count, maximum));
      countInput.value = String(count);
      return count;
    }

    function setStatus(message, isError) {
      if (!status) return;
      status.textContent = message;
      status.classList.toggle("text-red-700", Boolean(isError));
      status.classList.toggle("text-gray-500", !isError);
    }

    function setAdvanceDisabled(disabled) {
      if (!advanceForm) return;
      var button = advanceForm.querySelector('button[type="submit"]');
      if (button) button.disabled = disabled;
    }

    function blockEditingForConflict(message) {
      root.querySelectorAll("[data-deliberation-move], [data-deliberation-reset], [data-deliberation-drag-handle], [data-deliberation-count]").forEach(function (control) {
        control.disabled = true;
        if (control.hasAttribute("draggable")) control.draggable = false;
      });
      if (!conflictAlert) {
        window.alert(message);
        return;
      }
      if (conflictMessage) conflictMessage.textContent = message + ". Reload before continuing so you do not overwrite another admin's changes.";
      conflictAlert.hidden = false;
      window.requestAnimationFrame(function () {
        conflictAlert.focus();
      });
    }

    function syncForm() {
      if (revisionInput) revisionInput.value = String(revision);
      if (!orderInputs) return;
      orderInputs.replaceChildren();
      currentOrder().forEach(function (projectID) {
        var input = document.createElement("input");
        input.type = "hidden";
        input.name = "ProjectID";
        input.value = projectID;
        orderInputs.appendChild(input);
      });
    }

    async function saveNow() {
      if (blocked) return false;
      window.clearTimeout(saveTimer);
      syncForm();
      setStatus("Saving deliberation order...", false);
      setAdvanceDisabled(true);

      var body = new URLSearchParams();
      body.set("JudgeEventID", root.dataset.eventId || "");
      body.set("Revision", String(revision));
      if (countInput) body.set("AdvanceCount", String(currentCount()));
      currentOrder().forEach(function (projectID) {
        body.append("ProjectID", projectID);
      });

      try {
        var response = await fetch(root.dataset.saveUrl, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            "X-Requested-With": "fetch"
          },
          body: body
        });
        var payload = await response.json().catch(function () { return {}; });
        if (!response.ok) {
          var responseError = new Error(payload.error || "Unable to save deliberation order");
          responseError.conflict = response.status === 409;
          throw responseError;
        }
        revision = Number(payload.revision || revision);
        root.dataset.revision = String(revision);
        syncForm();
        setStatus("Deliberation order saved", false);
        setAdvanceDisabled(false);
        return true;
      } catch (error) {
        blocked = Boolean(error && error.conflict);
        var message = (error && error.message) || "Unable to save deliberation order";
        setStatus(message, true);
        setAdvanceDisabled(blocked);
        if (blocked) blockEditingForConflict(message);
        return false;
      }
    }

    function queueSave() {
      saveQueue = saveQueue.then(saveNow);
      return saveQueue;
    }

    function scheduleSave() {
      window.clearTimeout(saveTimer);
      saveTimer = window.setTimeout(queueSave, 250);
    }

    function orderChanged() {
      renderCutoff(root, list, currentCount());
      syncForm();
      scheduleSave();
    }

    list.querySelectorAll("[data-deliberation-drag-handle]").forEach(function (handle) {
      handle.addEventListener("dragstart", function (event) {
        draggedRow = handle.closest("[data-deliberation-project]");
        if (!draggedRow) return;
        moved = false;
        draggedRow.classList.add("hack-deliberation-row--dragging");
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("text/plain", draggedRow.dataset.deliberationProject || "");
      });
      handle.addEventListener("dragend", function () {
        if (draggedRow) draggedRow.classList.remove("hack-deliberation-row--dragging");
        draggedRow = null;
        if (moved) orderChanged();
      });
    });

    list.addEventListener("dragover", function (event) {
      if (!draggedRow) return;
      var target = event.target.closest('tr[data-deliberation-project][data-scored="true"]');
      if (!target || target === draggedRow) return;
      event.preventDefault();
      removeCutoff(list);
      var box = target.getBoundingClientRect();
      list.insertBefore(draggedRow, event.clientY < box.top + box.height / 2 ? target : target.nextSibling);
      moved = true;
    });

    list.addEventListener("click", function (event) {
      var button = event.target.closest("[data-deliberation-move]");
      if (!button) return;
      var row = button.closest('[data-deliberation-project][data-scored="true"]');
      var rows = scoredRows(list);
      var index = rows.indexOf(row);
      if (button.dataset.deliberationMove === "up" && index > 0) {
        list.insertBefore(row, rows[index - 1]);
        orderChanged();
      } else if (button.dataset.deliberationMove === "down" && index >= 0 && index < rows.length - 1) {
        list.insertBefore(rows[index + 1], row);
        orderChanged();
      }
    });

    root.querySelectorAll("[data-deliberation-reset]").forEach(function (button) {
      button.addEventListener("click", function () {
        applyProjectOrder(root, list, initialScoreOrder, currentCount());
        syncForm();
        scheduleSave();
      });
    });

    if (countInput) {
      countInput.addEventListener("input", function () {
        renderCutoff(root, list, currentCount());
        scheduleSave();
      });
    }

    if (advanceForm) {
      advanceForm.addEventListener("submit", async function (event) {
        if (submitting) return;
        event.preventDefault();
        var submitter = event.submitter;
        if (!(await queueSave())) return;
        syncForm();
        submitting = true;
        if (submitter) {
          advanceForm.requestSubmit(submitter);
        } else {
          advanceForm.submit();
        }
      });
    }

    if (reloadButton) {
      reloadButton.addEventListener("click", function () {
        window.location.reload();
      });
    }

    renderCutoff(root, list, currentCount());
    syncForm();
  }

  document.querySelectorAll("[data-deliberation-admin]").forEach(initAdmin);
})();
