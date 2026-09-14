(() => {
  'use strict';
  document.querySelectorAll('[data-copy-command]').forEach((button) => {
    button.addEventListener('click', async () => {
      const code = document.getElementById(button.dataset.copyCommand);
      const status = button.closest('details').querySelector('[role="status"]');
      if (!code) return;
      try {
        await navigator.clipboard.writeText(code.textContent);
        status.textContent = 'Command copied. Run it on your CLN node.';
      } catch (_) {
        const selection = window.getSelection();
        const range = document.createRange();
        range.selectNodeContents(code);
        selection.removeAllRanges();
        selection.addRange(range);
        status.textContent = 'Command selected. Copy it, then run it on your CLN node.';
      }
    });
  });
})();
