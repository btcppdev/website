(function () {
  'use strict';
  const form = document.querySelector('[data-role-form]');
  if (!form) return;
  const search = document.getElementById('role-person-search');
  const status = document.getElementById('role-search-status');
  const hits = form.querySelector('[data-role-hits]');
  const editor = form.querySelector('[data-role-editor]');
  const fields = form.querySelector('[data-role-fields]');
  const person = form.querySelector('[data-role-person]');
  const values = form.querySelector('[data-role-values]');
  const current = form.querySelector('[data-role-current]');
  const filter = document.getElementById('role-filter');
  const catalog = document.getElementById('role-catalog');
  const resultStatus = document.getElementById('role-results-status');
  const filters = Array.from(form.querySelectorAll('[data-role-filter]'));
  const checks = Array.from(form.querySelectorAll('[data-role-check]'));
  const options = new Map(checks.map(input => [input.value, input]));
  const save = form.querySelector('[data-role-save]');
  const reset = form.querySelector('[data-role-reset]');
  const changes = form.querySelector('[data-role-changes]');
  let original = new Set(), selected = new Set(), generation = 0, timer, ready = false, saving = false;
  let category = 'all', searchController, personController;
  function text(tag, value) { const node = document.createElement(tag); node.textContent = value; return node; }
  function label(tag) { return options.has(tag) ? options.get(tag).dataset.label : tag; }
  function dirty() { return original.size !== selected.size || [...original].some(tag => !selected.has(tag)); }
  function renderCatalog() {
    const words = filter.value.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
    let visible = 0;
    for (const input of checks) {
      const row = input.closest('[data-role-option]');
      const haystack = row.textContent.toLocaleLowerCase();
      row.hidden = (category !== 'all' && row.dataset.roleGroup !== category) || !words.every(word => haystack.includes(word));
      if (!row.hidden) visible++;
      input.checked = selected.has(input.value);
      input.disabled = !ready || (input.dataset.restricted === 'true' && !original.has(input.value));
    }
    for (const button of filters) button.setAttribute('aria-pressed', String(button.dataset.roleFilter === category));
    resultStatus.textContent = visible + (visible === 1 ? ' permission' : ' permissions') + ' · Check to add, uncheck to remove.';
    form.querySelector('[data-role-empty]').hidden = visible !== 0;
    catalog.hidden = visible === 0;
  }
  function render() {
    current.replaceChildren();
    const all = new Set([...original, ...selected]);
    for (const tag of all) {
      const row = document.createElement('div'); row.className = 'admin-role-row';
      row.dataset.state = !selected.has(tag) ? 'removed' : !original.has(tag) ? 'added' : 'assigned';
      const details = document.createElement('div');
      details.append(text('strong', label(tag)), text('code', tag));
      details.append(text('small', row.dataset.state === 'removed' ? 'Will be removed' : row.dataset.state === 'added' ? 'Will be added' : 'Currently assigned'));
      const action = text('button', selected.has(tag) ? 'Remove' : 'Undo'); action.type = 'button';
      action.dataset.currentRole = tag;
      action.setAttribute('aria-label', (selected.has(tag) ? 'Remove ' : 'Keep ') + label(tag));
      action.addEventListener('click', () => {
        if (selected.has(tag)) selected.delete(tag); else selected.add(tag);
        render();
        const replacement = Array.from(current.querySelectorAll('button')).find(button => button.dataset.currentRole === tag);
        (replacement || filter).focus();
      });
      row.append(details, action); current.append(row);
    }
    if (!all.size) current.append(text('p', 'No roles selected. Choose permissions from the list.'));
    form.querySelector('[data-role-count]').textContent = selected.size;
    const added = [...selected].filter(r => !original.has(r));
    const removed = [...original].filter(r => !selected.has(r));
    changes.textContent = dirty() ? added.length + ' to add · ' + removed.length + ' to remove. Save to apply.' : 'No pending changes.';
    values.value = [...selected].join(', ');
    save.disabled = !ready || !dirty();
    reset.disabled = save.disabled;
    renderCatalog();
  }
  for (const input of checks) input.addEventListener('change', () => {
    if (!ready || input.disabled) return;
    if (input.checked) selected.add(input.value); else selected.delete(input.value);
    render();
  });
  filter.addEventListener('input', () => { renderCatalog(); catalog.scrollTop = 0; });
  for (const button of filters) button.addEventListener('click', () => {
    category = button.dataset.roleFilter; renderCatalog(); catalog.scrollTop = 0;
  });
  reset.addEventListener('click', () => { selected = new Set(original); render(); filter.focus(); });
  async function selectPerson(hit) {
    if (person.value === hit.id && ready) { hits.hidden = true; filter.focus(); return; }
    if (dirty() && ready && !window.confirm('Discard unsaved role changes and switch people?')) return;
    const token = ++generation;
    clearTimeout(timer);
    if (searchController) searchController.abort();
    if (personController) personController.abort();
    personController = new AbortController();
    ready = false; fields.disabled = true; save.disabled = true; editor.hidden = true; hits.hidden = true;
    person.value = ''; values.value = ''; original = new Set(); selected = new Set();
    search.value = hit.name || hit.email; status.textContent = 'Loading current roles…';
    try {
      const response = await fetch('/api/speakers/' + encodeURIComponent(hit.id) + '/roles', {credentials:'same-origin', cache:'no-store', signal:personController.signal});
      if (!response.ok) throw new Error('lookup failed');
      const data = await response.json();
      if (data.roles === null) data.roles = [];
      if (!Array.isArray(data.roles) || data.roles.some(r => typeof r !== 'string')) throw new Error('invalid roles');
      if (token !== generation) return;
      original = new Set(data.roles); selected = new Set(original); ready = true; person.value = hit.id;
      form.querySelector('[data-role-person-name]').textContent = hit.name || hit.email;
      form.querySelector('[data-role-person-email]').textContent = hit.name && hit.email ? hit.email : '';
      filter.value = ''; category = 'all'; catalog.scrollTop = 0;
      status.textContent = 'Current roles loaded.'; editor.hidden = false; fields.disabled = false; render(); filter.focus();
    } catch (error) { if (token === generation && error.name !== 'AbortError') status.textContent = 'Could not load current roles. Search again to retry.'; }
  }
  search.addEventListener('input', () => {
    const token = ++generation;
    clearTimeout(timer);
    if (searchController) searchController.abort();
    if (personController) personController.abort();
    hits.hidden = true;
    const query = search.value.trim();
    if (query.length < 2) { status.textContent = 'Type at least two characters to find a person.'; return; }
    status.textContent = 'Searching…';
    timer = setTimeout(async () => {
      searchController = new AbortController();
      try {
        const response = await fetch('/api/speakers/search?q=' + encodeURIComponent(query), {credentials:'same-origin', signal:searchController.signal});
        if (!response.ok) throw new Error('search failed');
        const people = await response.json();
        if (!Array.isArray(people)) throw new Error('invalid results');
        if (token !== generation) return;
        hits.replaceChildren();
        for (const hit of people) {
          const li = document.createElement('li'); const button = text('button', hit.name || hit.email);
          if (hit.name && hit.email) button.append(text('small', hit.email));
          button.type = 'button'; button.addEventListener('click', () => selectPerson(hit)); li.append(button); hits.append(li);
        }
        hits.hidden = people.length === 0; status.textContent = people.length ? 'Choose a person below.' : 'No matching people.';
      } catch (error) { if (token === generation && error.name !== 'AbortError') status.textContent = 'Search unavailable. Please try again.'; }
    }, 200);
  });
  search.addEventListener('keydown', event => {
    if (event.key === 'Enter' || event.key === 'ArrowDown') {
      event.preventDefault();
      if (!hits.hidden && hits.querySelector('button')) hits.querySelector('button').focus();
    }
    if (event.key === 'Escape') hits.hidden = true;
  });
  hits.addEventListener('keydown', event => {
    const buttons = Array.from(hits.querySelectorAll('button'));
    const index = buttons.indexOf(document.activeElement);
    if (event.key === 'Escape') { hits.hidden = true; search.focus(); }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const next = buttons[index + (event.key === 'ArrowDown' ? 1 : -1)];
      if (next) next.focus(); else if (event.key === 'ArrowUp') search.focus();
    }
  });
  filter.addEventListener('keydown', event => {
    if (event.key === 'Enter') event.preventDefault();
    if (event.key === 'Escape') { filter.value = ''; renderCatalog(); }
  });
  window.addEventListener('beforeunload', event => {
    if (ready && dirty() && !saving) { event.preventDefault(); event.returnValue = ''; }
  });
  form.addEventListener('submit', event => {
    if (!ready || !person.value || save.disabled) { event.preventDefault(); return; }
    if (original.size && !selected.size && !window.confirm('Remove every role from this person?')) { event.preventDefault(); return; }
    saving = true; save.disabled = true; save.textContent = 'Saving…';
  });
})();
