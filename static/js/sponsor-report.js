"use strict";
const printButton = document.getElementById("report-print");
if (printButton) printButton.addEventListener("click", () => window.print());
const editButton = document.getElementById("report-edit");
if (editButton) editButton.addEventListener("click", () => {
  const editor = document.querySelector(".report-editor");
  if (!editor) return;
  editor.open = true;
  editor.scrollIntoView({behavior: "smooth", block: "start"});
});
const recipientInput = document.getElementById("report-recipient");
if (recipientInput) recipientInput.addEventListener("input", () => {
  const option = recipientInput.selectedOptions[0];
  const name = option.dataset.name || (option.value === "legacy" ? option.textContent.replace(" · previous recipient", "") : "Our partners");
  for (const target of document.querySelectorAll("[data-report-recipient]")) target.textContent = name;
  for (const fallback of document.querySelectorAll("[data-report-recipient-fallback]")) fallback.hidden = !!option.dataset.logo;
  for (const logo of document.querySelectorAll("[data-report-recipient-logo]")) {
    logo.hidden = !option.dataset.logo;
    if (option.dataset.logo) logo.src = option.dataset.logo; else logo.removeAttribute("src");
    logo.alt = name + " logo";
  }
  for (const target of document.querySelectorAll("[data-report-recipient]")) styleReportWordmarks(target);
});
const layoutSelect = document.getElementById("report-layout");
if (layoutSelect) {
  const requested = new URL(window.location.href).searchParams.get("layout");
  layoutSelect.value = requested === "slides" ? "slides" : "paper";
  const applyLayout = () => {
    document.body.dataset.layout = layoutSelect.value;
    const url = new URL(window.location.href);
    url.searchParams.set("layout", layoutSelect.value);
    window.history.replaceState(null, "", url);
  };
  applyLayout();
  layoutSelect.addEventListener("change", applyLayout);
}

// Style the brand in rendered copy, including editable narrative values.
// Work with text nodes so links, form values, and escaped user content stay intact.
function styleReportWordmarks(reportCopy) {
  if (!reportCopy) return;
  const walker = document.createTreeWalker(reportCopy, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      return node.nodeValue.includes("bitcoin++") &&
        !node.parentElement.closest(".report-wordmark, script, style, textarea, input, [contenteditable]")
        ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
    }
  });
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  for (const node of nodes) {
    const fragment = document.createDocumentFragment();
    node.nodeValue.split("bitcoin++").forEach((text, index) => {
      if (index) {
        const wordmark = document.createElement("span");
        wordmark.className = "report-wordmark";
        wordmark.append("bitcoin");
        const pluses = document.createElement("span");
        pluses.className = "wordmark-pluses";
        pluses.append("+");
        const second = document.createElement("span");
        second.className = "wordmark-plus-second";
        second.textContent = "+";
        pluses.append(second);
        wordmark.append(pluses);
        fragment.append(wordmark);
      }
      fragment.append(document.createTextNode(text));
    });
    node.replaceWith(fragment);
  }
}

styleReportWordmarks(document.querySelector("main"));
styleReportWordmarks(document.querySelector(".report-editor"));

// Present account snapshots as platform cards while preserving all source copy.
const socialCopy = document.querySelector('[data-section="social"] .story-text');
if (socialCopy) {
  const paragraphs = socialCopy.textContent.split(/\n\s*\n/);
  const content = document.createDocumentFragment();
  const cards = document.createElement("div");
  cards.className = "social-platforms";
  for (const paragraph of paragraphs) {
    const platform = paragraph.match(/^(YouTube|X):\s*(.+)$/s);
    if (!platform) {
      const note = document.createElement("p");
      note.className = cards.childElementCount ? "social-source" : "social-intro";
      note.textContent = paragraph;
      if (cards.childElementCount && !content.contains(cards)) content.append(cards);
      content.append(note);
      continue;
    }
    const card = document.createElement("article");
    card.className = "social-platform social-" + platform[1].toLowerCase();
    const heading = document.createElement("h3");
    heading.textContent = platform[1];
    card.append(heading);
    const metrics = document.createElement("dl");
    for (const metric of platform[2].split("·")) {
      const parts = metric.trim().replace(/\.$/, "").match(/^([\d,.]+(?:[kKM])?)\s+(.+)$/);
      const row = document.createElement("div");
      if (parts) {
        const label = document.createElement("dt");
        label.textContent = parts[2];
        const value = document.createElement("dd");
        value.textContent = parts[1];
        row.append(label, value);
      } else {
        row.textContent = metric.trim();
      }
      metrics.append(row);
    }
    card.append(metrics);
    cards.append(card);
  }
  if (cards.childElementCount) {
    if (!content.contains(cards)) content.append(cards);
    socialCopy.replaceChildren(content);
  }
}

