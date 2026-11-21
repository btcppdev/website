(() => {
  const form = document.querySelector('[data-attendee-form]');
  if (!form) return;
  const rows = Array.from(form.querySelectorAll('[data-attendee-row]'));
  const booklets = new Map(Array.from(form.querySelectorAll('[data-booklet-ticket]')).map(block => [block.dataset.bookletTicket, block]));
  function updateBooklet(row, prefill = false) {
    const block = booklets.get(row.dataset.ticketRef);
    if (!block) return;
    const named = block.querySelector('[data-booklet-choice]').value === 'named';
    const mine = row.querySelector('[data-attendee-mine]').checked;
    const name = block.querySelector('[data-booklet-name]');
    block.querySelector('[data-booklet-name-field]').hidden = !named;
    name.required = named;
    if (named && prefill && !name.value.trim()) {
      name.value = mine && row.dataset.profileHandle ? '@' + row.dataset.profileHandle : row.querySelector('[data-attendee-name]').value;
    }
    block.querySelector('[data-booklet-profile]').hidden = !named || !mine;
    const avatar = block.querySelector('[data-booklet-avatar]');
    avatar.disabled = !named || !mine;
    if (!mine) avatar.checked = false;
  }
  function updateFood(row) {
    const other = row.querySelector('[data-attendee-diet]').value === 'other';
    const field = row.querySelector('[data-attendee-other]');
    field.hidden = !other;
    field.querySelector('input').required = other;
  }
  function clearProfile(row) {
    row.querySelector('[data-attendee-mine]').checked = false;
    const name = row.querySelector('[data-attendee-name]');
    const shirt = row.querySelector('[data-attendee-shirt]');
    if (name.value === row.dataset.profileName) name.value = '';
    if (shirt.value === row.dataset.profileShirt) shirt.value = '';
    [['[data-attendee-diet]', 'profileDiet'], ['[data-attendee-other-input]', 'profileOther'], ['[data-attendee-allergies]', 'profileAllergies']].forEach(([selector, key]) => {
      const field = row.querySelector(selector);
      if (field.value === row.dataset[key]) field.value = '';
    });
    row.querySelector('[data-profile-note]')?.remove();
    updateFood(row);
    updateBooklet(row);
  }
  rows.forEach(row => {
    updateFood(row);
    updateBooklet(row, true);
    booklets.get(row.dataset.ticketRef)?.querySelector('[data-booklet-choice]').addEventListener('change', () => updateBooklet(row, true));
    row.querySelector('[data-attendee-diet]').addEventListener('change', () => updateFood(row));
    row.querySelector('[data-attendee-shirt]').addEventListener('change', () => row.querySelector('[data-profile-note]')?.remove());
    row.querySelector('[data-attendee-mine]').addEventListener('change', event => {
      if (!event.target.checked) { clearProfile(row); return; }
      rows.forEach(other => { if (other !== row && other.querySelector('[data-attendee-mine]').checked) clearProfile(other); });
      if (row.dataset.profileName) row.querySelector('[data-attendee-name]').value = row.dataset.profileName;
      if (row.dataset.profileShirt) row.querySelector('[data-attendee-shirt]').value = row.dataset.profileShirt;
      if (row.dataset.profileDiet) {
        row.querySelector('[data-attendee-diet]').value = row.dataset.profileDiet;
        row.querySelector('[data-attendee-other-input]').value = row.dataset.profileOther || '';
      }
      if (row.dataset.profileAllergies) {
        row.querySelector('[data-attendee-allergies]').value = row.dataset.profileAllergies;
      }
      updateFood(row);
      updateBooklet(row, true);
    });
  });
  window.addEventListener('pageshow', () => rows.forEach(row => { updateFood(row); updateBooklet(row); }));
})();
