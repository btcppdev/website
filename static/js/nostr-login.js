(() => {
  const button = document.querySelector('[data-nostr-login]');
  const status = document.querySelector('[data-nostr-status]');
  if (!button || !status) return;

  const show = (message, isError = false) => {
    status.textContent = message;
    status.classList.toggle('text-red-700', isError);
    status.classList.toggle('text-gray-500', !isError);
  };

  button.addEventListener('click', async () => {
    if (!window.nostr || typeof window.nostr.signEvent !== 'function') {
      show('No Nostr signer was found. Install or enable a NIP-07 browser extension.', true);
      return;
    }
    button.disabled = true;
    show('Waiting for your Nostr signer…');
    try {
      const next = button.dataset.next || '/dashboard';
      const challengeResponse = await fetch('/auth/nostr/challenge?next=' + encodeURIComponent(next), {
        credentials: 'same-origin',
        headers: {Accept: 'application/json'},
      });
      if (!challengeResponse.ok) throw new Error('Unable to start Nostr sign-in.');
      const challenge = await challengeResponse.json();
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
      show('Verifying signature…');
      const verifyResponse = await fetch('/auth/nostr/verify', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {'Content-Type': 'application/json', Accept: 'application/json'},
        body: JSON.stringify({event}),
      });
      const result = await verifyResponse.json();
      if (!verifyResponse.ok) throw new Error(result.error || 'Nostr sign-in failed.');
      window.location.assign(result.redirect || '/dashboard');
    } catch (error) {
      show(error instanceof Error ? error.message : 'Nostr sign-in failed.', true);
      button.disabled = false;
    }
  });
})();
