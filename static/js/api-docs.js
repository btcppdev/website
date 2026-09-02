(function () {
  "use strict";

  const base = "https://btcpp.dev/api/v1";
  const examples = {
    conferences: ["GET", "/conferences", false, null, "listConferences"],
    conference: ["GET", "/conferences/dev26", false, null, "getConference"],
    agenda: ["GET", "/conferences/dev26/agenda", false, null, "getConferenceAgenda"],
    speakers: ["GET", "/conferences/dev26/speakers", false, null, "listConferenceSpeakers"],
    people: ["GET", "/people?limit=20", false, null, "listPeople"],
    person: ["GET", "/people/PERSON_ID", false, null, "getPerson"],
    recordings: ["GET", "/recordings?limit=20", false, null, "listRecordings"],
    candidates: ["GET", "/conferences/dev26/recording-candidates", true, null, "listRecordingCandidates"],
    "broadcast-plans": ["GET", "/recording-broadcast-plans?updated_after=2026-08-20T18%3A00%3A00Z", true, null, "listRecordingBroadcastPlans"],
    "recording-update": ["PUT", "/conferences/dev26/talks/TALK_ID/recording", true, '{"youtube_url":"https://youtube.com/watch?v=…"}', "putConferenceTalkRecording"],
    "broadcast-update": ["PUT", "/recordings/RECORDING_ID/broadcast", true, '{"state":"live","hls_url":"https://stream.btcpp.dev/live/stream-1/index.m3u8"}', "updateRecordingBroadcast"],
    "inventory-variants": ["GET", "/shop/inventory/variants?limit=100", true, null, "listAccountingInventoryVariants"],
    "inventory-sales": ["GET", "/shop/inventory/sales?limit=100", true, null, "listAccountingInventorySales"],
    projects: ["GET", "/hackathons/HACKATHON_ID/projects", false, null, "listHackathonProjects"],
    project: ["GET", "/hackathons/HACKATHON_ID/projects/PROJECT_ID", false, null, "getHackathonProject"],
    results: ["GET", "/hackathons/HACKATHON_ID/results", false, null, "listHackathonResults"],
    identity: ["GET", "/me/identity", true, null, "getMyIdentity"],
    me: ["GET", "/me", true, null, "getMe"],
    "me-update": ["PATCH", "/me", true, '{"biography":"Building useful Bitcoin software."}', "updateMe"],
    "my-talks": ["GET", "/me/talks", true, null, "listMyTalks"],
    schedule: ["PUT", "/conferences/dev26/talks/TALK_ID/schedule", true, '{"venue":"Main Stage","starts_at":"2026-08-20T10:00:00-05:00","ends_at":"2026-08-20T10:30:00-05:00"}', "updateConferenceTalkSchedule"]
  };

  let language = "curl";
  let activeExample = "conferences";
  let responseExamples = null;
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
    const item = examples[key];
    const operationID = item && item[4];
    const response = operationID && responseExamples && responseExamples[operationID];
    if (response) return JSON.stringify(response.value, null, 2);
    if (responseExamples === null) return "Loading response example…";
    return "Response example unavailable. Open /api/v1/openapi.json for the API contract.";
  }

  function resolveContractRef(contract, reference) {
    if (!reference || reference.indexOf("#/") !== 0) return null;
    return reference.slice(2).split("/").reduce(function (value, segment) {
      return value && value[segment.replace(/~1/g, "/").replace(/~0/g, "~")];
    }, contract);
  }

  function examplesByOperation(contract) {
    const resolved = {};
    Object.keys(contract.paths || {}).forEach(function (path) {
      const pathItem = contract.paths[path];
      ["get", "post", "put", "patch", "delete"].forEach(function (method) {
        const operation = pathItem[method];
        if (!operation || !operation.operationId) return;
        const media = operation.responses && operation.responses["200"] &&
          operation.responses["200"].content && operation.responses["200"].content["application/json"];
        const namedExamples = media && media.examples;
        const firstExample = namedExamples && namedExamples[Object.keys(namedExamples)[0]];
        const example = firstExample && firstExample.$ref ? resolveContractRef(contract, firstExample.$ref) : firstExample;
        if (example && Object.prototype.hasOwnProperty.call(example, "value")) resolved[operation.operationId] = example;
      });
    });
    return resolved;
  }

  fetch("/api/v1/openapi.json", { cache: "no-store", headers: { "Accept": "application/json" } })
    .then(function (response) {
      if (!response.ok) throw new Error("OpenAPI contract unavailable");
      return response.json();
    })
    .then(function (contract) {
      responseExamples = examplesByOperation(contract);
      renderExample(activeExample);
    })
    .catch(function () {
      const responseNode = examplePanel && examplePanel.querySelector("[data-api-example-response]");
      if (responseNode) responseNode.textContent = "Response example unavailable. Open /api/v1/openapi.json for the API contract.";
    });

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
