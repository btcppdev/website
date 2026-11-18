(() => {
  "use strict";
  const root = document.getElementById("event-wizard-app");
  if (!root) return;
  const data = JSON.parse(
      document.getElementById("event-wizard-data").textContent,
    ),
    csrf = root.dataset.csrf;
  const steps = [
    [
      "basics",
      "Event basics",
      "Give the next gathering a home.",
      "Choose the identity, location, and dates. Unknown dates can stay blank while you plan.",
    ],
    [
      "artwork",
      "Artwork",
      "Set the scene.",
      "Event artwork and two logo versions, ready for the website and share cards.",
    ],
    [
      "venues",
      "Venues & access",
      "Know the space.",
      "Keep public hours and contracted staff access together. Add setup-only days with staff access and no public doors.",
    ],
    [
      "team",
      "People & committee",
      "Bring your people together.",
      "Invite staff and an optional steering committee. Invitations are tied to each person’s verified email.",
    ],
    [
      "sponsors",
      "Sponsors",
      "Build the partnerships.",
      "Keep prospects, confirmed sponsors, and their commitments in one place.",
    ],
    [
      "tickets",
      "Tickets",
      "Make room for everyone.",
      "Set price tiers, cumulative capacity, and local and student prices. Prices use whole currency units, matching the existing checkout.",
    ],
    [
      "applications",
      "Applications & crew",
      "Open the call.",
      "Set application windows in the event timezone, then preview staffing from your stages and venue hours.",
    ],
    [
      "merch",
      "Checkout merch",
      "Something to take home.",
      "Choose up to three catalog products to offer during ticket checkout. Event register stock is managed separately.",
    ],
    [
      "readiness",
      "Planning & readiness",
      "Turn the plan into an event.",
      "Assign the remaining work, follow the decisions, and review what is ready.",
    ],
  ];
  const step = data.new
      ? "basics"
      : new URLSearchParams(location.search).get("step") || "basics",
    current = steps.find((s) => s[0] === step) || steps[0];
  const plan = data.plan || {},
    readonly = !data.new && !data.can_edit;
  let dirty = false,
    busy = false;
  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function button(text, fn, cls = "ew-secondary") {
    const b = el("button", cls, text);
    b.type = "button";
    b.addEventListener("click", fn);
    b.disabled = readonly;
    return b;
  }
  function link(text, href, cls = "") {
    const a = el("a", cls, text);
    a.href = href;
    return a;
  }
  function section(title, parent = main) {
    const s = el("section", "ew-section");
    if (title) s.append(el("h2", "", title));
    parent.append(s);
    return s;
  }
  function grid(parent) {
    const g = el("div", "ew-grid");
    parent.append(g);
    return g;
  }
  function field(parent, obj, key, label, type = "text", hint = "", options) {
    const wrap = el(
      "label",
      "ew-field" + (type === "textarea" ? " ew-wide" : ""),
    );
    wrap.append(el("span", "", label));
    let input;
    if (options) {
      input = el("select");
      for (const [v, t] of options) {
        const o = el("option", "", t);
        o.value = v;
        input.append(o);
      }
    } else {
      input = el(type === "textarea" ? "textarea" : "input");
      if (type !== "textarea") input.type = type;
    }
    input.name = key;
    input.value = obj[key] ?? "";
    input.disabled = readonly;
    input.addEventListener("input", () => {
      obj[key] =
        type === "number"
          ? input.value === "" && key === "student_price"
            ? null
            : Number(input.value)
          : input.value;
      dirty = true;
    });
    wrap.append(input);
    if (hint) wrap.append(el("small", "", hint));
    parent.append(wrap);
    return input;
  }
  function check(parent, obj, key, label) {
    const wrap = el("label", "ew-check"),
      i = el("input");
    i.type = "checkbox";
    i.checked = !!obj[key];
    i.disabled = readonly;
    i.addEventListener("change", () => {
      obj[key] = i.checked;
      dirty = true;
    });
    wrap.append(i, el("span", "", label));
    parent.append(wrap);
    return i;
  }
  function note(parent, text) {
    parent.append(el("p", "ew-note", text));
  }
  const shell = el("div", "ew-shell"),
    side = el("aside", "ew-sidebar"),
    main = el("main", "ew-content");
  shell.append(side, main);
  root.append(shell);
  side.append(
    el(
      "span",
      "ew-pill",
      data.new
        ? "New draft"
        : data.published
          ? "Published event"
          : "Private draft",
    ),
    el("h2", "", data.basics.name || "Your next event"),
    el(
      "p",
      "",
      data.new
        ? "Start small. Keep building."
        : "/" + data.tag + " · " + data.basics.timezone,
    ),
  );
  const nav = el("nav", "ew-steps");
  nav.ariaLabel = "Event setup steps";
  steps.forEach((s, i) => {
    const a = link(
      "",
      data.new ? "#" : "?step=" + s[0],
      "ew-step" + (s[0] === current[0] ? " active" : ""),
    );
    a.append(el("b", "", String(i + 1).padStart(2, "0")), el("span", "", s[1]));
    if (data.new && i > 0) {
      a.style.opacity = ".4";
      a.addEventListener("click", (e) => e.preventDefault());
    }
    nav.append(a);
  });
  side.append(nav);
  if (window.matchMedia("(max-width: 650px)").matches) { const active = nav.querySelector(".active"); if (active) nav.scrollLeft = active.offsetLeft - nav.offsetLeft; }
  if (!data.new) {
    const links = el("div", "ew-side-links");
    links.append(
      link("Event dashboard ↗", "/" + data.tag + "/admin"),
      link("View event ↗", "/" + data.tag),
      link("All events", "/admin/events"),
    );
    side.append(links);
  }
  main.append(
    el(
      "p",
      "ew-kicker",
      "STEP " + (steps.indexOf(current) + 1) + " / " + steps.length,
    ),
    el("h1", "", data.new ? "Start your next event." : current[2]),
    el("p", "ew-description", current[3]),
  );
  if (readonly)
    main.append(
      el(
        "p",
        "ew-readonly",
        "You can view this plan. Event administrators manage its settings.",
      ),
    );
  const message = el("div", "ew-message");
  message.setAttribute("role", "status");
  message.setAttribute("aria-live", "polite");
  function show(text, error = false) {
    message.textContent = text;
    message.className = "ew-message " + (error ? "ew-error" : "ew-success");
  }
  async function request(section, payload = {}) {
    const response = await fetch(location.pathname, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: JSON.stringify({ section, revision: data.revision, data: payload }),
    });
    if (!response.headers.get("content-type")?.includes("application/json"))
      throw Error("Please sign in again, then reload this page.");
    const result = await response.json();
    if (!response.ok) throw Error(result.error || "Could not save this step.");
    return result;
  }
  function arrayEditor(title, arr, render, make, parent = main) {
    const host = el("div");
    parent.append(host);
    function draw() {
      host.replaceChildren();
      arr.forEach((item, i) => {
        const s = section("", host),
          head = el("div", "ew-section-head");
        head.append(el("h2", "", title + " " + (i + 1)));
        if (!(title === "Ticket tier" && item.id))
          head.append(
            button(
              "Remove",
              () => {
                arr.splice(i, 1);
                dirty = true;
                draw();
              },
              "ew-danger",
            ),
          );
        s.append(head);
        render(s, item, i);
      });
      host.append(
        button("＋ Add " + title.toLowerCase(), () => {
          arr.push(make());
          dirty = true;
          draw();
        }),
      );
    }
    draw();
    return host;
  }
  let payload = () => ({});
  if (current[0] === "basics") {
    const b = data.basics,
      s = section("The essentials"),
      g = grid(s);
    field(
      g,
      b,
      "name",
      "Event name",
      "text",
      "Example: bitcoin++ Tokyo · scripts edition",
    );
    const slug = field(
      g,
      b,
      "tag",
      "Event URL",
      "text",
      "Lowercase letters, numbers and hyphens. Set once when creating the event.",
    );
    slug.disabled = !data.new;
    field(g, b, "edition", "Event type", "text", "", [
      ["global", "Global edition"],
      ["local", "Local gathering"],
    ]);
    field(g, b, "location", "City / region");
    field(
      g,
      b,
      "country",
      "Country code",
      "text",
      "BR, DE, JP, US, or another two-letter country code",
    );
    field(
      g,
      b,
      "timezone",
      "Timezone",
      "text",
      "Examples: America/Sao_Paulo, Europe/Berlin, Asia/Tokyo, America/Chicago",
    );
    const dates = grid(section("Dates & description"));
    field(dates, b, "start", "Event starts", "datetime-local");
    field(dates, b, "end", "Event ends", "datetime-local");
    field(dates, b, "description", "About the event", "textarea");
    payload = () => b;
  }
  if (current[0] === "artwork") {
    const art = data.artwork || {},
      g = el("div", "ew-asset-grid");
    main.append(g);
    for (const [key, label, dark] of [
      ["leading", "Leading image", false],
      ["black", "Black logo", false],
      ["white", "White logo", true],
    ]) {
      const s = el("section", "ew-section ew-asset"),
        preview = el("div", "ew-art-preview" + (dark ? " dark" : ""));
      s.append(el("h2", "", label));
      if (art[key]) {
        const img = el("img");
        img.src = art[key];
        img.alt = label;
        preview.append(img);
      } else
        preview.append(
          el(
            "span",
            "",
            key === "leading" ? "Your event’s leading.png" : "Transparent logo",
          ),
        );
      s.append(preview);
      const f = el("input");
      f.type = "file";
      f.accept = "image/png,image/jpeg";
      f.disabled = readonly;
      f.setAttribute("aria-label", "Upload " + label);
      s.append(f);
      s.append(
        button("Upload " + label.toLowerCase(), async () => {
          if (!f.files[0]) return show("Choose an image first.", true);
          const fd = new FormData();
          fd.append("asset", key);
          fd.append("file", f.files[0]);
          try {
            show("Uploading…");
            const r = await fetch(location.pathname + "/artwork", {
                method: "POST",
                headers: { "X-CSRF-Token": csrf },
                body: fd,
              }),
              result = await r.json();
            if (!r.ok) throw Error(result.error);
            location.reload();
          } catch (e) {
            show(e.message, true);
          }
        }),
      );
      note(
        s,
        key === "leading"
          ? "PNG or JPEG, up to 10 MB. Use landscape artwork."
          : "Transparent PNG recommended. Up to 10 MB.",
      );
      g.append(s);
    }
  }
  if (current[0] === "venues") {
    const venues = plan.venues || [];
    arrayEditor(
      "Venue",
      venues,
      (s, v) => {
        const g = grid(s);
        field(g, v, "name", "Venue name");
        field(g, v, "address", "Street address");
        field(g, v, "city", "City");
        field(g, v, "region", "State / region");
        field(g, v, "postal_code", "Postal code");
        field(g, v, "country", "Country code");
        field(g, v, "website", "Website", "url");
        field(g, v, "contact", "Venue contact");
        field(g, v, "capacity", "Capacity", "number");
        field(
          g,
          v,
          "stages",
          "Rooms / stages",
          "text",
          "Separate names with commas. Example: Main Stage, Workshop Room",
        );
        field(
          g,
          v,
          "contract_url",
          "Contract reference",
          "url",
          "Link to the venue agreement in your document storage.",
        );
        field(
          g,
          v,
          "notes",
          "Access instructions & contract notes",
          "textarea",
        );
        const days = v.days || (v.days = []);
        arrayEditor(
          "Access day",
          days,
          (ds, d) => {
            const dg = grid(ds);
            field(dg, d, "date", "Date", "date");
            field(dg, d, "staff_open", "Staff access from", "time");
            field(dg, d, "staff_close", "Everyone out by", "time");
            field(dg, d, "doors_open", "Public doors open", "time");
            field(dg, d, "doors_close", "Public doors close", "time");
          },
          () => ({
            date: data.basics.start?.slice(0, 10) || "",
            staff_open: "",
            staff_close: "",
            doors_open: "",
            doors_close: "",
          }),
          s,
        );
      },
      () => ({
        id: crypto.randomUUID(),
        name: "",
        country: plan.country || "",
        capacity: 0,
        stages: "",
        days: [],
      }),
    );
    payload = () => ({ venues });
    note(
      main,
      "The first venue supplies the event’s primary address. Public hours and stages also populate the existing schedule tools.",
    );
  }
  if (current[0] === "team") {
    const people = data.team || [],
      s = section("The team");
    if (!people.length)
      s.append(
        el(
          "p",
          "ew-empty",
          "No team members yet. Steering committees are optional.",
        ),
      );
    const list = el("ul", "ew-list");
    people.forEach((p) => {
      const li = el("li"),
        text = el("div");
      text.append(
        el("strong", "", p.name || p.email),
        el("small", "", p.role + " · " + p.email),
        el("small", "", p.responsibility),
      );
      li.append(text);
      const status = el("div");
      status.append(
        el("span", "ew-pill", p.accepted_at ? "Joined" : "Invited"),
      );
      status.append(
        button(
          p.accepted_at ? "Remove from team" : "Revoke",
          async () => {
            try {
              await request("revoke-invite", { id: p.id });
              location.reload();
            } catch (e) {
              show(e.message, true);
            }
          },
          "ew-danger",
        ),
      );
      li.append(status);
      list.append(li);
    });
    s.append(list);
    const invite = {},
      g = grid(section("Invite someone"));
    field(g, invite, "name", "Name");
    field(g, invite, "email", "Email", "email");
    field(
      g,
      invite,
      "role",
      "Role",
      "text",
      "Steering members can view the plan. Staff and coordinators receive event-scoped access.",
      [
        ["steering", "Steering committee — planning view"],
        ["staff", "Event staff"],
        ["volcoord", "Volunteer coordinator"],
        ["admin", "Event administrator"],
      ],
    );
    invite.role = "steering";
    field(
      g,
      invite,
      "responsibility",
      "Responsibility",
      "text",
      "Example: venue lead, program chair, sponsor contact",
    );
    main.append(
      button(
        "Create invitation link",
        async () => {
          try {
            const out = await request("team", invite);
            const s = section("Invitation ready"),
              input = el("input");
            input.value = out.invite_url;
            input.readOnly = true;
            input.style.width = "100%";
            input.setAttribute("aria-label", "Invitation link");
            s.append(input);
            note(
              s,
              "Send this link to " +
                invite.email +
                ". It expires in 14 days and can only be accepted by that verified email.",
            );
            show("Invitation created.");
            dirty = false;
          } catch (e) {
            show(e.message, true);
          }
        },
        "ew-primary",
      ),
    );
    note(
      main,
      "Removing a team member revokes access granted by this invitation. Independently assigned roles remain in place.",
    );
  }
  if (current[0] === "sponsors") {
    const sponsors = plan.sponsors || [];
    arrayEditor(
      "Sponsor",
      sponsors,
      (s, item) => {
        const g = grid(s);
        const opts = [
          ["", "Choose an organization…"],
          ...(data.organizations || []).map((o) => [o.id, o.name]),
        ];
        const select = field(
          g,
          item,
          "organization_id",
          "Organization",
          "text",
          "",
          opts,
        );
        select.addEventListener("change", () => {
          const o = data.organizations.find((o) => o.id === select.value);
          if (o) {
            item.name = o.name;
            s.querySelector("[name=name]").value = o.name;
          }
        });
        field(g, item, "name", "Sponsor name");
        field(g, item, "level", "Tier / package");
        field(g, item, "status", "Status", "text", "", [
          ["prospect", "Prospect"],
          ["contacted", "In discussion"],
          ["confirmed", "Confirmed"],
        ]);
        field(g, item, "contact", "Primary contact");
        field(g, item, "commitments", "Commitments & next steps", "textarea");
      },
      () => ({
        name: "",
        level: "",
        status: "prospect",
        contact: "",
        commitments: "",
      }),
    );
    payload = () => ({ sponsors });
    note(
      main,
      "These are planning records. Manage contracts, payment status, and sponsor entitlements in the sponsor tools.",
    );
    main.append(
      link("Manage sponsorships ↗", "/" + data.tag + "/admin/sponsors"),
    );
  }
  if (current[0] === "tickets") {
    const tickets = data.tickets || [];
    arrayEditor(
      "Ticket tier",
      tickets,
      (s, t) => {
        const g = grid(s);
        field(g, t, "tier", "Tier name");
        field(g, t, "currency", "Currency", "text", "", [
          ["USD", "USD"],
          ["EUR", "EUR"],
          ["BRL", "BRL"],
          ["JPY", "JPY"],
        ]);
        field(g, t, "price", "Standard price", "number");
        field(
          g,
          t,
          "capacity",
          "Cumulative ticket limit",
          "number",
          "For 50 early tickets then 200 total, enter 50 and 200.",
        );
        field(
          g,
          t,
          "local_price",
          "Local price",
          "number",
          "0 disables the local option.",
        );
        field(
          g,
          t,
          "student_price",
          "Student price",
          "number",
          "Leave blank to disable student tickets.",
        );
        field(g, t, "sales_end", "Sales deadline", "datetime-local");
        field(
          g,
          t,
          "card_surcharge_bps",
          "Card surcharge (basis points)",
          "number",
          "1000 = 10%. Current checkout uses 10% when this is 0.",
        );
        field(
          g,
          t,
          "tax_code",
          "Stripe product tax code",
          "text",
          "Use the event’s reviewed tax classification; no exemption is assumed.",
        );
      },
      () => ({
        tier: "",
        price: 0,
        local_price: 0,
        student_price: null,
        capacity: 100,
        currency: { DE: "EUR", BR: "BRL", JP: "JPY" }[plan.country] || "USD",
        sales_end: data.basics.start || "",
        tax_code: "",
        card_surcharge_bps: 1000,
      }),
    );
    const eligibility = {
        local_eligibility: plan.local_eligibility || "",
        student_eligibility: plan.student_eligibility || "",
      },
      g = grid(section("Eligibility"));
    field(
      g,
      eligibility,
      "local_eligibility",
      "Who qualifies for local tickets?",
      "textarea",
    );
    field(
      g,
      eligibility,
      "student_eligibility",
      "Who qualifies for student tickets?",
      "textarea",
    );
    payload = () => ({ tickets, ...eligibility });
    note(
      main,
      "Existing tiers are preserved if omitted. Use the event details editor to change a tier you no longer want offered.",
    );
  }
  if (current[0] === "applications") {
    const a = data.applications;
    if (!a.shift_hours) a.shift_hours = 4;
    let g = grid(section("Talk applications"));
    field(g, a, "talks_open", "Applications open", "datetime-local");
    field(
      g,
      a,
      "talks_close",
      "Applications close",
      "datetime-local",
      "If blank, the existing event-relative deadline applies.",
    );
    g = grid(section("Volunteer applications"));
    field(g, a, "volunteers_open", "Applications open", "datetime-local");
    field(g, a, "volunteers_close", "Applications close", "datetime-local");
    field(g, a, "notes", "Volunteer requirements & jobs", "textarea");
    const s = section("Staffing from the event plan");
    field(s, a, "shift_hours", "Target shift length (hours)", "number");
    check(s, a, "merch_desk", "Include a staffed merch desk");
    note(
      s,
      "Save this step before previewing. Suggested shifts stay inside the saved venue access windows. Assigned and manually edited shifts are preserved.",
    );
    const output = el("div");
    s.append(
      button("Preview staffing", async () => {
        try {
          if (dirty)
            throw Error("Save your changes before previewing staffing.");
          const result = await request("staffing-preview");
          output.replaceChildren();
          const wrap = el("div", "ew-table-wrap"),
            table = el("table", "ew-table"),
            head = el("tr");
          ["Action", "Shift", "Local time", "People"].forEach((x) =>
            head.append(el("th", "", x)),
          );
          table.append(head);
          const fmt = (x) =>
            new Date(x).toLocaleString("en-GB", {
              timeZone: data.basics.timezone,
              month: "short",
              day: "numeric",
              hour: "2-digit",
              minute: "2-digit",
            });
          result.changes.forEach((c) => {
            const row = el("tr");
            [
              c.action,
              c.shift.name + (c.reason ? " — " + c.reason : ""),
              fmt(c.shift.start) + " → " + fmt(c.shift.end),
              c.shift.count,
            ].forEach((x) => row.append(el("td", "", String(x))));
            table.append(row);
          });
          wrap.append(table);
          output.append(
            wrap,
            button(
              "Apply staffing plan",
              async () => {
                try {
                  if (dirty)
                    throw Error(
                      "Save your changes and preview staffing again.",
                    );
                  await request("staffing-apply");
                  show(
                    "Staffing applied. Assigned and edited shifts were preserved.",
                  );
                  output.replaceChildren(
                    link(
                      "Open volunteer shifts ↗",
                      "/" + data.tag + "/volcoord/shifts",
                    ),
                  );
                } catch (e) {
                  show(e.message, true);
                }
              },
              "ew-primary",
            ),
          );
        } catch (e) {
          show(e.message, true);
        }
      }),
      output,
    );
    payload = () => a;
  }
  if (current[0] === "merch") {
    const chosen = new Set(data.product_ids || []),
      s = section("Offer at ticket checkout");
    if (!data.products.length)
      s.append(
        el(
          "p",
          "ew-empty",
          "Publish products in the shop catalog to make them available here.",
        ),
      );
    data.products.forEach((p) => {
      const model = { selected: chosen.has(p.id) },
        i = check(s, model, "selected", p.name);
      i.addEventListener("change", () => {
        if (i.checked) chosen.add(p.id);
        else chosen.delete(p.id);
      });
    });
    payload = () => ({ product_ids: [...chosen] });
    main.append(
      link(
        "Manage event register & allocated stock ↗",
        "/" + data.tag + "/admin/merch-pos",
        "ew-secondary",
      ),
    );
    note(
      main,
      "bitcoin++ is the seller. Organization ship-from profiles will be added in the shop phase.",
    );
  }
  if (current[0] === "readiness") {
    const tasks = plan.tasks || [],
      summary = el("div", "ew-summary");
    [
      [
        tasks.filter((t) => t.status === "done").length + "/" + tasks.length,
        "Tasks complete",
      ],
      [data.team?.length || 0, "Team invitations"],
      [plan.venues?.length || 0, "Venues"],
    ].forEach(([n, label]) => {
      const card = el("div", "ew-stat");
      card.append(el("strong", "", String(n)), el("span", "", label));
      summary.append(card);
    });
    main.append(summary);
    const s = section("Before opening the doors"),
      list = el("ul", "ew-list");
    [
      [!!data.basics.start, "Event dates"],
      [!!data.artwork.leading, "Leading artwork"],
      [!!data.artwork.black && !!data.artwork.white, "Black and white logos"],
      [!!plan.venues?.length, "Venue plan"],
      [!!data.tickets?.length, "Ticket pricing"],
      [!!data.applications.talks_close, "Explicit talk deadline"],
    ].forEach(([ok, label]) => {
      const li = el("li");
      li.append(
        el("span", "", label),
        el("span", "ew-pill", ok ? "Set" : "To decide"),
      );
      list.append(li);
    });
    s.append(list);
    arrayEditor(
      "Task",
      tasks,
      (s, t) => {
        const g = grid(s);
        field(g, t, "title", "Task");
        field(g, t, "owner", "Owner");
        field(g, t, "due", "Due date", "date");
        field(g, t, "status", "Status", "text", "", [
          ["todo", "To do"],
          ["doing", "In progress"],
          ["blocked", "Waiting on something"],
          ["done", "Done"],
        ]);
        field(g, t, "notes", "Notes", "textarea");
      },
      () => ({
        id: crypto.randomUUID(),
        title: "",
        owner: "",
        due: "",
        status: "todo",
        notes: "",
      }),
    );
    payload = () => ({ tasks });
    main.append(
      link(
        "Review publication & event settings ↗",
        "/" + data.tag + "/admin/details",
        "ew-secondary",
      ),
    );
    note(
      main,
      "Publication and reminder campaigns remain explicit controls in event settings. Saving a plan keeps the current publication state.",
    );
  }
  const footer = el("div", "ew-footer");
  footer.append(message);
  if (!["artwork", "team"].includes(current[0])) {
    const save = button(
      data.new ? "Create draft event" : "Save " + current[1].toLowerCase(),
      async () => {
        if (busy) return;
        busy = true;
        save.disabled = true;
        show("Saving…");
        try {
          if (data.new) {
            const response = await fetch("/admin/events/new", {
              method: "POST",
              headers: {
                "Content-Type": "application/json",
                "X-CSRF-Token": csrf,
              },
              body: JSON.stringify(payload()),
            });
            const result = await response.json();
            if (!response.ok) throw Error(result.error);
            dirty = false;
            location.assign(result.url);
          } else {
            await request(current[0], payload());
            dirty = false;
            location.reload();
          }
        } catch (e) {
          show(e.message, true);
        } finally {
          busy = false;
          save.disabled = readonly;
        }
      },
      "ew-primary",
    );
    footer.append(save);
  }
  main.append(footer);
  window.addEventListener("beforeunload", (e) => {
    if (dirty) {
      e.preventDefault();
      e.returnValue = "";
    }
  });
})();
