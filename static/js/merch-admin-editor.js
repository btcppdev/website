(function () {
  "use strict";
  var editor = document.querySelector('[data-merch-editor]');
  if (!editor) return;
  var rate = Number(editor.dataset.btcUsd);
  var dirty = new Set();
  editor.querySelectorAll('form').forEach(function (form) {
    var notice = document.createElement('small');
    notice.className = 'merch-editor-save-status';
    notice.setAttribute('role', 'status');
    form.appendChild(notice);
    form.addEventListener('input', function () {
      dirty.add(form);
      notice.textContent = 'Unsaved changes in this section';
      var base = form.querySelector('[name="base_price"]');
      var currency = form.querySelector('[name="currency"]');
      var preview = form.querySelector('[data-merch-price-preview]');
      if (base && preview) {
        preview.textContent = rate > 0 && currency.value.toUpperCase() === 'USD' && base.value !== ''
          ? Math.round(Number(base.value) * 100000 / rate) + 'k sats on the storefront'
          : 'Satoshi price updates after saving';
      }
    });
    form.addEventListener('submit', function () { dirty.delete(form); notice.textContent = 'Saving…'; });
    form.addEventListener('invalid', function (event) {
      var node = event.target.parentElement;
      while (node && node !== editor) { if (node.tagName === 'DETAILS') node.open = true; node = node.parentElement; }
    }, true);
  });
  editor.querySelectorAll('.merch-editor-nav a').forEach(function (link) {
    link.addEventListener('click', function () {
      var target = document.querySelector(link.getAttribute('href'));
      if (target && target.tagName === 'DETAILS') target.open = true;
    });
  });
  var name = editor.querySelector('[name="name"]');
  if (name && location.pathname.endsWith('/new')) name.addEventListener('change', function () {
    var slug = name.value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    ['tag', 'slug'].forEach(function (field) { var input = editor.querySelector('[name="' + field + '"]'); if (input && !input.value) input.value = slug; });
  });
  window.addEventListener('beforeunload', function (event) { if (dirty.size) {event.preventDefault(); event.returnValue = '';} });
})();
