(() => {
  const dialog = document.querySelector('[data-upgrade-dialog]');
  const open = document.querySelector('[data-upgrade-open]');
  if (!dialog || !open) return;
  const qr = dialog.querySelector('[data-upgrade-qr]');
  const status = dialog.querySelector('[data-upgrade-status]');
  const sats = dialog.querySelector('[data-upgrade-sats]');
  const networks = Array.from(dialog.querySelectorAll('[data-network]'));
  let network = 'lightning', payment, timer, busy = false;
  const paymentURL = () => dialog.dataset.upgradeUrl + '/' + encodeURIComponent(payment.id);
  function render() {
    if (!payment) return;
    if (payment.qr_available && payment.expires_at && Date.now() >= Date.parse(payment.expires_at)) {
      payment.qr_available = false;
      payment.message = 'QR expired. Checking for payment; do not pay this invoice.';
    }
    status.textContent = payment.message;
    dialog.querySelector('[data-upgrade-amount]').textContent = payment.amount;
    sats.hidden = !payment.sats;
    sats.textContent = Number(payment.sats).toLocaleString() + ' sats';
    qr.hidden = !payment.qr_available;
    networks.forEach(button => {
      button.setAttribute('aria-pressed', String(button.dataset.network === network));
      button.disabled = !payment.qr_available;
    });
    dialog.querySelector('[data-upgrade-chain-note]').hidden = network !== 'onchain' || !payment.qr_available;
    if (payment.qr_available) {
      qr.alt = (network === 'lightning' ? 'Lightning' : 'On-chain') + ' payment QR code';
      const src = paymentURL() + '/qr?network=' + network;
      if (qr.getAttribute('src') !== src) qr.src = src;
    } else qr.removeAttribute('src');
  }
  async function responseJSON(response) {
    if (!response.ok) throw new Error(await response.text());
    return response.json();
  }
  function schedule() {
    clearTimeout(timer);
    if (dialog.open && payment && !['paid', 'expired', 'failed', 'refunded', 'review'].includes(payment.status)) timer = setTimeout(poll, 3000);
  }
  async function poll() {
    if (!dialog.open || !payment) return;
    try {
      payment = await responseJSON(await fetch(paymentURL(), {cache:'no-store'}));
      render();
      if (payment.status === 'paid') { window.location.reload(); return; }
    } catch (error) { render(); status.textContent = error.message || 'Unable to check payment. Do not charge again.'; }
    schedule();
  }
  open.addEventListener('click', async () => {
    if (busy) return;
    busy = true;
    network = 'lightning';
    payment = undefined;
    qr.hidden = true;
    sats.hidden = true;
    networks.forEach(button => { button.disabled = true; button.setAttribute('aria-pressed', String(button.dataset.network === network)); });
    dialog.querySelector('[data-upgrade-chain-note]').hidden = true;
    status.textContent = 'Preparing payment…';
    dialog.showModal();
    try {
      payment = await responseJSON(await fetch(dialog.dataset.upgradeUrl, {method:'POST',body:new URLSearchParams({csrf:dialog.dataset.csrf,revision:dialog.dataset.revision})}));
      if (dialog.open) { render(); if (payment.status === 'paid') window.location.reload(); else schedule(); }
    } catch (error) { status.textContent = error.message || 'Unable to prepare payment.'; }
    finally { busy = false; }
  });
  networks.forEach(button => button.addEventListener('click', () => { network = button.dataset.network; render(); }));
  dialog.querySelector('[data-upgrade-cancel]').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => { clearTimeout(timer); qr.hidden = true; qr.removeAttribute('src'); });
})();
