(() => {
  const page = document.body;
  if (!page) return;

  const destination = '/dashboard/settings?resume=settings-action';
  const reauthURL = '/reauth?next=' + encodeURIComponent(destination);
  const forms = document.querySelectorAll('main.person-emails-page form[method="POST"], main.person-emails-page form[method="post"]');
  const containsSecret = (form) => Boolean(form.querySelector('input[type="password"]'));

  const location = new URL(window.location.href);
  if (location.searchParams.get('resume') === 'settings-action') {
    location.searchParams.delete('resume');
    window.history.replaceState({}, '', location.pathname + location.search + location.hash);
    let pending;
    try {
      pending = JSON.parse(sessionStorage.getItem('btcpp:pending-settings-action') || 'null');
      sessionStorage.removeItem('btcpp:pending-settings-action');
    } catch (_) {
      return;
    }
    if (!pending || typeof pending.action !== 'string' || !Array.isArray(pending.values)) return;
    const original = Array.from(forms).find((candidate) => candidate.action === pending.action);
    if (!original || containsSecret(original)) return;
    const replay = document.createElement('form');
    replay.method = 'POST';
    replay.action = original.action;
    const csrf = original.querySelector('input[name="csrf"]');
    const values = csrf ? [['csrf', csrf.value], ...pending.values] : pending.values;
    values.forEach(([name, value]) => {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      replay.appendChild(input);
    });
    document.body.appendChild(replay);
    replay.submit();
    return;
  }

  if (page.dataset.reauthRequired !== 'true') return;
  const beginReauthentication = () => window.location.assign(reauthURL);

  forms.forEach((form) => {
    if (containsSecret(form)) {
      form.addEventListener('focusin', beginReauthentication, {once: true});
    }
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      if (!containsSecret(form)) {
        const values = [];
        new FormData(form).forEach((value, name) => {
          if (name !== 'csrf' && typeof value === 'string') values.push([name, value]);
        });
        if (event.submitter && event.submitter.name) values.push([event.submitter.name, event.submitter.value]);
        try {
          sessionStorage.setItem('btcpp:pending-settings-action', JSON.stringify({action: form.action, values}));
        } catch (_) {}
      }
      beginReauthentication();
    });
  });
})();
