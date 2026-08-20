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

  const show = (status, message, isError = false) => {
    if (!status) return;
    status.textContent = message;
    status.classList.toggle('text-red-700', isError);
    status.classList.toggle('text-gray-500', !isError);
  };

  const perform = async (button, mode) => {
    const container = button.parentElement.parentElement;
    const status = container.querySelector('[data-passkey-status]');
    if (!window.PublicKeyCredential || !navigator.credentials) {
      show(status, 'This browser does not support passkeys.', true);
      return;
    }
    button.disabled = true;
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
      const challengeResponse = await fetch(challengeURL, {credentials: 'same-origin', headers: {Accept: 'application/json'}});
      const challenge = await challengeResponse.json();
      if (!challengeResponse.ok) throw new Error(challenge.error || 'Unable to start the passkey request.');
      const credential = mode === 'register'
        ? await navigator.credentials.create({publicKey: creationOptions(challenge.publicKey)})
        : await navigator.credentials.get({publicKey: requestOptions(challenge.publicKey)});
      if (!credential) throw new Error('No passkey was selected.');
      show(status, 'Verifying passkey…');
      const verifyResponse = await fetch(verifyURL, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {'Content-Type': 'application/json', Accept: 'application/json'},
        body: JSON.stringify(credentialJSON(credential)),
      });
      const result = await verifyResponse.json();
      if (!verifyResponse.ok) throw new Error(result.error || 'Passkey verification failed.');
      window.location.assign(result.redirect || '/dashboard');
    } catch (error) {
      show(status, error instanceof Error ? error.message : 'Passkey request failed.', true);
      button.disabled = false;
    }
  };

  document.querySelectorAll('[data-passkey-login]').forEach((button) => {
    button.addEventListener('click', () => perform(button, 'login'));
  });
  document.querySelectorAll('[data-passkey-register]').forEach((button) => {
    button.addEventListener('click', () => perform(button, 'register'));
  });
})();
