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
  const totalSpots = () => allocations.reduce((sum, a) => sum + a.quantity, 0);
  const validAllocation = () => {
    if (totalSpots() !== Number(party.value)) return `Assign exactly ${party.value} spots. You currently have ${totalSpots()}.`;
    for (const type of Object.keys(cabinLimits)) {
      if (allocations.filter(a => a.type === type).length > cabinLimits[type]) return `There are only ${cabinLimits[type]} ${fare[type].name.toLowerCase()} cabins in this preview. Choose a different arrangement.`;
    }
    return '';
  };
  function renderAllocations() {
    const activeID = document.activeElement?.id;
    const target = document.getElementById('cabin-allocations');
    target.replaceChildren();
    allocations.forEach((a, index) => {
      const row = document.createElement('article');
      row.className = 'cabin-allocation';
      row.innerHTML = `<h2>Cabin arrangement ${index + 1}</h2><div class="allocation-fields"><div><label for="cabin-type-${index}">Cabin type</label><select id="cabin-type-${index}"></select></div><div><label for="cabin-spots-${index}">Your group’s spots</label><select id="cabin-spots-${index}"></select></div></div><div class="roommate-request"></div><button type="button" class="checkout-secondary">Remove this cabin</button>`;
      const type = row.querySelector(`#cabin-type-${index}`);
      for (const [key, f] of Object.entries(fare)) type.add(new Option(`${f.name} · ${money(f.cents)} / person`, key));
      type.value = a.type;
      type.addEventListener('change', () => { a.type = type.value; a.quantity = Math.min(a.quantity, capacities[a.type]); a.preference = 'coed'; renderAllocations(); });
      const spots = row.querySelector(`#cabin-spots-${index}`);
      for (let n = 1; n <= capacities[a.type]; n++) spots.add(new Option(String(n), String(n)));
      spots.value = String(a.quantity);
      spots.addEventListener('change', () => { a.quantity = Number(spots.value); renderAllocations(); });
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
      row.querySelector('button').addEventListener('click', () => { allocations.splice(index, 1); renderAllocations(); document.getElementById('add-cabin').focus(); });
      target.append(row);
    });
    document.getElementById('allocation-count').textContent = `${totalSpots()} of ${party.value} spots assigned.`;
    document.getElementById('add-cabin').disabled = allocations.length >= Number(party.value);
    error.textContent = '';
    if (activeID) document.getElementById(activeID)?.focus();
  }
  const merchInputs = [...document.querySelectorAll('.merch-quantity')];
  const merchTotal = () => merchInputs.reduce((sum, el) => sum + Number(el.value) * Number(el.dataset.price), 0);
  const validMerch = () => merchInputs.every(el => Number.isInteger(Number(el.value)) && Number(el.value) >= 0 && Number(el.value) <= Number(el.max));
  function renderMerchTotal() {
    document.getElementById('merch-subtotal').textContent = validMerch() ? `Merch subtotal: ${money(merchTotal())} USD` : 'Choose valid whole-number quantities within the displayed limits.';
  }
  merchInputs.forEach(el => el.addEventListener('input', renderMerchTotal));
  renderMerchTotal();
  function renderPayment() {
    const summary = document.getElementById('booking-summary');
    summary.replaceChildren();
    const heading = document.createElement('h2'); heading.textContent = `${party.value} spots · ${allocations.length} cabin arrangements`; summary.append(heading);
    for (const a of allocations) {
      const line = document.createElement('p');
      const pref = a.type === 'shared' && a.quantity < 2 ? ` · ${a.preference === 'male' ? 'M' : a.preference === 'female' ? 'F' : 'Co-ed'} roommate requested (not guaranteed)` : a.quantity < capacities[a.type] ? ' · Co-ed sharing' : ' · Filled by your group';
      line.textContent = `${a.quantity} × ${fare[a.type].name}${pref}`; summary.append(line);
    }
    for (const el of merchInputs.filter(el => Number(el.value) > 0)) {
      const p = document.createElement('p'); p.textContent = `${el.value} × ${el.dataset.name}${el.dataset.variant ? ` · ${el.dataset.variant}` : ''} · ${money(Number(el.value) * Number(el.dataset.price))}`; summary.append(p);
    }
    const full = form.elements.plan.value === 'full';
    const bitcoin = form.elements.provider.value === 'opennode';
    const standard = allocations.reduce((sum, a) => sum + a.quantity * fare[a.type].cents, 0);
    const discounted = allocations.reduce((sum, a) => sum + a.quantity * Math.round(fare[a.type].cents * 95 / 100), 0);
    const total = full && bitcoin ? discounted : standard;
    const totals = document.getElementById('checkout-totals');
    totals.replaceChildren();
    const add = (tag, text) => { const el = document.createElement(tag); el.textContent = text; totals.append(el); };
    add('p', `Passage total: ${money(total)} USD${full && bitcoin ? ` · You save ${money(standard - total)}` : ''}`);
    const merch = merchTotal();
    add('p', `Merch subtotal: ${money(merch)} USD`);
    add('strong', `Estimated due at signup: ${money((full ? total : total / 5) + merch)}`);
    if (merch) add('p', 'Includes merchandise paid upfront. Onboard pickup · Shipping $0. Any applicable merchandise tax is not included in this preview.');
    if (full) add('p', 'No remaining passage installments.');
    else for (const date of ['January 1, 2027', 'April 1, 2027', 'July 1, 2027', 'October 1, 2027']) add('p', `${date} · ${money(total / 5)}`);
  }
  function showStep() {
    document.querySelectorAll('[data-step]').forEach((el, i) => { el.hidden = i !== step; });
    document.querySelectorAll('.checkout-progress li').forEach((el, i) => { if (i === step) el.setAttribute('aria-current', 'step'); else el.removeAttribute('aria-current'); });
    document.getElementById('checkout-back').hidden = step === 0;
    const next = document.getElementById('checkout-next'); next.hidden = step === 3;
    next.textContent = ['Choose berths →', 'Explore merch →', 'Review & payment →'][step] || '';
    error.textContent = '';
    if (step === 1) renderAllocations();
    if (step === 3) renderPayment();
    document.querySelector(`[data-step="${step}"] h1`).focus();
  }
  form.addEventListener('submit', event => {
    event.preventDefault();
    if (step === 0 && (!Number.isInteger(Number(party.value)) || Number(party.value) < 1 || Number(party.value) > Number(party.max))) { error.textContent = `Choose between 1 and ${party.max} spots for this preview.`; return; }
    if (step === 1) { const message = validAllocation(); if (message) { error.textContent = message; return; } }
    if (step === 2 && !validMerch()) { error.textContent = 'Choose valid whole-number merch quantities within the displayed limits.'; return; }
    if (step < 3) { step++; showStep(); }
  });
  document.getElementById('checkout-back').addEventListener('click', () => { step--; showStep(); });
  document.getElementById('add-cabin').addEventListener('click', () => { allocations.push({ type: 'shared', quantity: 1, preference: 'coed' }); renderAllocations(); document.getElementById(`cabin-type-${allocations.length - 1}`).focus(); });
  form.querySelectorAll('[name=plan],[name=provider]').forEach(el => el.addEventListener('change', renderPayment));
})();
