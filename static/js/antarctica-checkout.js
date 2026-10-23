(() => {
  const form = document.getElementById('expedition-checkout');
  if (!form) return;
  const fare = Object.fromEntries([...document.querySelectorAll('[data-berth]')].map(el => [el.dataset.berth, { name: el.dataset.name, cents: Number(el.dataset.price) }]));
  const capacities = { shared: 2, triple: 3, quad: 4 };
  const cabinLimits = { shared: 9, triple: 1, quad: 1 }; // Explicit preview inventory, never checkout authority.
  const money = cents => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);
  let step = 0;
  let allocations = [{ type: 'shared', quantity: 1, preference: 'coed' }];
  const party = document.getElementById('party-size');
  const error = document.getElementById('checkout-error');
  const remaining = Math.max(0, Number(party.max));
  const spotOptions = document.getElementById('spot-options');
  const customParty = document.getElementById('custom-party-size');
  for (let count = 1; count <= Math.min(6, remaining); count++) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = count === 6 ? '6+' : String(count);
    button.setAttribute('aria-label', count === 6 ? '6 or more spots' : `${count} ${count === 1 ? 'spot' : 'spots'}`);
    button.setAttribute('aria-pressed', String(count === 1));
    if (count === 6) button.setAttribute('aria-controls', 'custom-party-size');
    button.addEventListener('click', () => {
      const custom = count === 6;
      const previous = Number(party.value);
      party.value = String(custom && previous >= 6 && previous <= remaining ? previous : count);
      party.min = custom ? '6' : '1';
      customParty.hidden = !custom;
      spotOptions.querySelectorAll('button').forEach(option => option.setAttribute('aria-pressed', String(option === button)));
      error.textContent = '';
      if (custom) party.focus();
    });
    spotOptions.append(button);
  }
  if (!remaining) {
    party.value = '0';
    document.getElementById('checkout-next').disabled = true;
  }
  party.addEventListener('input', () => { error.textContent = ''; });

  const totalSpots = () => allocations.reduce((sum, a) => sum + a.quantity, 0);
  const validAllocation = () => {
    if (totalSpots() !== Number(party.value)) return `Assign exactly ${party.value} spots. You currently have ${totalSpots()}.`;
    for (const type of Object.keys(cabinLimits)) {
      if (allocations.filter(a => a.type === type).length > cabinLimits[type]) return `There are only ${cabinLimits[type]} ${fare[type].name.toLowerCase()} cabins available. Choose a different arrangement.`;
    }
    return '';
  };
  let removedCabin = null;
  function renderAllocations() {
    // A smaller party selected on the previous step cannot retain excess spots.
    let available = Number(party.value);
    allocations = allocations.filter(a => {
      a.quantity = Math.min(a.quantity, available);
      available -= a.quantity;
      return a.quantity > 0;
    });
    const activeID = document.activeElement?.id;
    const target = document.getElementById('cabin-allocations');
    target.replaceChildren();
    allocations.forEach((a, index) => {
      const row = document.createElement('article');
      row.className = 'cabin-allocation';
      row.innerHTML = `<div class="cabin-heading"><h2>Cabin ${index + 1}</h2><button type="button" class="remove-cabin" aria-label="Remove cabin ${index + 1}">Remove</button></div><div class="allocation-fields"><div><label for="cabin-type-${index}">Cabin type</label><select id="cabin-type-${index}"></select><p class="cabin-fare"></p></div><div><span class="cabin-spots-label" id="cabin-spots-label-${index}">Your group’s spots</span><div id="cabin-spots-${index}" class="spot-options cabin-spot-options" role="group" aria-labelledby="cabin-spots-label-${index}"></div></div></div><div class="roommate-request"></div>`;
      const type = row.querySelector(`#cabin-type-${index}`);
      for (const [key, f] of Object.entries(fare)) type.add(new Option(f.name, key));
      type.value = a.type;
      row.querySelector('.cabin-fare').textContent = `${money(fare[a.type].cents)} USD per person`;
      type.addEventListener('change', () => { a.type = type.value; a.quantity = Math.min(a.quantity, capacities[a.type]); a.preference = 'coed'; renderAllocations(); });
      const spots = row.querySelector(`#cabin-spots-${index}`);
      for (let n = 1; n <= capacities[a.type]; n++) {
        const button = document.createElement('button');
        button.type = 'button';
        button.id = `cabin-spots-${index}-${n}`;
        button.textContent = String(n);
        button.setAttribute('aria-label', `${n} ${n === 1 ? 'spot' : 'spots'} in cabin ${index + 1}`);
        button.setAttribute('aria-pressed', String(n === a.quantity));
        button.disabled = n > Number(party.value) - totalSpots() + a.quantity;
        if (button.disabled) button.title = 'Not enough unassigned people. Reduce another cabin’s spots first.';
        button.addEventListener('click', () => {
          if (n > Number(party.value) - totalSpots() + a.quantity) return;
          a.quantity = n; renderAllocations();
        });
        spots.append(button);
      }
      const preference = row.querySelector('.roommate-request');
      if (a.type === 'shared' && a.quantity < capacities[a.type]) {
        preference.innerHTML = `<label for="roommate-${index}">Roommate preference (not guaranteed)</label><select id="roommate-${index}"><option value="male">M</option><option value="female">F</option><option value="coed">Co-ed</option></select>`;
        const select = preference.querySelector('select');
        select.value = a.preference;
        select.addEventListener('change', () => { a.preference = select.value; });
      } else {
        const note = document.createElement('p');
        note.textContent = a.quantity === capacities[a.type] ? 'Your group fills this cabin. No outside roommate preference is needed.' : 'Shared triple and quad cabins default to co-ed.';
        preference.append(note);
      }
      row.querySelector('button').addEventListener('click', () => { removedCabin = { cabin: { ...a }, index }; allocations.splice(index, 1); renderAllocations(); document.getElementById('undo-cabin').focus(); });
      target.append(row);
    });
    const unassigned = Number(party.value) - totalSpots();
    document.getElementById('allocation-count').textContent = unassigned ? `${unassigned} ${unassigned === 1 ? 'person needs' : 'people need'} a spot` : 'Everyone has a spot!';
    const people = document.getElementById('allocation-people');
    people.replaceChildren();
    for (let n = 0; n < Number(party.value); n++) {
      const person = document.createElement('span');
      person.className = `allocation-person${n >= unassigned ? ' assigned' : ''}`;
      person.innerHTML = '<svg viewBox="0 0 24 40" fill="currentColor" aria-hidden="true"><circle cx="12" cy="5" r="4"/><path d="M8 11h8a4 4 0 0 1 4 4v10h-4v14h-3V26h-2v13H8V25H4V15a4 4 0 0 1 4-4Z"/></svg>';
      people.append(person);
    }
    document.getElementById('allocation-help').textContent = unassigned > 0 ? 'Dark figures still need a spot. Choose spots below or add a cabin.' : unassigned < 0 ? `Remove ${-unassigned} spots to match your group size.` : 'Everyone has a spot. Continue to merch when you’re ready.';
    document.getElementById('add-cabin').disabled = unassigned <= 0 || !Object.keys(cabinLimits).some(key => allocations.filter(a => a.type === key).length < cabinLimits[key]);
    document.getElementById('cabin-undo').hidden = !removedCabin;
    const canUndo = removedCabin && removedCabin.cabin.quantity <= unassigned && allocations.filter(a => a.type === removedCabin.cabin.type).length < cabinLimits[removedCabin.cabin.type];
    document.getElementById('undo-cabin').disabled = !canUndo;
    document.getElementById('undo-cabin').title = canUndo ? 'Restore removed cabin' : 'Free up spots before restoring this cabin';
    if (!allocations.length) {
      const empty = document.createElement('div');
      empty.className = 'cabins-empty';
      const title = document.createElement('h2'); title.textContent = 'Let’s find your first cabin.';
      const help = document.createElement('p'); help.textContent = 'Use + Add cabin above to start arranging your group, or Undo to restore the cabin you removed.';
      empty.append(title, help); target.append(empty);
    }
    error.textContent = '';
    if (activeID) document.getElementById(activeID)?.focus();
  }
  const merchInputs = [...document.querySelectorAll('.merch-quantity')];
  const merchTotal = () => merchInputs.reduce((sum, el) => sum + Number(el.value) * Number(el.dataset.price), 0);
  const validMerch = () => merchInputs.every(el => el.value !== '' && Number.isInteger(Number(el.value)) && Number(el.value) >= 0 && Number(el.value) <= Number(el.max));
  function renderMerchTotal() {
    const valid = validMerch();
    document.getElementById('merch-subtotal').textContent = valid ? money(merchTotal()) : '—';
    const count = merchInputs.reduce((sum, el) => sum + Number(el.value), 0);
    document.getElementById('merch-count').textContent = valid ? (count ? `${count} ${count === 1 ? 'item' : 'items'} · Onboard pickup included` : 'No items added · Optional') : 'Enter a whole number within the available quantity.';
    merchInputs.forEach(el => {
      const row = el.closest('.checkout-merch');
      const quantity = Number(el.value);
      const validQuantity = el.value !== '' && Number.isInteger(quantity) && quantity >= 0 && quantity <= Number(el.max);
      el.setAttribute('aria-invalid', String(!validQuantity));
      row.querySelector('[data-merch-line-total]').textContent = validQuantity ? money(quantity * Number(el.dataset.price)) : '—';
      row.querySelector('[data-quantity-change="-1"]').disabled = quantity <= 0;
      row.querySelector('[data-quantity-change="1"]').disabled = quantity >= Number(el.max);
    });
  }
  merchInputs.forEach(el => {
    el.addEventListener('input', renderMerchTotal);
    el.closest('.quantity-stepper').querySelectorAll('button').forEach(button => button.addEventListener('click', () => {
      el.value = String(Math.max(0, Math.min(Number(el.max), Math.floor(Number(el.value) || 0) + Number(button.dataset.quantityChange))));
      renderMerchTotal();
    }));
  });
  renderMerchTotal();
  function renderPayment() {
    const summary = document.getElementById('booking-summary');
    summary.replaceChildren();
    const heading = document.createElement('h2');
    heading.textContent = `${party.value} ${Number(party.value) === 1 ? 'passenger' : 'passengers'} · ${allocations.length} ${allocations.length === 1 ? 'cabin' : 'cabins'}`;
    summary.append(heading);
    for (const a of allocations) {
      const cabin = document.createElement('div'); cabin.className = 'booking-cabin-summary';
      const people = document.createElement('div'); people.className = 'allocation-people';
      people.setAttribute('role', 'img');
      people.setAttribute('aria-label', `${a.quantity} ${a.quantity === 1 ? 'passenger' : 'passengers'}`);
      for (let n = 0; n < a.quantity; n++) {
        const person = document.createElement('span'); person.className = 'allocation-person';
        person.innerHTML = '<svg viewBox="0 0 24 40" fill="currentColor" aria-hidden="true"><circle cx="12" cy="5" r="4"/><path d="M8 11h8a4 4 0 0 1 4 4v10h-4v14h-3V26h-2v13H8V25H4V15a4 4 0 0 1 4-4Z"/></svg>';
        people.append(person);
      }
      const detail = document.createElement('div');
      const name = document.createElement('strong'); name.textContent = fare[a.type].name; detail.append(name);
      if (a.quantity < capacities[a.type]) {
        const preference = document.createElement('p');
        preference.textContent = a.type === 'shared' ? `${a.preference === 'male' ? 'M' : a.preference === 'female' ? 'F' : 'Co-ed'} roommate requested (not guaranteed)` : 'Co-ed sharing';
        detail.append(preference);
      }
      cabin.append(people, detail); summary.append(cabin);
    }
    for (const el of merchInputs.filter(el => Number(el.value) > 0)) {
      const p = document.createElement('p'); p.textContent = `${el.value} × ${el.dataset.name}${el.dataset.variant ? ` · ${el.dataset.variant}` : ''} · ${money(Number(el.value) * Number(el.dataset.price))}`; summary.append(p);
    }
    const full = form.elements.plan.value === 'full';
    const bitcoin = form.elements.provider.value === 'opennode';
    const standard = allocations.reduce((sum, a) => sum + a.quantity * fare[a.type].cents, 0);
    const discounted = allocations.reduce((sum, a) => sum + a.quantity * Math.round(fare[a.type].cents * 95 / 100), 0);
    const total = full && bitcoin ? discounted : standard;
    const installment = allocations.reduce((sum, a) => sum + a.quantity * Math.floor(fare[a.type].cents / 5), 0);
    const totals = document.getElementById('checkout-totals');
    totals.replaceChildren();
    const add = (tag, text) => { const el = document.createElement(tag); el.textContent = text; totals.append(el); };
    const row = (label, amount, className = '', detail = '') => {
      const line = document.createElement('div'); line.className = `checkout-total-row ${className}`;
      const name = document.createElement('span'); name.textContent = label;
      if (detail) { const description = document.createElement('small'); description.className = 'fare-line-detail'; description.textContent = detail; name.append(description); }
      const value = document.createElement('strong'); value.textContent = money(amount);
      line.append(name, value); totals.append(line);
    };
    add('h2', 'Your total');
    add('p', `${totalSpots()} ${totalSpots() === 1 ? 'person' : 'people'} · ${allocations.length} ${allocations.length === 1 ? 'cabin' : 'cabins'}`);
    allocations.forEach((a, index) => {
      row(`Cabin ${index + 1} · ${fare[a.type].name}`, a.quantity * fare[a.type].cents, 'checkout-fare-row', `${a.quantity} ${a.quantity === 1 ? 'person' : 'people'} × ${money(fare[a.type].cents)} per person`);
    });
    row('Passage subtotal', standard, 'checkout-booking-total');
    if (full && bitcoin && form.dataset.beta !== 'true') row('Bitcoin discount · 5%', -(standard - total), 'checkout-saving');
    const merch = merchTotal();
    row('Merch · paid at signup', merch);
    row('Estimated booking total · USD', total + merch, 'checkout-booking-total');
    row('Due today to reserve your spot', (full ? total : installment) + merch, 'checkout-due');
    if (merch) add('p', 'Onboard pickup included. Any applicable merchandise tax is not included in this demonstration.');
    if (full) add('p', 'Paid in full. No remaining passage installments.');
    else {
      add('h3', 'Remaining passage payments');
      ['January 1, 2027', 'April 1, 2027', 'July 1, 2027', 'October 1, 2027'].forEach((date, index) => row(date, index === 3 ? total - installment * 4 : installment, 'checkout-schedule-row'));
    }
  }
  function showStep() {
    document.querySelectorAll('[data-step]').forEach((el, i) => { el.hidden = i !== step; });
    document.querySelectorAll('.checkout-progress li').forEach((el, i) => { if (i === step) el.setAttribute('aria-current', 'step'); else el.removeAttribute('aria-current'); });
    document.getElementById('checkout-back').hidden = step === 0;
    const next = document.getElementById('checkout-next'); next.hidden = step === 3;
    next.textContent = ['Choose berths →', 'Next →', 'Review & payment →'][step] || '';
    error.textContent = '';
    if (step === 1) renderAllocations();
    if (step === 3) renderPayment();
    document.querySelector(`[data-step="${step}"] h1`).focus();
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (step === 0 && (!Number.isInteger(Number(party.value)) || Number(party.value) < Number(party.min) || Number(party.value) > remaining)) { error.textContent = `Choose between ${party.min} and ${remaining} spots.`; return; }
    if (step === 1) { const message = validAllocation(); if (message) { error.textContent = message; return; } }
    if (step === 2 && !validMerch()) { merchInputs.find(el => !el.validity.valid)?.reportValidity(); return; }
    if (step < 3) { step++; if (step === 2 && form.dataset.live === 'true') step++; showStep(); return; }
    if (form.dataset.live !== 'true') return;
    if (!form.querySelector('[name=terms_accepted]')?.checked) { error.textContent = 'Please read and accept the booking terms.'; return; }
    const invalid = [...form.querySelectorAll('.checkout-billing input')].find(el => !el.checkValidity());
    if (invalid) { invalid.reportValidity(); return; }
    const plan = form.querySelector('[name=plan]:checked').value;
    const provider = form.querySelector('[name=provider]:checked').value;
    const expected = allocations.reduce((sum, a) => sum + a.quantity * (plan === 'full' && provider === 'opennode' ? Math.round(fare[a.type].cents * 95 / 100) : fare[a.type].cents), 0);
    const field = name => form.querySelector(`[name=billing-${name}]`).value.trim();
    const payload = { csrf: form.dataset.csrf, key: form.dataset.requestKey, spots: Number(party.value), selections: allocations.map(a => ({ berth: a.type, quantity: a.quantity, preference: a.preference })), plan, provider, terms_version: form.querySelector('[name=terms_version]').value, accepted: true, expected_total: expected, address: { Line1: field('line1'), City: field('city'), Region: field('region'), PostalCode: field('postal'), Country: field('country').toUpperCase() } };
    // Freeze this attempt after submission: uncertain responses must retry the
    // same reservation, never silently create another hold or charge.
    try { sessionStorage.setItem('antarctica-pending-checkout', JSON.stringify(payload)); } catch (_) {}
    const controls = [...form.querySelectorAll('input,select,button')];
    controls.forEach(el => { el.disabled = true; });
    const pay = document.getElementById('checkout-pay');
    pay.textContent = 'Preparing your order…'; error.textContent = '';
    try {
      const response = await fetch('/expeditions/antarctica/checkout', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
      if (!response.ok) { if ([400, 401, 403, 422].includes(response.status)) controls.forEach(el => { el.disabled = false; }); throw new Error(await response.text()); }
      const result = await response.json();
      if (!/^\/services\/orders\/[^/]+$/.test(result.url)) throw new Error('Unable to open your order. Please retry.');
      try { sessionStorage.removeItem('antarctica-pending-checkout'); } catch (_) {}
      window.location.assign(result.url);
    } catch (e) {
      error.textContent = e.message || 'Unable to prepare payment. Retry this checkout.';
      pay.disabled = false; pay.textContent = 'Retry payment →';
    }
  });
  document.getElementById('checkout-back').addEventListener('click', () => { step--; if (step === 2 && form.dataset.live === 'true') step--; showStep(); });
  document.getElementById('add-cabin').addEventListener('click', () => {
    const type = Object.keys(cabinLimits).find(key => allocations.filter(a => a.type === key).length < cabinLimits[key]);
    if (!type || totalSpots() >= Number(party.value)) return;
    allocations.push({ type, quantity: 1, preference: 'coed' });
    renderAllocations(); document.getElementById(`cabin-type-${allocations.length - 1}`).focus();
  });
  document.getElementById('undo-cabin').addEventListener('click', () => {
    if (!removedCabin || document.getElementById('undo-cabin').disabled) return;
    const index = Math.min(removedCabin.index, allocations.length);
    allocations.splice(index, 0, removedCabin.cabin); removedCabin = null;
    renderAllocations(); document.getElementById(`cabin-type-${index}`).focus();
  });
  form.querySelectorAll('[name=plan],[name=provider]').forEach(el => el.addEventListener('change', renderPayment));
  if (form.dataset.live === 'true') {
    try {
      const pending = JSON.parse(sessionStorage.getItem('antarctica-pending-checkout') || 'null');
      if (pending?.key === form.dataset.requestKey) {
        allocations = pending.selections.map(a => ({ type: a.berth, quantity: a.quantity, preference: a.preference }));
        party.value = String(pending.spots);
        form.elements.plan.value = pending.plan; form.elements.provider.value = 'opennode';
        for (const [field, key] of Object.entries({ line1: 'Line1', city: 'City', region: 'Region', postal: 'PostalCode', country: 'Country' })) form.querySelector(`[name=billing-${field}]`).value = pending.address[key] || '';
        step = 3; showStep();
        error.textContent = 'Your previous checkout is ready to retry. Review the details and accept the terms to continue.';
      }
    } catch (_) { /* Storage may be unavailable; the server still protects retries. */ }
  }
  if (form.dataset.beta === 'true') {
    party.value = '1';
    form.querySelector('[name=plan][value=full]').checked = true;
    form.querySelector('[name=plan][value=installments]').disabled = true;
    renderAllocations();
    if (step === 3) renderPayment();
  }
})();
