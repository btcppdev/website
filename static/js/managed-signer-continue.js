document.addEventListener('DOMContentLoaded', () => {
  const form = document.querySelector('[data-managed-signer-continue]');
  if (form instanceof HTMLFormElement) form.requestSubmit();
});
