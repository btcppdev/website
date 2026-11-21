(() => {
  "use strict";
  const root = document.getElementById("publishing");
  if (!root) return;
  const $ = (s) => root.querySelector(s),
    el = (tag, text, cls) => {
      const n = document.createElement(tag);
      if (text !== undefined) n.textContent = text;
      if (cls) n.className = cls;
      return n;
    };
  const views = ["week", "month", "agenda", "queue", "channels"];
  const phoneLayout = window.matchMedia("(max-width: 950px)");
  $("#filters").open = !phoneLayout.matches;
  phoneLayout.addEventListener("change", () => {
    $("#filters").open = !phoneLayout.matches;
  });
  let view = views.includes(location.hash.slice(1))
      ? location.hash.slice(1)
      : "week",
    anchor = new Date(),
    data = {
      channels: [],
      entries: [],
      deliveries: [],
      recordings: [],
      slots: [],
    },
    media = [],
    loading = 0;
  let dragged = null,
    pointerDrag = null,
    dragFrame = 0,
    moveDelivery = null,
    moveSaving = false,
    suppressClickUntil = 0,
    moveStatusTimer = 0,
    dragRecordingSlots = null,
    recordingPreview = [],
    recordingOrder = [],
    recordingDragID = null,
    recordingPreviewRequest = 0,
    recordingFocus = "",
    initialLoad = true;
  const initialParams = new URLSearchParams(location.search);
  let zone = localStorage.getItem("publishing-timezone") || "America/Chicago";
  $("#timezone").value = zone;
  const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  function parts(date) {
    return Object.fromEntries(
      new Intl.DateTimeFormat("en-CA", {
        timeZone: zone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      })
        .formatToParts(new Date(date))
        .map((x) => [x.type, x.value]),
    );
  }
  function dayKey(date) {
    const p = parts(date);
    return `${p.year}-${p.month}-${p.day}`;
  }
  function localUTC(value) {
    const [d, t] = value.split("T"),
      [y, m, day] = d.split("-").map(Number),
      [h, min] = t.split(":").map(Number);
    const desired = Date.UTC(y, m - 1, day, h, min);
    let guess = desired;
    for (let i = 0; i < 3; i++) {
      const p = parts(guess);
      guess +=
        desired - Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute);
    }
    if (
      dayKey(guess) !== d ||
      new Intl.DateTimeFormat("en-GB", {
        timeZone: zone,
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }).format(guess) !== t
    )
      throw Error(
        "This local time does not exist because the clocks change. Choose another time.",
      );
    return new Date(guess).toISOString();
  }
  function civilDate(date) {
    return new Date(dayKey(date) + "T12:00:00Z");
  }
  function bounds() {
    let start = civilDate(anchor);
    if (view === "month") start.setUTCDate(1);
    if (view === "week" || view === "month")
      start.setUTCDate(start.getUTCDate() - ((start.getUTCDay() + 6) % 7));
    const count = view === "month" ? 42 : view === "week" ? 7 : 31;
    let end = new Date(start);
    end.setUTCDate(end.getUTCDate() + count);
    return { start, end, count };
  }
  function label(at, full = false) {
    if (!at) return "Unscheduled";
    return new Intl.DateTimeFormat("en-US", {
      timeZone: zone,
      ...(full ? { month: "short", day: "numeric" } : {}),
      hour: "numeric",
      minute: "2-digit",
    }).format(new Date(at));
  }
  function channel(id) {
    const c = data.channels.find((c) => c.id === id) || {
      name: id,
      platform: "",
      provider: "",
    };
    return id === "broadcasts" ? { ...c, name: "Bitcoin++ · X Broadcast" } : c;
  }
  function notice(msg) {
    $("#notice").textContent = msg;
  }
  async function api(body) {
    const res = await fetch("/admin/publishing/action", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": root.dataset.csrf,
      },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw Error(await res.text());
    return res.json();
  }
  async function load() {
    const seq = ++loading;
    const b = bounds();
    $("#calendar").setAttribute("aria-busy", "true");
    try {
      const res = await fetch(
        "/admin/publishing/data?" +
          new URLSearchParams({
            start: localUTC(b.start.toISOString().slice(0, 10) + "T00:00"),
            end: localUTC(b.end.toISOString().slice(0, 10) + "T00:00"),
          }),
      );
      if (!res.ok) throw Error(await res.text());
      const next = await res.json();
      if (seq !== loading) return;
      data = next;
      filters();
      if (initialLoad && initialParams.get("event"))
        $("#event").value = initialParams.get("event");
      render();
      if (initialLoad) {
        initialLoad = false;
        if (initialParams.get("recordings") === "1")
          openRecordingScheduler(
            initialParams.get("recording") || "",
            initialParams.get("recording_channel") || "",
          );
      }
      const streamFreshness = data.streamer_synced_at
        ? "Streamer updated " + label(data.streamer_synced_at, true)
        : "Streamer has not synced yet";
      $("#freshness").textContent = streamFreshness + " · " + zone;
      notice(
        !data.recording_worker_enabled
          ? "Publishing is paused on the server. Schedule changes stay pending until publishing is enabled."
          : data.worker_enabled
            ? ""
            : "Recording scheduling is enabled. Connect X to enable direct social posts.",
      );
    } catch (e) {
      notice(e.message);
    } finally {
      if (seq === loading) $("#calendar").setAttribute("aria-busy", "false");
    }
  }
  function filters() {
    for (const [id, items] of [
      [
        "channel",
        data.channels.map((c) => [
          c.id,
          channel(c.id).name + " · " + c.platform,
        ]),
      ],
      [
        "event",
        (data.events || []).map((x) => [x.tag, x.tag + " · " + x.name]),
      ],
    ]) {
      const select = $("#" + id),
        selected = select.value;
      while (select.options.length > 1) select.remove(1);
      items.forEach(([v, t]) => select.add(new Option(t, v)));
      select.value = selected;
    }
    const list = $("#events");
    list.replaceChildren();
    (data.events || []).forEach((x) => list.append(new Option(x.name, x.tag)));
    const picker = $("#recording-event");
    picker.replaceChildren();
    (data.events || []).forEach((x) =>
      picker.add(new Option(x.tag + " · " + x.name, x.tag)),
    );
    recordingLinks();
  }
  function matches(e) {
    return (
      (!$("#status").value || e.state === $("#status").value) &&
      (!$("#channel").value || e.channel_id === $("#channel").value) &&
      (!$("#event").value || e.conference === $("#event").value) &&
      (!$("#kind").value || e.kind === $("#kind").value)
    );
  }
  function deliveryFor(e) {
    return e.id.startsWith("delivery:")
      ? data.deliveries.find((d) => d.id === e.id.slice(9))
      : (data.recordings || []).find((d) => d.id === e.id);
  }
  function isRecording(d) {
    return Boolean(d?.recording_id);
  }
  function stateLabel(e) {
    const d = deliveryFor(e);
    return d?.cancel_pending ? "cancellation · " + e.state : e.state;
  }
  function canMove(d) {
    if (isRecording(d)) return d.eligible && Boolean(d.at);
    return (
      d &&
      d.state === "scheduled" &&
      !d.root_id &&
      d.at &&
      channel(d.channel_id).provider === "direct"
    );
  }
  function slotFree(s) {
    return (
      new Date(s.at).getTime() > Date.now() + 60000 &&
      !data.entries.some(
        (e) =>
          e.kind !== "slot" &&
          e.channel_id === s.channel_id &&
          [
            "scheduled",
            "sending",
            "pending",
            "syncing",
            "needs_review",
            "failed",
            "live",
          ].includes(e.state) &&
          new Date(e.at).getTime() === new Date(s.at).getTime(),
      )
    );
  }
  function validTarget(node) {
    return (
      dragged &&
      node &&
      node.dataset.slotChannel === dragged.channel_id &&
      (!isRecording(dragged) ||
        dragRecordingSlots?.some(
          (s) =>
            new Date(s.at).getTime() ===
            new Date(node.dataset.slotAt).getTime(),
        )) &&
      data.slots.some(
        (s) =>
          s.channel_id === node.dataset.slotChannel &&
          s.at === node.dataset.slotAt &&
          slotFree(s),
      )
    );
  }
  function highlightSlots() {
    root
      .querySelectorAll("[data-slot-at]")
      .forEach((n) =>
        n.classList.toggle("is-drop-target", Boolean(validTarget(n))),
      );
  }
  function hoverSlot(node) {
    root
      .querySelectorAll(".is-drop-hover")
      .forEach((n) => n.classList.remove("is-drop-hover"));
    if (validTarget(node)) node.classList.add("is-drop-hover");
  }
  function moveStatus(message, persistent = false) {
    clearTimeout(moveStatusTimer);
    $("#move-status").textContent = message;
    if (!persistent)
      moveStatusTimer = setTimeout(() => {
        $("#move-status").textContent = "";
      }, 10000);
  }
  function beginDrag(d) {
    dragged = d;
    root.classList.add("pub-dragging");
    dragRecordingSlots = null;
    highlightSlots();
    if (isRecording(d)) {
      moveStatus("Checking available slots and broadcast runtimes…", true);
      fetchAvailableSlots(d)
        .then((slots) => {
          if (dragged !== d) return;
          dragRecordingSlots = slots;
          highlightSlots();
          hoverSlot(pointerTarget());
          moveStatus(
            "Move to a highlighted " +
              channel(d.channel_id).name +
              " slot. Other destinations keep their times.",
            true,
          );
        })
        .catch((e) => {
          if (dragged === d) moveStatus(e.message, true);
        });
      return;
    }
    moveStatus(
      "Move " +
        channel(d.channel_id).name +
        " to one of its highlighted publication slots. Escape cancels.",
      true,
    );
  }
  function endDrag() {
    dragged = null;
    dragRecordingSlots = null;
    root.classList.remove("pub-dragging");
    root
      .querySelectorAll(".is-drop-target, .is-drop-hover")
      .forEach((n) => n.classList.remove("is-drop-target", "is-drop-hover"));
    $(".pub-drag-ghost")?.remove();
    if (dragFrame) cancelAnimationFrame(dragFrame);
    dragFrame = 0;
  }
  function pointerTarget() {
    return (
      pointerDrag &&
      document
        .elementFromPoint(pointerDrag.x, pointerDrag.y)
        ?.closest("[data-slot-at]")
    );
  }
  function scrollDrag() {
    if (!pointerDrag?.active) return;
    const y = pointerDrag.y;
    const speed = y < 70 ? -14 : y > innerHeight - 70 ? 14 : 0;
    if (speed) window.scrollBy(0, speed);
    hoverSlot(pointerTarget());
    dragFrame = requestAnimationFrame(scrollDrag);
  }
  async function moveTo(d, at) {
    if (moveSaving) return;
    moveSaving = true;
    try {
      await api({
        action: isRecording(d) ? "recording_move" : "move",
        id: d.id,
        ...(isRecording(d) ? { revision: d.revision } : { version: d.version }),
        target_channel: d.channel_id,
        at,
      });
      await load();
      moveStatus(
        (isRecording(d) ? "Reserved " : "Moved ") +
          channel(d.channel_id).name +
          " to " +
          label(at, true) +
          " (" +
          zone +
          (isRecording(d)
            ? "). Pending confirmation from the destination; other items are unchanged."
            : "). Other items are unchanged."),
      );
      return true;
    } finally {
      moveSaving = false;
    }
  }
  function moveHandle(d) {
    const handle = el("button", "⠿", "pub-drag-handle");
    handle.type = "button";
    handle.title = "Drag to a publication slot, or click to choose one";
    handle.setAttribute(
      "aria-label",
      "Move " +
        (d.title || d.body?.slice(0, 70) || "Recording") +
        " · " +
        channel(d.channel_id).name,
    );
    handle.onclick = () => {
      if (performance.now() >= suppressClickUntil) openMove(d);
    };
    handle.onpointerdown = (ev) => {
      if (ev.button !== 0 || moveSaving) return;
      pointerDrag = {
        id: ev.pointerId,
        d,
        handle,
        x: ev.clientX,
        y: ev.clientY,
        startX: ev.clientX,
        startY: ev.clientY,
        active: false,
      };
      handle.setPointerCapture(ev.pointerId);
    };
    handle.onpointermove = (ev) => {
      if (!pointerDrag || pointerDrag.id !== ev.pointerId) return;
      pointerDrag.x = ev.clientX;
      pointerDrag.y = ev.clientY;
      if (
        !pointerDrag.active &&
        Math.hypot(
          ev.clientX - pointerDrag.startX,
          ev.clientY - pointerDrag.startY,
        ) > 6
      ) {
        pointerDrag.active = true;
        beginDrag(d);
        const ghost = el(
          "div",
          channel(d.channel_id).name +
            " · " +
            (d.title || d.body?.slice(0, 60) || "Recording"),
          "pub-drag-ghost",
        );
        root.append(ghost);
        dragFrame = requestAnimationFrame(scrollDrag);
      }
      if (pointerDrag.active) {
        ev.preventDefault();
        const ghost = $(".pub-drag-ghost");
        ghost.style.left = Math.min(ev.clientX + 14, innerWidth - 240) + "px";
        ghost.style.top = Math.max(8, ev.clientY - 48) + "px";
        hoverSlot(pointerTarget());
      }
    };
    handle.onpointerup = async (ev) => {
      if (!pointerDrag || pointerDrag.id !== ev.pointerId) return;
      const active = pointerDrag.active,
        target = pointerTarget(),
        at = validTarget(target) ? target.dataset.slotAt : null;
      pointerDrag = null;
      if (handle.hasPointerCapture(ev.pointerId))
        handle.releasePointerCapture(ev.pointerId);
      if (!active) return;
      suppressClickUntil = performance.now() + 500;
      endDrag();
      if (!at) {
        moveStatus(
          "Post unchanged. Choose an available publication slot for this account.",
        );
        return;
      }
      try {
        await moveTo(d, at);
      } catch (e) {
        moveStatus(e.message);
      }
    };
    handle.onpointercancel = handle.onlostpointercapture = () => {
      if (!pointerDrag) return;
      pointerDrag = null;
      endDrag();
      moveStatus("Move canceled. Post unchanged.");
    };
    return handle;
  }
  function card(e) {
    const button = el("button", undefined, "pub-card");
    button.type = "button";
    button.dataset.kind = e.kind;
    button.dataset.channel = e.channel_id;
    button.dataset.entry = e.id;
    const account = channel(e.channel_id);
    const kind =
      e.kind === "recording"
        ? "YouTube release"
        : e.kind === "stream"
          ? "Broadcast"
          : e.kind === "slot"
            ? "Publication slot"
            : "Post";
    button.append(
      el("small", label(e.at)),
      el("strong", e.kind === "slot" ? "Publication slot" : e.title),
      el("small", (account.platform === "x" ? "X · " : "") + account.name),
      el(
        "span",
        kind +
          (e.conference ? " · " + e.conference : "") +
          (e.state ? " · " + stateLabel(e) : ""),
        "pub-status",
      ),
    );
    button.onclick = () => {
      if (performance.now() >= suppressClickUntil) details(e);
    };
    if (
      e.kind === "slot" &&
      ["direct", "recordings"].includes(account.provider) &&
      slotFree(e)
    ) {
      button.dataset.slotAt = e.at;
      button.dataset.slotChannel = e.channel_id;
      button.ondragover = (ev) => {
        if (validTarget(button)) {
          ev.preventDefault();
          ev.dataTransfer.dropEffect = "move";
          hoverSlot(button);
        }
      };
      button.ondragleave = () => button.classList.remove("is-drop-hover");
      button.ondrop = async (ev) => {
        ev.preventDefault();
        if (!validTarget(button)) return;
        const d = dragged;
        endDrag();
        suppressClickUntil = performance.now() + 500;
        try {
          await moveTo(d, e.at);
        } catch (err) {
          moveStatus(err.message);
        }
      };
    }
    const d = deliveryFor(e);
    if (!canMove(d)) return button;
    button.draggable = true;
    button.ondragstart = (ev) => {
      if (moveSaving) {
        ev.preventDefault();
        return;
      }
      ev.dataTransfer.setData("text/plain", d.id);
      ev.dataTransfer.effectAllowed = "move";
      beginDrag(d);
    };
    button.ondragend = () => {
      if (dragged)
        moveStatus(
          "Post unchanged. Choose an available publication slot for this account.",
        );
      endDrag();
      suppressClickUntil = performance.now() + 300;
    };
    const item = el("div", undefined, "pub-item");
    item.append(button, moveHandle(d));
    return item;
  }
  async function fetchAvailableSlots(d) {
    const res = await fetch(
      "/admin/publishing/slots?" + new URLSearchParams({ id: d.id }),
    );
    if (!res.ok) throw Error(await res.text());
    return res.json();
  }
  async function openMove(d) {
    if (moveSaving) return;
    for (const id of ["detail", "calendar-day"])
      if ($("#" + id).open) $("#" + id).close();
    moveDelivery = d;
    $("#move-description").textContent =
      (d.title || d.body?.slice(0, 100) || "Recording") +
      " · " +
      channel(d.channel_id).name;
    $("#move-error").textContent = "";
    $("#move-zone").textContent = "Times in " + zone + " · Next 90 days";
    $("#move-slot").replaceChildren(
      new Option("Loading publication slots…", ""),
    );
    $("#move-form [type=submit]").disabled = true;
    $("#move-post").showModal();
    try {
      const slots = await fetchAvailableSlots(d);
      if (moveDelivery !== d || !$("#move-post").open) return;
      $("#move-slot").replaceChildren(
        new Option(
          slots.length
            ? "Choose a publication slot"
            : "No available publication slots",
          "",
        ),
      );
      slots.forEach((s) =>
        $("#move-slot").add(new Option(label(s.at, true), s.at)),
      );
      $("#move-form [type=submit]").disabled = !slots.length;
      if (!slots.length)
        $("#move-error").textContent =
          "Add publication slots in Channels & schedules, or free an occupied slot.";
    } catch (e) {
      $("#move-error").textContent = e.message;
    }
  }
  $("#close-move").onclick = () => $("#move-post").close();
  $("#move-post").addEventListener("close", () => {
    moveDelivery = null;
  });
  $("#move-form").onsubmit = async (ev) => {
    ev.preventDefault();
    const d = moveDelivery;
    if (!d) return;
    const button = $("#move-form [type=submit]");
    button.disabled = true;
    try {
      await moveTo(d, $("#move-slot").value);
      $("#move-post").close();
    } catch (e) {
      $("#move-error").textContent = e.message;
    } finally {
      button.disabled = false;
    }
  };
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && (dragged || pointerDrag)) {
      suppressClickUntil = performance.now() + 500;
      pointerDrag = null;
      endDrag();
      moveStatus("Move canceled. Item unchanged.");
    }
  });
  function render() {
    root
      .querySelectorAll("[data-view]")
      .forEach((b) =>
        b.setAttribute("aria-pressed", String(b.dataset.view === view)),
      );
    const container = $("#calendar");
    container.replaceChildren();
    $(".pub-toolbar").hidden = view === "channels";
    const b = bounds();
    $("#calendar-zone").textContent = "Times in " + zone;
    const activeFilters = ["event", "channel", "kind", "status"].filter(
      (id) => $("#" + id).value,
    ).length;
    $("#filter-count").textContent = activeFilters
      ? activeFilters + " active"
      : "All posts";
    $("#range").textContent = new Intl.DateTimeFormat("en-US", {
      timeZone: "UTC",
      month: "long",
      year: "numeric",
      ...(view === "week" ? { day: "numeric" } : {}),
    }).format(view === "month" ? civilDate(anchor) : b.start);
    if (view === "channels") {
      renderChannels(container);
      return;
    }
    if (view === "queue") {
      if (
        !["recording", "stream"].includes($("#kind").value) &&
        !["youtube", "broadcasts"].includes($("#channel").value)
      )
        renderQueue(container);
      renderRecordingQueue(container);
      return;
    }
    const entries = data.entries.filter(matches);
    if (view === "agenda") {
      const list = el("div", undefined, "pub-list");
      for (const e of entries) {
        const row = el("article");
        row.append(el("small", label(e.at, true) + " · " + zone), card(e));
        list.append(row);
      }
      container.append(list);
      if (!entries.length)
        container.append(
          el(
            "p",
            "No posts, recordings or streams in this period.",
            "pub-empty",
          ),
        );
      return;
    }
    const legend = el("p", undefined, "pub-legend");
    legend.append(
      el(
        "span",
        "Drag a post, YouTube release, or broadcast to an available slot for its channel. Use the move handle to choose a slot by touch or keyboard.",
        view === "month" ? "pub-month-key" : "",
      ),
    );
    if (view === "month")
      legend.append(
        el(
          "span",
          "Tap a day for details · Numbers: posts, releases & streams · Dots: default slots",
          "pub-month-hint",
        ),
      );
    container.append(legend);
    const grid = el(
      "div",
      undefined,
      "pub-grid" + (view === "month" ? " pub-month" : " pub-week"),
    );
    for (let i = 0; i < b.count; i++) {
      const d = new Date(b.start);
      d.setUTCDate(d.getUTCDate() + i);
      const key = d.toISOString().slice(0, 10);
      const cell = el(
        "section",
        undefined,
        "pub-day" +
          (key === dayKey(new Date()) ? " is-today" : "") +
          (view === "month" &&
          d.getUTCMonth() !== civilDate(anchor).getUTCMonth()
            ? " is-outside-month"
            : ""),
      );
      const head = el("h3", days[d.getUTCDay()]);
      head.append(el("b", String(d.getUTCDate())));
      cell.append(head);
      const booked = entries.filter((e) => e.at && dayKey(e.at) === key);
      const slots = data.slots.filter(
        (s) =>
          s.at &&
          dayKey(s.at) === key &&
          (!$("#channel").value || s.channel_id === $("#channel").value) &&
          (!$("#kind").value ||
            $("#kind").value ===
              (s.channel_id === "youtube"
                ? "recording"
                : s.channel_id === "broadcasts"
                  ? "stream"
                  : "post")) &&
          (!$("#status").value || $("#status").value === "scheduled") &&
          !data.entries.some(
            (e) =>
              e.channel_id === s.channel_id &&
              [
                "scheduled",
                "sending",
                "pending",
                "syncing",
                "needs_review",
                "failed",
                "live",
              ].includes(e.state) &&
              new Date(e.at).getTime() === new Date(s.at).getTime(),
          ),
      );
      const dayEntries = [...booked, ...slots].sort(
        (a, b) => new Date(a.at) - new Date(b.at),
      );
      const content = el("div", undefined, "pub-day-content");
      dayEntries.forEach((e) => content.append(card(e)));
      if (!dayEntries.length)
        content.append(el("p", "No posts scheduled", "pub-day-empty"));
      cell.append(content);
      if (view === "month") {
        const dateLabel = new Intl.DateTimeFormat("en-US", {
          timeZone: "UTC",
          weekday: "long",
          month: "long",
          day: "numeric",
          year: "numeric",
        }).format(d);
        const counts = [
          booked.length
            ? booked.length + (booked.length === 1 ? " post" : " posts")
            : "",
          slots.length
            ? slots.length + (slots.length === 1 ? " slot" : " slots")
            : "",
        ].filter(Boolean);
        const summary = el("button", undefined, "pub-day-summary");
        summary.type = "button";
        summary.setAttribute(
          "aria-label",
          dateLabel + ": " + (counts.join(", ") || "No posts scheduled"),
        );
        if (key === dayKey(new Date()))
          summary.setAttribute("aria-current", "date");
        if (booked.length)
          summary.append(el("span", String(booked.length), "pub-day-count"));
        else if (slots.length) summary.append(el("span", "·", "pub-slot-dot"));
        summary.onclick = () => {
          $("#calendar-day-title").textContent = dateLabel;
          $("#calendar-day-zone").textContent = "Times in " + zone;
          const list = $("#calendar-day-entries");
          list.replaceChildren();
          dayEntries.forEach((e) => list.append(card(e)));
          if (!dayEntries.length)
            list.append(
              el("p", "No posts scheduled for this day.", "pub-empty"),
            );
          $("#calendar-day").showModal();
        };
        cell.append(summary);
      }
      grid.append(cell);
    }
    container.append(grid);
    const attention = entries.filter(
      (e) =>
        !e.at &&
        !["disabled", "past", "ended", "published", "canceled"].includes(
          e.state,
        ),
    );
    if (attention.length) {
      container.append(el("h3", "Needs a time / review"));
      attention.forEach((e) => container.append(card(e)));
    }
  }
  function actionButton(text, fn) {
    const b = el("button", text);
    b.type = "button";
    b.onclick = async () => {
      b.disabled = true;
      try {
        await fn();
      } catch (e) {
        if ($("#detail").open) {
          let error = $("#detail-body .pub-action-error");
          if (!error) {
            error = el("p", undefined, "pub-action-error pub-recording-error");
            error.setAttribute("role", "alert");
            $("#detail-body").append(error);
          }
          error.textContent = e.message;
        } else notice(e.message);
      } finally {
        b.disabled = false;
      }
    };
    return b;
  }
  function renderQueue(container) {
    const rows = data.deliveries.filter(
      (d) =>
        matches({ ...d, kind: "post" }) &&
        !["published", "canceled"].includes(d.state),
    );
    container.append(
      el(
        "p",
        "Queues are shared across events for each account. Fixed-time posts keep their time. Paused accounts retain their queue.",
        "pub-legend",
      ),
    );
    const list = el("div", undefined, "pub-list");
    for (const d of rows) {
      const row = el("article");
      row.append(
        el(
          "small",
          channel(d.channel_id).name +
            " · " +
            d.state +
            " · " +
            label(d.at, true),
        ),
        el("h3", d.title || d.body.slice(0, 100)),
        el("p", d.body),
      );
      if (d.error) row.append(el("p", d.error));
      const actions = el("div", undefined, "pub-actions");
      if (["draft", "scheduled", "failed"].includes(d.state) && !d.root_id)
        actions.append(actionButton("Edit", () => edit(d)));
      if (canMove(d))
        actions.append(actionButton("Move to slot", () => openMove(d)));
      if (d.mode === "queue" && d.state === "scheduled")
        actions.append(actionButton("Move to top", () => act(d, "top")));
      if (d.state === "failed")
        actions.append(actionButton("Retry", () => act(d, "retry")));
      if (d.state === "needs_review")
        actions.append(
          actionButton("Review result", () =>
            details({
              id: "delivery:" + d.id,
              channel_id: d.channel_id,
              title: d.title || d.body,
              state: d.state,
              provider: "direct",
            }),
          ),
        );
      if (!["sending", "needs_review"].includes(d.state))
        actions.append(actionButton("Cancel", () => act(d, "cancel")));
      row.append(actions);
      list.append(row);
    }
    container.append(list);
    if (!rows.length)
      container.append(
        el(
          "p",
          "Your queue is clear. Create a draft or add a post to the next slot.",
          "pub-empty",
        ),
      );
  }
  function renderRecordingQueue(container) {
    const rows = (data.recordings || []).filter(
      (d) =>
        ((d.at && new Date(d.at) > new Date()) ||
          ["pending", "syncing", "failed", "needs_review"].includes(d.state)) &&
        matches(recordingEntry(d)),
    );
    if (!rows.length) return;
    container.append(el("h3", "Recording schedules"));
    const list = el("div", undefined, "pub-list");
    rows.sort((a, b) => new Date(a.at || 0) - new Date(b.at || 0));
    for (const d of rows) {
      const row = el("article");
      row.append(el("small", labelTime(d.at)), card(recordingEntry(d)));
      list.append(row);
    }
    container.append(list);
  }
  async function act(d, action, url = "") {
    await api({ action, id: d.id, version: d.version, url });
    await load();
  }
  function renderChannels(container) {
    const intro = el("div", undefined, "pub-list");
    intro.append(
      el(
        "p",
        "Manage X posts, YouTube releases, and X broadcasts here. Each destination has its own schedule. Instagram and LinkedIn are planned next.",
      ),
    );
    if (data.x_configured) {
      const connect = el("a", "Connect an X account");
      connect.href = "/admin/publishing/connect-x";
      intro.append(connect);
    } else
      intro.append(
        el(
          "p",
          "X connection needs server configuration: X_OAUTH_CLIENT_ID, X_OAUTH_CLIENT_SECRET, X_OAUTH_REDIRECT_URL, and SOCIAL_STATE_KEY.",
        ),
      );
    intro.append(
      el(
        "p",
        data.youtube_connected
          ? "YouTube is connected. Schedule releases here; use the media library for uploads."
          : "Connect YouTube from an event’s recordings page.",
      ),
    );
    container.append(intro);
    const grid = el("div", undefined, "pub-channels");
    for (const c of data.channels) {
      const row = el("article", undefined, "pub-channel");
      row.append(
        el("h3", channel(c.id).name),
        el(
          "span",
          c.platform.toUpperCase() +
            " · " +
            (c.provider === "direct" ? "Direct publishing" : "Recordings"),
          "pub-badge",
        ),
      );
      if (
        c.provider !== "direct" &&
        !["youtube", "broadcasts"].includes(c.id)
      ) {
        row.append(
          el(
            "p",
            "Manage releases and full-duration broadcast reservations from the recording entry.",
          ),
          el("p", c.timezone),
        );
        if (c.slots?.length)
          row.append(
            el("p", c.slots.map((s) => days[s.day] + " " + s.time).join(" · ")),
          );
        grid.append(row);
        continue;
      }
      const form = el("form"),
        zoneLabel = el("label", "Account timezone"),
        zoneInput = el("input");
      zoneInput.value = c.timezone;
      zoneInput.required = true;
      zoneLabel.append(zoneInput);
      const slotLabel = el(
          "label",
          "Default posting slots · one per line: Mon 10:00",
        ),
        slots = el("textarea");
      slots.value = (c.slots || [])
        .map((s) => days[s.day] + " " + s.time)
        .join("\n");
      slotLabel.append(slots);
      const pausedLabel = el("label", undefined, "pub-checkbox-label"),
        paused = el("input");
      paused.type = "checkbox";
      paused.checked = c.paused;
      pausedLabel.append(
        paused,
        document.createTextNode(" Pause this account"),
      );
      const routingLabel = el("label", undefined, "pub-checkbox-label"),
        routing = el("input");
      routing.type = "checkbox";
      routing.checked = c.route_event_posts;
      routingLabel.append(
        routing,
        document.createTextNode(" Use for event X announcements"),
      );
      const save = el("button", "Save schedule");
      save.type = "submit";
      form.append(zoneLabel, slotLabel);
      if (c.provider === "direct") form.append(pausedLabel, routingLabel);
      form.append(save);
      form.onsubmit = async (ev) => {
        ev.preventDefault();
        save.disabled = true;
        try {
          const parsed = slots.value.trim()
            ? slots.value
                .trim()
                .split("\n")
                .map((line) => {
                  const m =
                    /^(Sun|Mon|Tue|Wed|Thu|Fri|Sat)\s+(\d{2}:\d{2})$/i.exec(
                      line.trim(),
                    );
                  if (!m)
                    throw Error("Use one slot per line, such as Mon 10:00.");
                  return {
                    day: days.findIndex(
                      (x) => x.toLowerCase() === m[1].toLowerCase(),
                    ),
                    time: m[2],
                  };
                })
            : [];
          await api({
            action: "channel",
            channel: {
              ...c,
              timezone: zoneInput.value,
              slots: parsed,
              paused: paused.checked,
              route_event_posts: routing.checked,
            },
          });
          await load();
        } catch (e) {
          notice(e.message);
        } finally {
          save.disabled = false;
        }
      };
      row.append(form);
      grid.append(row);
    }
    for (const platform of ["Instagram", "LinkedIn"]) {
      const row = el("article", undefined, "pub-channel");
      row.append(
        el("h3", platform),
        el("span", "Planned", "pub-badge"),
        el(
          "p",
          "Direct account connection and publishing are coming in the next phase.",
        ),
      );
      grid.append(row);
    }
    container.append(grid);
  }
  function previews() {
    const target = $("#media-preview");
    target.replaceChildren();
    media.forEach((m, i) => {
      const wrap = el("div"),
        img = el("img");
      img.src = m.url;
      img.alt = "Attachment " + (i + 1);
      wrap.append(
        img,
        actionButton("Remove", () => {
          media.splice(i, 1);
          previews();
        }),
      );
      target.append(wrap);
    });
  }
  function edit(d = {}) {
    if ($("#calendar-day").open) $("#calendar-day").close();
    const form = $("#post-form");
    form.reset();
    form.elements.client_key.value = crypto.randomUUID();
    $("#editor-error").textContent = "";
    const accounts = data.channels.filter(
      (c) => c.provider === "direct" && c.platform === "x",
    );
    if (!accounts.length) {
      view = "channels";
      render();
      notice(
        "Connect an X account to create posts. Use the recording entry for YouTube releases.",
      );
      return;
    }
    const options = $("#account-options");
    options.replaceChildren();
    for (const [i, c] of accounts.entries()) {
      const label = el("label", undefined, "pub-account-option"),
        input = el("input");
      input.type = "checkbox";
      input.name = "channel_ids";
      input.value = c.id;
      input.checked = d.id
        ? c.id === d.channel_id
        : d.channel_id
          ? c.id === d.channel_id
          : i === 0;
      input.disabled = Boolean(d.id);
      label.append(
        input,
        el("span", "X · " + c.name + (c.paused ? " · paused" : "")),
      );
      options.append(label);
    }
    form.elements.channel_id.value = d.channel_id || accounts[0].id;
    $("#account-help").textContent = d.id
      ? "Editing only this account’s item. Other copies are unchanged."
      : "Each selected account gets its own item and schedule. You can move or edit each copy independently.";
    for (const key of [
      "id",
      "version",
      "channel_id",
      "conference",
      "title",
      "body",
      "reply",
      "mode",
    ])
      if (d[key] !== undefined) form.elements[key].value = d[key];
    if (d.at) {
      const p = parts(d.at);
      form.elements.at.value = `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
    }
    media = [...(d.media || [])];
    previews();
    $("#editor-title").textContent = d.id ? "Edit post" : "New post";
    $("#input-timezone").textContent = zone;
    $("#fixed-time").hidden = form.elements.mode.value !== "fixed";
    $("#editor").showModal();
  }
  function safeLink(raw) {
    try {
      const u = new URL(raw, location.origin);
      return u.protocol === "https:" ||
        (u.origin === location.origin && u.pathname.startsWith("/"))
        ? u.href
        : "";
    } catch {
      return "";
    }
  }
  function details(e) {
    if (e.kind === "slot") {
      if (channel(e.channel_id).provider === "recordings") {
        openRecordingScheduler("", e.channel_id);
        return;
      }
      edit({ channel_id: e.channel_id, mode: "fixed", at: e.at });
      return;
    }
    const box = $("#detail-body");
    box.replaceChildren();
    box.append(
      el("p", e.title),
      el(
        "p",
        channel(e.channel_id).name +
          " · " +
          stateLabel(e) +
          " · " +
          label(e.at, true) +
          (e.end ? " → " + label(e.end) : ""),
      ),
    );
    const d = deliveryFor(e);
    if (isRecording(d)) {
      if (d.error) box.append(el("p", d.error, "pub-recording-error"));
      if (["pending", "syncing", "failed", "needs_review"].includes(d.state)) {
        box.append(
          el(
            "p",
            "Requested change: " +
              (d.cancel_pending ? "Cancel this schedule" : label(d.at, true)) +
              ". " +
              (d.confirmed_at
                ? "Previously confirmed: " + label(d.confirmed_at, true) + "."
                : "No confirmed schedule yet."),
          ),
        );
      }
      if (d.eligible)
        box.append(
          actionButton(d.at ? "Move to slot" : "Choose a slot", () =>
            openMove(d),
          ),
        );
      else if (d.reason) box.append(el("p", d.reason));
      if (d.cancelable) {
        const youtube = d.channel_id === "youtube";
        box.append(
          el(
            "p",
            youtube
              ? "Canceling removes the YouTube release time and leaves the video unlisted."
              : "Canceling removes the scheduled broadcast from X and stops its queued playback. The recording file is kept.",
          ),
        );
        box.append(
          actionButton(
            youtube ? "Cancel scheduled release" : "Cancel broadcast",
            async () => {
              if (
                !window.confirm(
                  youtube
                    ? "Cancel this YouTube release and set the video to unlisted? Its broadcast schedule will stay unchanged."
                    : "Cancel this X broadcast and its queued playback? Its YouTube release will stay unchanged.",
                )
              )
                return;
              await api({
                action: "recording_cancel",
                id: d.id,
                revision: d.revision,
              });
              $("#detail").close();
              await load();
              moveStatus(
                youtube
                  ? "Release cancellation is pending confirmation from YouTube."
                  : "Broadcast cancellation requested. Queued playback is disabled on the next streamer sync; X confirmation is pending.",
              );
            },
          ),
        );
      } else if (d.cancel_reason && !d.cancel_pending && d.at) {
        box.append(el("p", d.cancel_reason));
      }
      if (["failed", "needs_review"].includes(d.state)) {
        let confirm = null;
        if (d.retry_confirmation) {
          const label = el("label", undefined, "pub-recording-check");
          confirm = el("input");
          confirm.type = "checkbox";
          label.append(
            confirm,
            el(
              "span",
              "I checked X Studio: no broadcast was created. If it exists, leave this stopped for reconciliation.",
            ),
          );
          box.append(label);
        }
        box.append(
          actionButton(
            d.cancel_pending ? "Retry cancellation" : "Retry synchronization",
            async () => {
              if (confirm && !confirm.checked)
                throw Error("Check X Studio and confirm before retrying.");
              await api({
                action: "recording_retry",
                id: d.id,
                revision: d.revision,
                confirmed_not_created: Boolean(confirm?.checked),
              });
              $("#detail").close();
              await load();
              moveStatus("Schedule synchronization queued.");
            },
          ),
        );
      }
    } else if (d) {
      box.append(el("p", d.body));
      if (d.reply) box.append(el("p", "Reply: " + d.reply));
      if (d.error) box.append(el("p", d.error));
      if (["draft", "scheduled", "failed"].includes(d.state) && !d.root_id)
        box.append(
          actionButton("Edit post", () => {
            $("#detail").close();
            edit(d);
          }),
        );
      if (canMove(d))
        box.append(actionButton("Move to slot", () => openMove(d)));
      if (["draft", "scheduled", "failed"].includes(d.state))
        box.append(
          actionButton("Cancel post", async () => {
            if (!window.confirm("Cancel this post for this account?")) return;
            await act(d, "cancel");
            $("#detail").close();
          }),
        );
      if (d.state === "needs_review") {
        box.append(
          el(
            "p",
            "Check the X account before choosing an outcome. If the main post is already saved here, retry sends only its follow-up reply.",
          ),
        );
        const input = el("input");
        input.placeholder = "https://x.com/…/status/…";
        input.type = "url";
        input.value = d.url;
        box.append(
          input,
          actionButton("Confirm published", async () => {
            await act(d, "confirm_sent", input.value);
            $("#detail").close();
          }),
          actionButton("I checked: remaining post was not sent", async () => {
            await act(d, "confirm_not_sent");
            $("#detail").close();
          }),
        );
      }
    }
    const url = safeLink(e.url || d?.url);
    if (url) {
      const a = el(
        "a",
        e.provider === "recordings"
          ? "Open recording media"
          : "View published post",
      );
      a.href = url;
      a.target = "_blank";
      a.rel = "noopener";
      box.append(el("p"), a);
    }
    $("#detail").showModal();
  }
  $("#post-form").onsubmit = async (ev) => {
    ev.preventDefault();
    const f = ev.target,
      button = f.querySelector("[type=submit]");
    button.disabled = true;
    try {
      const values = new FormData(f);
      const d = Object.fromEntries(values);
      const channelIDs = values.getAll("channel_ids");
      delete d.channel_ids;
      if (!d.id && !channelIDs.length)
        throw Error("Choose at least one account.");
      d.version = Number(d.version);
      d.at = d.mode === "fixed" ? localUTC(d.at) : null;
      d.media = media;
      await api({
        action: "save",
        delivery: d,
        ...(!d.id ? { channel_ids: channelIDs } : {}),
      });
      $("#editor").close();
      await load();
    } catch (e) {
      $("#editor-error").textContent = e.message;
    } finally {
      button.disabled = false;
    }
  };
  $("#post-form").elements.mode.onchange = (ev) => {
    $("#fixed-time").hidden = ev.target.value !== "fixed";
  };
  $("#media-input").onchange = async (ev) => {
    const files = [...ev.target.files];
    if (media.length + files.length > 4) {
      $("#editor-error").textContent = "Choose at most four images.";
      return;
    }
    const submit = $("#post-form [type=submit]");
    submit.disabled = true;
    try {
      for (const file of files) {
        if (file.size > 5 * 1024 * 1024)
          throw Error("Each image must be at most 5 MB.");
        const body = new FormData();
        body.append("file", file);
        const res = await fetch("/admin/publishing/media", {
          method: "POST",
          headers: { "X-CSRF-Token": root.dataset.csrf },
          body,
        });
        if (!res.ok) throw Error(await res.text());
        media.push(await res.json());
      }
      previews();
    } catch (e) {
      $("#editor-error").textContent = e.message;
    } finally {
      submit.disabled = false;
      ev.target.value = "";
    }
  };
  root.querySelectorAll("[data-view]").forEach(
    (b) =>
      (b.onclick = () => {
        view = b.dataset.view;
        location.hash = view;
        load();
      }),
  );
  ["event", "channel", "kind", "status"].forEach(
    (id) => ($("#" + id).onchange = render),
  );
  $("#timezone").onchange = () => {
    try {
      new Intl.DateTimeFormat("en", {
        timeZone: $("#timezone").value,
      }).format();
      zone = $("#timezone").value;
      localStorage.setItem("publishing-timezone", zone);
      load();
    } catch {
      notice("Use an IANA timezone such as America/Chicago.");
    }
  };
  $("#prev").onclick = () => {
    view === "month"
      ? (anchor.setDate(1), anchor.setMonth(anchor.getMonth() - 1))
      : anchor.setDate(anchor.getDate() - 7);
    load();
  };
  $("#next").onclick = () => {
    view === "month"
      ? (anchor.setDate(1), anchor.setMonth(anchor.getMonth() + 1))
      : anchor.setDate(anchor.getDate() + 7);
    load();
  };
  $("#today").onclick = () => {
    anchor = new Date();
    load();
  };

  function recordingEntry(d) {
    return {
      id: d.id,
      channel_id: d.channel_id,
      conference: d.conference,
      title: d.title,
      kind: d.kind,
      state: d.state,
      at: d.at,
      provider: "recordings",
      url:
        "/" +
        encodeURIComponent(d.conference) +
        "/admin/recordings/" +
        encodeURIComponent(d.recording_id),
    };
  }
  function invalidateRecordingPreview() {
    clearRecordingDrag();
    recordingPreviewRequest++;
    recordingPreview = [];
    recordingOrder = [];
    $("#recording-schedule-preview").replaceChildren();
    $("#apply-recording-schedule").disabled = true;
    $("#preview-recording-schedule").disabled = false;
    const selected = [
      ...$("#recording-options").querySelectorAll("input:checked"),
    ];
    $("#recording-selection-count").textContent = selected.length + " selected";
  }
  function recordingDestinationLabel(d) {
    return el(
      "span",
      d.channel_id === "youtube" ? "YouTube release" : "X Broadcast",
      "pub-recording-destination",
    );
  }
  function renderRecordingOptions(selectedIDs = []) {
    const selected = new Set(selectedIDs);
    const box = $("#recording-options");
    box.replaceChildren();
    const event = $("#schedule-event").value,
      destination = $("#schedule-destination").value;
    const items = (data.recordings || []).filter(
      (d) =>
        (!event || d.conference === event) &&
        (!destination || d.channel_id === destination) &&
        (!recordingFocus || d.recording_id === recordingFocus) &&
        ($("#include-excluded").checked || !d.excluded) &&
        ($("#include-scheduled").checked || !d.at),
    );
    for (const d of items) {
      const row = el("div", undefined, "pub-recording-option");
      const label = el("label", undefined, "pub-recording-check"),
        check = el("input"),
        text = el("span");
      check.type = "checkbox";
      check.value = d.id;
      check.disabled = !d.eligible;
      check.checked = d.eligible && selected.has(d.id);
      text.append(
        recordingDestinationLabel(d),
        el("strong", d.title),
        el(
          "small",
          channel(d.channel_id).name +
            " · " +
            d.conference +
            (d.at ? " · " + labelTime(d.at) : " · Not scheduled"),
        ),
      );
      if (
        d.channel_id === "youtube" &&
        ["private", "unlisted", "public"].includes(d.youtube_privacy)
      )
        text.append(el("small", "YouTube visibility: " + d.youtube_privacy));
      if (d.reason)
        text.append(
          el(
            "small",
            d.reason,
            d.excluded ? "pub-legend" : "pub-recording-error",
          ),
        );
      label.append(check, text);
      row.append(label);
      if (d.excluded || (d.state === "draft" && !d.at && !d.confirmed_at)) {
        const button = el(
          "button",
          d.excluded ? "Restore to scheduling" : "Exclude from scheduling",
        );
        button.type = "button";
        button.setAttribute(
          "aria-label",
          button.textContent +
            ": " +
            d.title +
            " · " +
            channel(d.channel_id).name,
        );
        button.onclick = async () => {
          const selected = [...box.querySelectorAll("input:checked")].map(
            (x) => x.value,
          );
          invalidateRecordingPreview();
          button.disabled = true;
          $("#recording-schedule-error").textContent = "";
          try {
            await api({
              action: "recording_exclusion",
              id: d.id,
              revision: d.revision,
              excluded: !d.excluded,
            });
            await load();
            renderRecordingOptions(selected.filter((id) => id !== d.id));
          } catch (e) {
            $("#recording-schedule-error").textContent = e.message;
            button.disabled = false;
          }
        };
        row.append(button);
      }
      box.append(row);
      check.onchange = invalidateRecordingPreview;
    }
    if (!items.length)
      box.append(
        el(
          "p",
          "No recordings match. Choose another event, include scheduled recordings, or show excluded recordings.",
          "pub-empty",
        ),
      );
    invalidateRecordingPreview();
  }
  function labelTime(at) {
    return label(at, true) + " (" + zone + ")";
  }
  function openRecordingScheduler(recording = "", destination = "") {
    for (const dialog of root.querySelectorAll("dialog[open]")) dialog.close();
    recordingFocus = recording;
    const event = $("#schedule-event");
    event.replaceChildren(new Option("All events", ""));
    (data.events || []).forEach((e) =>
      event.add(new Option(e.tag + " · " + e.name, e.tag)),
    );
    event.value = $("#event").value;
    $("#schedule-destination").value = destination;
    $("#include-scheduled").checked = Boolean(recording);
    $("#include-excluded").checked = Boolean(recording);
    $("#recording-schedule-error").textContent = "";
    renderRecordingOptions();
    $("#recording-scheduler").showModal();
  }
  function clearRecordingDrag() {
    recordingDragID = null;
    $("#recording-schedule-preview")
      .querySelectorAll(".pub-recording-preview-row")
      .forEach((row) =>
        row.classList.remove(
          "is-reordering",
          "is-insert-before",
          "is-insert-after",
        ),
      );
  }
  function recordingReorderTarget(row, event) {
    const rect = row.getBoundingClientRect();
    return event.clientY >= rect.top + rect.height / 2;
  }
  function enableRecordingReorder(row, id) {
    row.draggable = true;
    row.ondragstart = (event) => {
      if (event.target.closest("button, a, input")) {
        event.preventDefault();
        return;
      }
      clearRecordingDrag();
      recordingDragID = id;
      event.dataTransfer.setData("text/plain", id);
      event.dataTransfer.effectAllowed = "move";
      row.classList.add("is-reordering");
      $("#apply-recording-schedule").disabled = true;
    };
    row.ondragover = (event) => {
      if (!recordingDragID || recordingDragID === id) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      $("#recording-schedule-preview")
        .querySelectorAll(".is-insert-before, .is-insert-after")
        .forEach((node) =>
          node.classList.remove("is-insert-before", "is-insert-after"),
        );
      row.classList.add(
        recordingReorderTarget(row, event)
          ? "is-insert-after"
          : "is-insert-before",
      );
    };
    row.ondragleave = (event) => {
      if (!row.contains(event.relatedTarget))
        row.classList.remove("is-insert-before", "is-insert-after");
    };
    row.ondrop = (event) => {
      if (!recordingDragID || recordingDragID === id) return;
      event.preventDefault();
      const moved = recordingDragID;
      const order = recordingOrder.filter((item) => item !== moved);
      const at =
        order.indexOf(id) + (recordingReorderTarget(row, event) ? 1 : 0);
      order.splice(at, 0, moved);
      clearRecordingDrag();
      if (order.some((item, index) => item !== recordingOrder[index]))
        previewRecordingSchedule(order);
    };
    row.ondragend = () => {
      clearRecordingDrag();
      $("#apply-recording-schedule").disabled = !recordingPreview.length;
    };
  }
  async function previewRecordingSchedule(ids) {
    clearRecordingDrag();
    if (!ids.length) {
      invalidateRecordingPreview();
      return;
    }
    const request = ++recordingPreviewRequest;
    const button = $("#preview-recording-schedule");
    button.disabled = true;
    $("#recording-schedule-error").textContent = "";
    $("#apply-recording-schedule").disabled = true;
    recordingPreview = [];
    $("#recording-schedule-preview").replaceChildren(
      el("p", "Updating preview…"),
    );
    try {
      const preview = await api({ action: "recording_preview", ids });
      if (request !== recordingPreviewRequest) return;
      recordingPreview = preview;
      recordingOrder = ids.slice();
      const box = $("#recording-schedule-preview");
      box.replaceChildren(el("h3", "Review publication times"));
      const help = el(
        "p",
        "Drag a talk to reorder it, or use the up/down buttons. Times update after each move.",
        "pub-legend",
      );
      help.id = "recording-reorder-help";
      box.append(help);
      recordingPreview.forEach((m, index) => {
        const d = data.recordings.find((x) => x.id === m.id),
          row = el("div", undefined, "pub-recording-preview-row");
        row.dataset.recordingId = m.id;
        row.setAttribute("aria-describedby", "recording-reorder-help");
        enableRecordingReorder(row, m.id);
        const grip = el("span", "⠿", "pub-recording-grip");
        grip.setAttribute("aria-hidden", "true");
        grip.title = "Drag to reorder";
        const text = el("div");
        text.append(
          recordingDestinationLabel(d),
          el("strong", d.title),
          el("small", channel(d.channel_id).name + " · " + labelTime(m.at)),
        );
        row.append(grip, text);
        for (const [direction, icon] of [
          [-1, "↑"],
          [1, "↓"],
        ]) {
          const button = el("button", icon);
          button.type = "button";
          button.setAttribute(
            "aria-label",
            (direction < 0 ? "Earlier: " : "Later: ") +
              d.title +
              " · " +
              channel(d.channel_id).name,
          );
          button.disabled =
            index + direction < 0 ||
            index + direction >= recordingPreview.length;
          button.onclick = (event) => {
            event.preventDefault();
            event.stopPropagation();
            const order = recordingOrder.slice();
            [order[index], order[index + direction]] = [
              order[index + direction],
              order[index],
            ];
            previewRecordingSchedule(order);
          };
          row.append(button);
        }
        const remove = el("button", "Remove", "pub-recording-remove");
        remove.type = "button";
        remove.setAttribute(
          "aria-label",
          "Remove from this batch: " +
            d.title +
            " · " +
            channel(d.channel_id).name,
        );
        remove.onclick = (event) => {
          event.preventDefault();
          event.stopPropagation();
          for (const check of $("#recording-options").querySelectorAll(
            "input",
          )) {
            if (check.value === d.id) check.checked = false;
          }
          const remaining = recordingOrder.filter((id) => id !== d.id);
          $("#recording-selection-count").textContent =
            remaining.length + " selected";
          previewRecordingSchedule(remaining);
        };
        row.append(remove);
        box.append(row);
      });
      box.append(
        el(
          "p",
          "Remove skips an item in this batch only. Use Exclude from scheduling above to keep it out of future batches. Times remain pending until the destination confirms.",
          "pub-legend",
        ),
      );
      $("#apply-recording-schedule").disabled = !recordingPreview.length;
      box.scrollIntoView({ block: "start" });
    } catch (e) {
      if (request !== recordingPreviewRequest) return;
      recordingPreview = [];
      $("#recording-schedule-preview").replaceChildren();
      $("#recording-schedule-error").textContent = e.message;
    } finally {
      if (request === recordingPreviewRequest) button.disabled = false;
    }
  }
  $("#refresh-recording-status").onclick = async () => {
    const button = $("#refresh-recording-status");
    button.disabled = true;
    $("#recording-schedule-error").textContent = "";
    try {
      await api({
        action: "recording_refresh",
        event: $("#schedule-event").value,
      });
      await load();
      renderRecordingOptions();
    } catch (e) {
      $("#recording-schedule-error").textContent = e.message;
    } finally {
      button.disabled = false;
    }
  };
  $("#schedule-recordings").onclick = () => openRecordingScheduler();
  $("#close-recording-scheduler").onclick = () =>
    $("#recording-scheduler").close();
  $("#recording-scheduler").addEventListener("close", clearRecordingDrag);
  $("#schedule-event").onchange = () => {
    recordingFocus = "";
    renderRecordingOptions();
  };
  $("#schedule-destination").onchange = () => renderRecordingOptions();
  $("#include-scheduled").onchange = () => renderRecordingOptions();
  $("#include-excluded").onchange = () => renderRecordingOptions();
  $("#select-recordings").onclick = () => {
    $("#recording-options")
      .querySelectorAll("input:not(:disabled)")
      .forEach((x) => (x.checked = true));
    invalidateRecordingPreview();
  };
  $("#clear-recordings").onclick = () => {
    $("#recording-options")
      .querySelectorAll("input")
      .forEach((x) => (x.checked = false));
    invalidateRecordingPreview();
  };
  $("#preview-recording-schedule").onclick = () =>
    previewRecordingSchedule(
      [...$("#recording-options").querySelectorAll("input:checked")].map(
        (x) => x.value,
      ),
    );
  $("#recording-schedule-form").onsubmit = async (ev) => {
    ev.preventDefault();
    if (!recordingPreview.length) return;
    const button = $("#apply-recording-schedule");
    button.disabled = true;
    try {
      await api({ action: "recording_apply", moves: recordingPreview });
      $("#recording-scheduler").close();
      await load();
      moveStatus(
        "Recording slots reserved. YouTube releases and X broadcasts will synchronize independently.",
      );
    } catch (e) {
      $("#recording-schedule-error").textContent =
        e.message +
        ". Preview the schedule again to refresh the available times.";
      recordingPreview = [];
      button.disabled = true;
    }
  };
  function recordingLinks() {
    const tag = $("#recording-event").value;
    $("#open-recordings").href =
      "/" + encodeURIComponent(tag) + "/admin/recordings";
  }
  $("#recording-event").onchange = recordingLinks;
  $("#recording-workflows").onclick = () => $("#recording-picker").showModal();
  $("#close-recordings").onclick = () => $("#recording-picker").close();
  $("#compose").onclick = () => edit();
  $("#close-editor").onclick = () => $("#editor").close();
  $("#close-day").onclick = () => $("#calendar-day").close();
  $("#close-detail").onclick = () => $("#detail").close();
  $("#refresh-calendar").onclick = async () => {
    const button = $("#refresh-calendar");
    button.disabled = true;
    try {
      await load();
    } finally {
      button.disabled = false;
    }
  };
  setInterval(() => {
    if (
      !document.hidden &&
      view !== "channels" &&
      !root.querySelector("dialog[open]") &&
      !dragged &&
      !pointerDrag &&
      !moveSaving
    )
      load();
  }, 60000);
  load();
})();