const reportForm = document.getElementById("report-edit-form");
if (reportForm) {
  let dirty = false;
  let submitting = false;
  const saveStatus = document.getElementById("report-save-status");
  const pending = new Set();
  const markDirty = (event) => {
    if (event?.target?.id === "report-post-search" || event?.target?.id === "report-post-filter") return;
    dirty = true;
    reportForm.dataset.dirty = "true";
    if (!event?.target?.matches("[data-report-section]")) reportForm.dataset.figuresEdited = "true";
    saveStatus.textContent = (document.querySelector(".synthetic-banner") || document.body.dataset.readonly === "true")
      ? "Preview updated — changes are not saved."
      : `Unsaved changes — click ${reportForm.dataset.saveLabel || "Save event draft"} to keep them.`;
  };
  reportForm.addEventListener("input", markDirty);
  reportForm.addEventListener("report-saved", () => { dirty = false; saveStatus.textContent = "Draft saved."; });
  reportForm.addEventListener("submit", () => { submitting = true; });
  window.addEventListener("beforeunload", event => {
    if (dirty && !submitting) { event.preventDefault(); event.returnValue = ""; }
  });
  for (const input of reportForm.querySelectorAll("[data-report-section]")) {
    const key = input.dataset.reportSection;
    const preview = document.getElementById("preview-" + key);
    const status = document.getElementById("preview-status-" + key);
    const stories = () => document.querySelectorAll('[data-section="' + key + '"] .story-text');
    let timer, controller, version = 0;
    input.addEventListener("input", () => {
      clearTimeout(timer);
      if (controller) controller.abort();
      const current = ++version;
      pending.add(key);
      status.textContent = "Updating preview…";
      timer = setTimeout(async () => {
        controller = new AbortController();
        try {
          const response = await fetch("/admin/reports/preview", {
            method: "POST", credentials: "same-origin", signal: controller.signal,
            headers: {"Content-Type": "application/x-www-form-urlencoded"},
            body: new URLSearchParams({text: input.value, csrf: reportForm.elements.csrf.value})
          });
          if (!response.ok || response.redirected) throw new Error("Preview unavailable");
          const html = await response.text();
          if (current !== version) return;
          // Only insert HTML from our authenticated, allowlisted Markdown renderer.
          preview.innerHTML = html;
          for (const story of stories()) { story.innerHTML = html; story.classList.add("report-markdown"); styleReportWordmarks(story); }
          styleReportWordmarks(preview);

          status.textContent = "Preview up to date · not saved";
          pending.delete(key);
        } catch (error) {
          if (current !== version) return;
          status.textContent = "Preview unavailable. Your text is still here; edit again to retry or save the draft.";
        }
      }, 250);
    });
  }
  for (const button of reportForm.querySelectorAll("[data-markdown]")) {
    button.addEventListener("click", () => {
      const input = document.getElementById(button.dataset.target);
      const start = input.selectionStart, end = input.selectionEnd;
      const selected = input.value.slice(start, end) || "Your text";
      const replacements = {bold: "**"+selected+"**", italic: "*"+selected+"*", heading: "\n\n## "+selected+"\n", list: "\n- "+selected.replace(/\n/g,"\n- "), link: "["+selected+"](https://example.com)"};
      input.setRangeText(replacements[button.dataset.markdown], start, end, "select");
      input.focus();
      input.dispatchEvent(new Event("input", {bubbles:true}));
    });
  }
  printButton?.addEventListener("click", event => {
    if (pending.size) {
      event.stopImmediatePropagation();
      event.preventDefault();
      saveStatus.textContent = "Wait for the narrative previews to update before exporting.";
      saveStatus.scrollIntoView({block:"center"});
    }
  }, true);
}

// Attribution is saved server-side; filters never alter or drop form controls.
const postSearch = document.getElementById("report-post-search");
const postFilter = document.getElementById("report-post-filter");
function filterReportPosts() {
  const query = (postSearch?.value || "").toLowerCase();
  const filter = postFilter?.value || "all";
  document.querySelectorAll(".report-post-review").forEach(row => {
    const choice = row.querySelector("select").value;
    const matches = filter === "all" || (filter === "suggested" && row.dataset.suggested === "true") || (filter === "included" && choice === "include") || (filter === "pending" && choice === "");
    row.hidden = !matches || !row.textContent.toLowerCase().includes(query);
  });
}
postSearch?.addEventListener("input", filterReportPosts);
postFilter?.addEventListener("change", filterReportPosts);
document.querySelector(".report-post-review-list")?.addEventListener("change", filterReportPosts);

