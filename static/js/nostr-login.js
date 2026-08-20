(() => {
  const buttons = document.querySelectorAll('[data-nostr-login], [data-nostr-link]');
  if (!buttons.length) return;

  const show = (status, message, isError = false) => {
    if (!status) return;
    status.textContent = message;
    status.classList.toggle('text-red-700', isError);
    status.classList.toggle('text-gray-500', !isError);
  };

  buttons.forEach((button) => button.addEventListener('click', async () => {
    const status = button.parentElement.querySelector('[data-nostr-status]');
    if (!window.nostr || typeof window.nostr.signEvent !== 'function') {
      show(status, 'No Nostr signer was found. Install or enable a NIP-07 browser extension.', true);
      return;
    }
    button.disabled = true;
    show(status, 'Waiting for your Nostr signer…');
    try {
      const next = button.dataset.next || '/dashboard';
      const challengeURL = button.dataset.challenge || ('/auth/nostr/challenge?next=' + encodeURIComponent(next));
      const verifyURL = button.dataset.verify || '/auth/nostr/verify';
      const challengeResponse = await fetch(challengeURL, {
        credentials: 'same-origin',
        headers: {Accept: 'application/json'},
      });
      const challenge = await challengeResponse.json();
      if (!challengeResponse.ok) throw new Error(challenge.error || 'Unable to start Nostr sign-in.');
      const event = await window.nostr.signEvent({
        kind: challenge.kind,
        created_at: challenge.created_at,
        tags: [
          ['u', challenge.url],
          ['method', challenge.method],
          ['challenge', challenge.challenge],
        ],
        content: '',
      });
      show(status, 'Verifying signature…');
      const verifyResponse = await fetch(verifyURL, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {'Content-Type': 'application/json', Accept: 'application/json'},
        body: JSON.stringify({event}),
      });
      const result = await verifyResponse.json();
      if (!verifyResponse.ok) throw new Error(result.error || 'Nostr sign-in failed.');
      window.location.assign(result.redirect || '/dashboard');
    } catch (error) {
      show(status, error instanceof Error ? error.message : 'Nostr sign-in failed.', true);
      button.disabled = false;
    }
  }));
})();
