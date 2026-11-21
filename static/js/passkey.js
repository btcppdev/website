(() => {
  const toBuffer = (value) => {
    const base64 = value.replace(/-/g, '+').replace(/_/g, '/');
    const padded = base64 + '='.repeat((4 - base64.length % 4) % 4);
    return Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
  };

  const toBase64URL = (value) => {
    if (!value) return '';
    const bytes = new Uint8Array(value);
    let binary = '';
    bytes.forEach((byte) => { binary += String.fromCharCode(byte); });
    return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  };

  const creationOptions = (options) => {
    options.challenge = toBuffer(options.challenge);
    options.user.id = toBuffer(options.user.id);
    (options.excludeCredentials || []).forEach((credential) => {
      credential.id = toBuffer(credential.id);
    });
    return options;
  };

  const requestOptions = (options) => {
    options.challenge = toBuffer(options.challenge);
    (options.allowCredentials || []).forEach((credential) => {
      credential.id = toBuffer(credential.id);
    });
    return options;
  };

  const credentialJSON = (credential) => {
    const response = {clientDataJSON: toBase64URL(credential.response.clientDataJSON)};
    if (credential.response.attestationObject) {
      response.attestationObject = toBase64URL(credential.response.attestationObject);
      response.transports = typeof credential.response.getTransports === 'function' ? credential.response.getTransports() : [];
      if (typeof credential.response.getAuthenticatorData === 'function') response.authenticatorData = toBase64URL(credential.response.getAuthenticatorData());
      if (typeof credential.response.getPublicKey === 'function') response.publicKey = toBase64URL(credential.response.getPublicKey());
      if (typeof credential.response.getPublicKeyAlgorithm === 'function') response.publicKeyAlgorithm = credential.response.getPublicKeyAlgorithm();
    } else {
      response.authenticatorData = toBase64URL(credential.response.authenticatorData);
      response.signature = toBase64URL(credential.response.signature);
      response.userHandle = toBase64URL(credential.response.userHandle);
    }
    return {
      id: credential.id,
      rawId: toBase64URL(credential.rawId),
      type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment || '',
      clientExtensionResults: credential.getClientExtensionResults(),
      response,
    };
  };

  class PasskeyRequestError extends Error {}

  const fetchJSON = async (url, options, fallback) => {
    let response;
    try {
      response = await fetch(url, options);
    } catch (_) {
      throw new PasskeyRequestError('Could not connect. Check your connection and try again.');
    }
    let data;
    try {
      data = await response.json();
    } catch (_) {
      throw new PasskeyRequestError(fallback);
    }
    if (!data || typeof data !== 'object' || Array.isArray(data)) {
      throw new PasskeyRequestError(fallback);
    }
    return {response, data};
  };

  const errorMessage = (error, mode) => {
    if (error instanceof PasskeyRequestError) return error.message;
    if (error && (error.name === 'NotAllowedError' || error.name === 'AbortError')) {
      return 'The passkey request was cancelled or timed out. Try again when you are ready.';
    }
    if (error && error.name === 'SecurityError') {
      return 'Passkeys are not available at this address. Use another sign-in method.';
    }
    if (mode === 'register' && error && error.name === 'InvalidStateError') {
      return 'That passkey is already registered. Choose another passkey or device.';
    }
    return mode === 'register'
      ? 'Could not add your passkey. Please try again.'
      : 'Could not sign in with your passkey. Try again or use another sign-in method.';
  };

  const show = (status, message, isError = false) => {
    if (!status) return;
    status.hidden = false;
    status.setAttribute('role', isError ? 'alert' : 'status');
    status.setAttribute('aria-live', isError ? 'assertive' : 'polite');
    status.classList.toggle('is-error', isError);
    status.textContent = message;
  };

  const perform = async (button, mode) => {
    if (button.disabled) return;
    const container = button.parentElement.parentElement;
    const status = document.getElementById(button.dataset.passkeyFeedback);
    if (!window.PublicKeyCredential || !navigator.credentials) {
      show(status, 'This browser does not support passkeys.', true);
      return;
    }
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    show(status, mode === 'register' ? 'Waiting for your passkey provider…' : 'Choose a passkey…');
    try {
      const next = button.dataset.next || '/dashboard';
      let challengeURL = '/auth/passkey/login/challenge?next=' + encodeURIComponent(next);
      let verifyURL = '/auth/passkey/login/verify';
      if (mode === 'register') {
        const nameInput = container.querySelector('[data-passkey-name]');
        const name = nameInput && nameInput.value ? nameInput.value : 'Passkey';
        challengeURL = '/auth/passkey/register/challenge?name=' + encodeURIComponent(name);
        verifyURL = '/auth/passkey/register/verify';
      }
      const unavailable = mode === 'register'
        ? 'Adding a passkey is temporarily unavailable. Please try again.'
        : 'Passkey sign-in is temporarily unavailable. Try again or use another sign-in method.';
      const {response: challengeResponse, data: challenge} = await fetchJSON(challengeURL,
        {credentials: 'same-origin', headers: {Accept: 'application/json'}}, unavailable);
      if (!challengeResponse.ok && typeof challenge.reauth_url === 'string' && challenge.reauth_url) {
        if (mode === 'register') {
          const nameInput = container.querySelector('[data-passkey-name]');
          try { sessionStorage.setItem('btcpp:resume-passkey-name', nameInput && nameInput.value ? nameInput.value : 'Passkey'); } catch (_) {}
        }
        window.location.assign(challenge.reauth_url);
        return;
      }
      if (!challengeResponse.ok) throw new PasskeyRequestError(typeof challenge.error === 'string' && challenge.error || unavailable);
      if (!challenge.publicKey || typeof challenge.publicKey.challenge !== 'string') {
        throw new PasskeyRequestError(unavailable);
      }
      const credential = mode === 'register'
        ? await navigator.credentials.create({publicKey: creationOptions(challenge.publicKey)})
        : await navigator.credentials.get({publicKey: requestOptions(challenge.publicKey)});
      if (!credential) throw new PasskeyRequestError('No passkey was selected. Try again or use another sign-in method.');
      show(status, 'Verifying passkey…');
      const {response: verifyResponse, data: result} = await fetchJSON(verifyURL, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {'Content-Type': 'application/json', Accept: 'application/json'},
        body: JSON.stringify(credentialJSON(credential)),
      }, 'Could not verify your passkey. Please try again.');
      if (!verifyResponse.ok) throw new PasskeyRequestError(typeof result.error === 'string' && result.error || 'Could not verify your passkey. Please try again.');
      window.location.assign(result.redirect || '/dashboard');
    } catch (error) {
      show(status, errorMessage(error, mode), true);
    } finally {
      button.disabled = false;
      button.removeAttribute('aria-busy');
    }
  };

  document.querySelectorAll('[data-passkey-login]').forEach((button) => {
    button.addEventListener('click', () => perform(button, 'login'));
  });
  document.querySelectorAll('[data-passkey-register]').forEach((button) => {
    button.addEventListener('click', () => perform(button, 'register'));
  });

  const location = new URL(window.location.href);
  if (location.searchParams.get('resume') === 'passkey-register') {
    const button = document.querySelector('[data-passkey-register]');
    if (button) {
      const container = button.parentElement.parentElement;
      const nameInput = container.querySelector('[data-passkey-name]');
      try {
        const savedName = sessionStorage.getItem('btcpp:resume-passkey-name');
        if (savedName && nameInput) nameInput.value = savedName;
        sessionStorage.removeItem('btcpp:resume-passkey-name');
      } catch (_) {}
      location.searchParams.delete('resume');
      window.history.replaceState({}, '', location.pathname + location.search + location.hash);
      queueMicrotask(() => perform(button, 'register'));
    }
  }
})();
