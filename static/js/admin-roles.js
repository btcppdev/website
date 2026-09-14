(function () {
  'use strict';
  const form = document.querySelector('[data-role-form]');
  if (!form) return;
  const search = document.getElementById('role-person-search');
  const status = document.getElementById('role-search-status');
  const hits = form.querySelector('[data-role-hits]');
  const editor = form.querySelector('[data-role-editor]');
  const person = form.querySelector('[data-role-person]');
  const values = form.querySelector('[data-role-values]');
  const current = form.querySelector('[data-role-current]');
  const choice = document.getElementById('role-choice');
  const help = document.getElementById('role-choice-help');
  const save = form.querySelector('[data-role-save]');
  const add = form.querySelector('[data-role-add]');
  const reset = form.querySelector('[data-role-reset]');
  const changes = form.querySelector('[data-role-changes]');
  const options = new Map(Array.from(choice.options).map(o => [o.value, o]));
  let original = new Set(), selected = new Set(), generation = 0, timer, ready = false;
  function text(tag, value) { const node = document.createElement(tag); node.textContent = value; return node; }
  function label(tag) { return options.has(tag) ? options.get(tag).textContent : tag; }
  function render() {
    current.replaceChildren();
    const all = new Set([...original, ...selected]);
    for (const tag of all) {
      const row = document.createElement('div'); row.className = 'admin-role-row';
      const details = document.createElement('div');
      details.append(text('strong', label(tag)), text('code', tag));
      details.append(text('small', !selected.has(tag) ? 'Will be removed when saved' : !original.has(tag) ? 'Will be added when saved' : 'Currently assigned'));
      if (options.has(tag)) details.append(text('small', options.get(tag).dataset.description));
      const action = text('button', selected.has(tag) ? 'Remove' : 'Keep'); action.type = 'button';
      action.setAttribute('aria-label', (selected.has(tag) ? 'Remove ' : 'Keep ') + label(tag));
      action.addEventListener('click', () => { if (selected.has(tag)) selected.delete(tag); else selected.add(tag); render(); });
      row.append(details, action); current.append(row);
    }
    if (!all.size) current.append(text('p', 'No roles assigned.'));
    form.querySelector('[data-role-count]').textContent = '(' + original.size + ')';
    const added = [...selected].filter(r => !original.has(r));
    const removed = [...original].filter(r => !selected.has(r));
    changes.textContent = added.length || removed.length ? 'Pending: ' + added.length + ' addition(s), ' + removed.length + ' removal(s). Nothing changes until you save.' : 'No pending changes.';
    values.value = [...selected].join(', ');
    save.disabled = !ready || !(added.length || removed.length);
    reset.disabled = save.disabled;
    add.disabled = !ready || !choice.value || selected.has(choice.value) || choice.selectedOptions[0].disabled;
  }
  choice.addEventListener('change', () => { help.textContent = choice.selectedOptions[0].dataset.description || 'Choose the permission you want to grant.'; render(); });
  add.addEventListener('click', () => { if (!add.disabled) {selected.add(choice.value); render();} });
  reset.addEventListener('click', () => {selected = new Set(original); render();});
  async function selectPerson(hit) {
    const token = ++generation;
    ready = false; save.disabled = true; editor.hidden = true; hits.hidden = true;
    person.value = ''; search.value = hit.name || hit.email; status.textContent = 'Loading current roles…';
    try {
      const response = await fetch('/api/speakers/' + encodeURIComponent(hit.id) + '/roles', {credentials:'same-origin', cache:'no-store'});
      if (!response.ok) throw new Error('lookup failed');
      const data = await response.json();
      if (data.roles === null) data.roles = [];
      if (!Array.isArray(data.roles) || data.roles.some(r => typeof r !== 'string')) throw new Error('invalid roles');
      if (token !== generation) return;
      original = new Set(data.roles); selected = new Set(original); ready = true; person.value = hit.id;
      form.querySelector('[data-role-person-name]').textContent = (hit.name || hit.email) + (hit.name && hit.email ? ' · ' + hit.email : '');
      status.textContent = 'Current roles loaded.'; editor.hidden = false; render();
    } catch (_) { if (token === generation) status.textContent = 'Could not load current roles. Select the person again to retry. Saving is disabled.'; }
  }
  search.addEventListener('input', () => {
    const token = ++generation;
    clearTimeout(timer); ready = false; save.disabled = true; editor.hidden = true; hits.hidden = true; person.value = ''; values.value = '';
    const query = search.value.trim();
    if (query.length < 2) {status.textContent = 'Type at least two characters to find a person.'; return;}
    status.textContent = 'Searching…';
    timer = setTimeout(async () => {
      try {
        const response = await fetch('/api/speakers/search?q=' + encodeURIComponent(query), {credentials:'same-origin'});
        if (!response.ok) throw new Error('search failed');
        const people = await response.json();
        if (token !== generation) return;
        hits.replaceChildren();
        for (const hit of people) {
          const li = document.createElement('li'); const button = text('button', (hit.name || hit.email) + (hit.name && hit.email ? ' · ' + hit.email : ''));
          button.type = 'button'; button.addEventListener('click', () => selectPerson(hit)); li.append(button); hits.append(li);
        }
        hits.hidden = people.length === 0; status.textContent = people.length ? 'Choose a person below.' : 'No matching people.';
      } catch (_) {if (token === generation) status.textContent = 'Search unavailable. Please try again.';}
    }, 200);
  });
  form.addEventListener('submit', event => {
    if (!ready || !person.value || save.disabled) {event.preventDefault();return;}
    if (original.size && !selected.size && !window.confirm('Remove every role from this person?')) {event.preventDefault();return;}
    save.disabled = true; save.textContent = 'Saving…';
  });
})();
