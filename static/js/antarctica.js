(() => {
  const choice = document.getElementById('berth-choice');
  const change = document.getElementById('bitcoin-change');
  const priceNow = document.getElementById('bitcoin-price-now');
  const priceFuture = document.getElementById('bitcoin-price-future');
  const source = document.getElementById('bitcoin-price-source');
  let priceRevision = 0;
  let futureEdited = false;
  const dollars = cents => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);
  const updateScenario = () => {
    if (!choice || !change) return;
    const price = Number(choice.selectedOptions[0].dataset.priceCents);
    const now = priceNow.valueAsNumber;
    const future = priceFuture.valueAsNumber;
    const valid = Number.isFinite(now) && now >= 1 && now <= 1e9 && Number.isFinite(future) && future >= 0 && future <= 1e9;
    document.getElementById('bitcoin-price-error').hidden = valid;
    document.getElementById('bitcoin-scenario-results').hidden = !valid;
    change.disabled = !valid;
    if (!valid) return;
    const percent = (future / now - 1) * 100;
    change.max = String(Math.max(200, Math.ceil(percent / 50) * 50));
    change.value = String(percent);
    document.querySelector('.range-labels span:last-child').textContent = `+${change.max}%`;
    const heldBTC = price / 100 / 2 / now;
    const held = Math.round(heldBTC * future * 100);
    document.getElementById('scenario-btc-deposit').textContent = `${(price / 100 * .75 / now).toFixed(8)} BTC`;
    document.getElementById('scenario-btc-held').textContent = `${heldBTC.toFixed(8)} BTC`;
    document.getElementById('scenario-break-even').textContent = dollars(now * 1.5 * 100);
    const fare = price * 3 / 4;
    const balance = fare - held;
    document.getElementById('bitcoin-change-label').textContent = `${percent > 0 ? '+' : ''}${Number(percent.toFixed(2))}%`;
    change.setAttribute('aria-valuetext', `${percent}% bitcoin price change`);
    document.getElementById('scenario-held').textContent = dollars(held);
    document.getElementById('scenario-fare').textContent = dollars(fare);
    const maximum = Math.max(price * 1.5, held);
    document.getElementById('scenario-maximum').textContent = dollars(maximum);
    document.getElementById('scenario-held-bar').style.width = `${held / maximum * 100}%`;
    document.getElementById('scenario-fare-bar').style.width = `${fare / maximum * 100}%`;
    document.getElementById('scenario-balance').textContent = dollars(Math.abs(balance));
    document.getElementById('scenario-result-label').textContent = balance > 0
      ? 'Balance at Oct 1, 2027 · before taxes and fees'
      : balance < 0 ? 'Potential refund before taxes and fees' : 'Fare covered before taxes and fees';
    document.getElementById('scenario-explanation').textContent = percent === 0
      ? 'With no price change, you still owe 25% of the fare.'
      : balance > 0 ? 'The held bitcoin does not cover the remaining fare. Taxes and fees may add to this balance.'
      : balance < 0 ? 'This is the gross surplus. Taxes and fees may reduce it or leave an amount due.'
      : 'The held bitcoin matches the remaining fare. Taxes and fees may still leave an amount due.';
  };
  if (choice) {
    const update = () => {
      const option = choice.selectedOptions[0];
      document.getElementById('deposit-amount').textContent = option.dataset.deposit;
      document.querySelectorAll('[data-installment-amount]').forEach(el => { el.textContent = option.dataset.deposit; });
      document.getElementById('bitcoin-amount').textContent = option.dataset.contribution;
      document.getElementById('balance-amount').textContent = option.dataset.contribution;
      document.getElementById('reservation-amount').textContent = option.dataset.reservation;
      document.getElementById('held-amount').textContent = option.dataset.held;
      updateScenario();
    };
    choice.addEventListener('change', update);
    document.querySelectorAll('[data-berth-select]').forEach(link => link.addEventListener('click', () => {
      choice.value = link.dataset.berthSelect;
      update();
    }));
    update();
  }
  if (change && choice) {
    change.addEventListener('input', () => {
      futureEdited = true;
      priceFuture.value = (priceNow.valueAsNumber * (1 + Number(change.value) / 100)).toFixed(2);
      updateScenario();
    });
    priceNow.addEventListener('input', () => {
      priceRevision++;
      source.textContent = 'Using your entered deposit price, not a live quote.';
      updateScenario();
    });
    priceFuture.addEventListener('input', () => { futureEdited = true; updateScenario(); });
    document.getElementById('bitcoin-spot').addEventListener('click', async () => {
      const button = document.getElementById('bitcoin-spot');
      const revision = priceRevision;
      button.disabled = true;
      source.textContent = 'Loading BTC/USD spot price from Coinbase…';
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 8000);
      try {
        const response = await fetch('https://api.coinbase.com/v2/prices/BTC-USD/spot', { signal: controller.signal, cache: 'no-store' });
        if (!response.ok) throw new Error('Quote unavailable');
        const body = await response.json();
        const quote = Number(body.data?.amount);
        if (body.data?.base !== 'BTC' || body.data?.currency !== 'USD' || !Number.isFinite(quote) || quote < 1 || quote > 1e9) throw new Error('Invalid quote');
        if (revision !== priceRevision) return;
        priceNow.value = quote.toFixed(2);
        if (!futureEdited) priceFuture.value = quote.toFixed(2);
        source.textContent = `Coinbase BTC/USD spot: ${dollars(quote * 100)}. Retrieved ${new Date().toLocaleString()}. Snapshot only; not a locked exchange rate.`;
        updateScenario();
      } catch (_) {
        if (revision === priceRevision) source.textContent = 'Live price unavailable. Existing inputs are unchanged; enter your own deposit price to model the result.';
      } finally {
        clearTimeout(timeout);
        button.disabled = false;
      }
    });
    document.getElementById('bitcoin-explorer').hidden = false;
  }
  const patches = {
    'chest-left': ['Left chest patch', 'A prominent placement on the front of the expedition parka.'],
    'chest-right': ['Right chest patch', 'A companion placement alongside the expedition identity.'],
    sleeve: ['Sleeve patch', 'A compact placement on the upper sleeve of the expedition parka.'],
    back: ['Back panel', 'A larger proposed placement across the back of the expedition parka.'],
  };
  const selectPatch = key => {
    const patch = patches[key];
    if (!patch) return;
    document.getElementById('patch-name').textContent = patch[0];
    document.getElementById('patch-copy').textContent = patch[1];
  };
  document.querySelectorAll('[data-patch]').forEach(button => button.addEventListener('click', () => selectPatch(button.dataset.patch)));
  document.querySelectorAll('[data-parka-view]').forEach(button => button.addEventListener('click', () => {
    const front = button.dataset.parkaView === 'front';
    document.querySelectorAll('.parka-front').forEach(el => el.toggleAttribute('hidden', !front));
    document.querySelectorAll('.parka-back').forEach(el => el.toggleAttribute('hidden', front));
    document.querySelectorAll('[data-parka-view]').forEach(el => el.setAttribute('aria-pressed', String(el === button)));
    selectPatch(front ? 'chest-left' : 'back');
  }));
})();