// Pull source data on opening a live annual draft. Collection never saves this
// form: narrative edits and exclusion choices remain in the user's control.
const annualRefresh = document.getElementById("annual-refresh-all");
async function collectAnnualSources(automatic) {
  if (!reportForm || annualRefresh.disabled) return;
  const status = document.getElementById("annual-refresh-status");
  const summary = document.createElement("p");
  const details = document.createElement("details");
  const heading = document.createElement("summary");
  heading.textContent = "Collection details";
  details.append(heading);
  summary.textContent = "Collecting event analytics… You can keep editing.";
  status.replaceChildren(summary, details);
  const jobs = Array.from(reportForm.querySelectorAll('input[name="event"]:checked:not(:disabled)'))
    .map(input => ({action:"refresh_annual_event", id:input.value, name:input.closest("label").textContent.trim()}));
  if (jobs.length) jobs.push({action:"refresh_annual_posts", id:"", name:"X post analytics"});
  const freeze = reportForm.querySelector('[value="freeze"]');
  const blocked = [annualRefresh, freeze, printButton].filter(button => button && !button.disabled);
  blocked.forEach(button => button.disabled = true);
  let completed = 0, attention = false;
  try {
    async function collect(job) {
      const row = document.createElement("p");
      row.textContent = job.name + ": checking…";
      details.append(row);
      const body = new URLSearchParams();
      body.set("csrf", reportForm.elements.csrf.value);
      body.set("action", job.action);
      body.set("event_id", job.id);
      body.set("automatic", automatic ? "yes" : "no");
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 120000);
      try {
        const response = await fetch(location.href, {method:"POST", body, credentials:"same-origin", signal:controller.signal});
        if (response.redirected) throw new Error("Session expired; sign in again.");
        const message = await response.text();
        if (!response.ok || message.trim().startsWith("<") || /unavailable|retained/i.test(message)) attention = true;
        row.textContent = job.name + ": " + (message.trim().startsWith("<") ? "Server error; previous data retained." : message);
      } catch (error) {
        attention = true;
        row.textContent = job.name + ": " + (error.name === "AbortError" ? "Timed out; previous data retained." : error.message);
      } finally {
        clearTimeout(timeout);
        summary.textContent = `Checked ${++completed} of ${jobs.length} sources. You can keep editing.`;
      }
    }
    // A small queue keeps independent events moving without flooding providers.
    let next = 0;
    await Promise.all(Array.from({length:Math.min(2, jobs.length)}, async () => {
      while (next < jobs.length) await collect(jobs[next++]);
    }));
    const response = await fetch(location.href, {credentials:"same-origin", cache:"no-store"});
    if (!response.ok || response.redirected) throw new Error("Could not update the report preview; your edits are still here.");
    const refreshed = new DOMParser().parseFromString(await response.text(), "text/html");
    const main = refreshed.querySelector("main");
    const sources = refreshed.getElementById("annual-source-review");
    if (!main || !sources) throw new Error("Could not update the report preview; your edits are still here.");
    // A changed scope or manual metric must be saved before rendering new totals.
    // Narrative-only edits can be preserved while figures update around them.
    if (reportForm.dataset.figuresEdited !== "true") {
      for (const input of reportForm.querySelectorAll("[data-report-section]")) {
        if (input.value === input.defaultValue) continue;
        const preview = document.getElementById("preview-" + input.dataset.reportSection);
        for (const story of main.querySelectorAll('[data-section="' + input.dataset.reportSection + '"] .story-text')) {
          story.innerHTML = preview.innerHTML;
        }
      }
      document.querySelector("main").replaceWith(main);
      sources.open = document.getElementById("annual-source-review").open;
      document.getElementById("annual-source-review").replaceWith(sources);
      styleReportWordmarks(main);
    }
    summary.textContent = (attention ? "Collection finished with some sources unavailable; previous figures were retained. See details below." : "Available source data is up to date.") +
      (reportForm.dataset.figuresEdited === "true" ? " Save your edits to apply the latest figures and your exclusions." : " Report figures have updated automatically.");
    details.open = attention;
  } catch (error) {
    summary.textContent = error.message;
    details.open = true;
  } finally {
    blocked.forEach(button => button.disabled = false);
  }
}
annualRefresh?.addEventListener("click", () => collectAnnualSources(false));
if (annualRefresh && document.body.dataset.readonly !== "true") collectAnnualSources(true);
