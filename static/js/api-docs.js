(function () {
  "use strict";

  const base = "https://btcpp.dev/api/v1";
  const examples = {
    conferences: ["GET", "/conferences", false],
    conference: ["GET", "/conferences/dev26", false],
    agenda: ["GET", "/conferences/dev26/agenda", false],
    speakers: ["GET", "/conferences/dev26/speakers", false],
    people: ["GET", "/people?limit=20", false],
    person: ["GET", "/people/PERSON_ID", false],
    recordings: ["GET", "/recordings?limit=20", false],
    candidates: ["GET", "/conferences/dev26/recording-candidates", true],
    "recording-update": ["PUT", "/conferences/dev26/talks/TALK_ID/recording", true, '{"youtube_url":"https://youtube.com/watch?v=…"}'],
    projects: ["GET", "/hackathons/HACKATHON_ID/projects", false],
    project: ["GET", "/hackathons/HACKATHON_ID/projects/PROJECT_ID", false],
    results: ["GET", "/hackathons/HACKATHON_ID/results", false],
    identity: ["GET", "/me/identity", true],
    me: ["GET", "/me", true],
    "me-update": ["PATCH", "/me", true, '{"biography":"Building useful Bitcoin software."}'],
    "my-talks": ["GET", "/me/talks", true],
    schedule: ["PUT", "/conferences/dev26/talks/TALK_ID/schedule", true, '{"venue":"Main Stage","starts_at":"2026-08-20T10:00:00-05:00","ends_at":"2026-08-20T10:30:00-05:00"}']
  };

  let language = "curl";
  let activeExample = "conferences";
  const examplePanel = document.querySelector("[data-api-example]");

  function exampleCode(key, selectedLanguage) {
    const item = examples[key] || examples.conferences;
    const method = item[0];
    const path = item[1];
    const authenticated = item[2];
    const body = item[3];
    if (selectedLanguage === "javascript") {
      const headers = ['"Accept": "application/json"'];
      if (authenticated) headers.push('"Authorization": "Bearer " + token');
      if (body) headers.push('"Content-Type": "application/json"');
      return "const response = await fetch(\n  \"" + base + path + "\",\n  {\n    method: \"" + method + "\",\n    headers: {\n      " + headers.join(",\n      ") + "\n    }" + (body ? ",\n    body: JSON.stringify(" + body + ")" : "") + "\n  }\n);\n\nconst result = await response.json();";
    }
    const lines = ["curl -X " + method];
    lines.push("  -H 'Accept: application/json'");
    if (authenticated) lines.push("  -H 'Authorization: Bearer YOUR_TOKEN'");
    if (body) {
      lines.push("  -H 'Content-Type: application/json'");
      lines.push("  --data '" + body + "'");
    }
    lines.push("  '" + base + path + "'");
    return lines.join(" \\\n");
  }

  function renderExample(key) {
    if (!examplePanel || !examples[key]) return;
    activeExample = key;
    const item = examples[key];
    const methodLabel = examplePanel.querySelector("[data-api-example-method]");
    methodLabel.textContent = item[0];
    methodLabel.className = item[0] === "GET" ? "" : "is-write";
    examplePanel.querySelector("[data-api-example-path]").textContent = item[1];
    examplePanel.querySelector("[data-api-example-code]").textContent = exampleCode(key, language);
    examplePanel.querySelector("[data-api-example-response]").textContent = exampleResponse(key);
    document.querySelectorAll("[data-api-endpoint]").forEach(function (button) {
      button.classList.toggle("is-active", button.dataset.apiEndpoint === key);
    });
  }

  function exampleResponse(key) {
    if (["conferences", "agenda", "speakers", "people", "recordings", "candidates", "projects", "results", "my-talks"].includes(key)) {
      return '{\n  "data": [\n    {\n      "id": "…"\n    }\n  ],\n  "meta": {\n    "request_id": "…"\n  }\n}';
    }
    return '{\n  "data": {\n    "id": "…"\n  },\n  "meta": {\n    "request_id": "…"\n  }\n}';
  }

  document.querySelectorAll("[data-api-endpoint]").forEach(function (button) {
    button.addEventListener("click", function () {
      renderExample(button.dataset.apiEndpoint);
      if (window.matchMedia("(max-width: 1080px)").matches && examplePanel) {
        examplePanel.scrollIntoView({ behavior: "smooth", block: "start" });
      }
    });
  });

  document.querySelectorAll("[data-api-language]").forEach(function (button) {
    button.addEventListener("click", function () {
      language = button.dataset.apiLanguage;
      document.querySelectorAll("[data-api-language]").forEach(function (candidate) {
        candidate.setAttribute("aria-selected", String(candidate === button));
      });
      renderExample(activeExample);
    });
  });

  function copyText(text, button) {
    if (!navigator.clipboard) return;
    navigator.clipboard.writeText(text).then(function () {
      const original = button.textContent;
      button.textContent = "Copied";
      window.setTimeout(function () { button.textContent = original; }, 1400);
    });
  }

  document.querySelectorAll("[data-copy-code]").forEach(function (button) {
    button.addEventListener("click", function () {
      const code = button.closest("[data-copyable]").querySelector("code");
      copyText(code.textContent, button);
    });
  });
  const exampleCopy = document.querySelector("[data-copy-example]");
  if (exampleCopy) exampleCopy.addEventListener("click", function () {
    copyText(examplePanel.querySelector("[data-api-example-code]").textContent, exampleCopy);
  });

  const search = document.querySelector("[data-docs-search]");
  const searchInput = document.querySelector("[data-docs-search-input]");
  const searchResults = document.querySelector("[data-docs-search-results]");
  const searchItems = Array.from(document.querySelectorAll("[data-docs-search-item]"));

  function closeSearch() {
    if (!search) return;
    search.hidden = true;
    document.body.classList.remove("has-api-search");
  }

  function renderSearchResults(query) {
    if (!searchResults) return;
    const normalized = query.trim().toLowerCase();
    const matches = searchItems.filter(function (item) {
      return !normalized || (item.dataset.searchLabel || item.textContent).toLowerCase().includes(normalized);
    }).slice(0, 9);
    searchResults.replaceChildren();
    matches.forEach(function (item) {
      const link = document.createElement("a");
      link.href = item.getAttribute("href") || ("#" + item.closest("section").id);
      link.textContent = item.dataset.searchLabel || item.textContent.trim();
      link.addEventListener("click", closeSearch);
      searchResults.appendChild(link);
    });
    if (!matches.length) {
      const empty = document.createElement("p");
      empty.textContent = "No matching API documentation.";
      searchResults.appendChild(empty);
    }
  }

  function openSearch() {
    if (!search || !searchInput) return;
    search.hidden = false;
    document.body.classList.add("has-api-search");
    searchInput.value = "";
    renderSearchResults("");
    searchInput.focus();
  }

  document.querySelectorAll("[data-docs-search-open]").forEach(function (button) { button.addEventListener("click", openSearch); });
  document.querySelectorAll("[data-docs-search-close]").forEach(function (button) { button.addEventListener("click", closeSearch); });
  if (searchInput) searchInput.addEventListener("input", function () { renderSearchResults(searchInput.value); });
  document.addEventListener("keydown", function (event) {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      openSearch();
    } else if (event.key === "Escape") {
      closeSearch();
    }
  });

  if ("IntersectionObserver" in window) {
    const sectionObserver = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        document.querySelectorAll('.api-reference__sidebar a[href^="#"]').forEach(function (link) {
          link.classList.toggle("is-current", link.getAttribute("href") === "#" + entry.target.id);
        });
      });
    }, { rootMargin: "-20% 0px -65% 0px" });
    document.querySelectorAll("[data-docs-section]").forEach(function (section) { sectionObserver.observe(section); });
  }

  renderExample(activeExample);
})();
