(() => {
  if (!document.querySelector('[data-ticket-confirmation]')) return;
  const endpoint = new URL(window.location.href);
  endpoint.searchParams.set('check_details', '1');
  let attempts = 0;
  async function check() {
    try {
      const response = await fetch(endpoint, { credentials: 'same-origin', cache: 'no-store' });
      if (response.ok) {
        const result = await response.json();
        if (result.details_url) {
          const destination = new URL(result.details_url, window.location.origin);
          if (destination.origin === window.location.origin) { window.location.replace(destination.href); return; }
        }
      }
    } catch (_) { /* The visible check-again link remains usable. */ }
    if (++attempts < 40) window.setTimeout(check, 3000);
  }
  window.setTimeout(check, 1500);
})();
