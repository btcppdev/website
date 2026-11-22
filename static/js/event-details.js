(function () {
  "use strict";
  var editor = document.querySelector('[data-event-details]');
  if (!editor) return;
  var baseline = new WeakMap();
  var dirty = new Set();
  var saving = false;
  var leaving = false;

  function values(form) {
    return new URLSearchParams(new FormData(form)).toString();
  }

  function notice(form, message, error) {
    var status = form.querySelector('[data-save-status]');
    if (!status) {
      status = document.createElement('p');
      status.dataset.saveStatus = '';
      status.setAttribute('role', 'status');
      form.prepend(status);
    }
    status.className = 'mb-3 text-sm ' + (error ? 'text-red-700' : 'text-gray-600');
    status.textContent = message;
    status.hidden = !message;
  }

  function initialize() {
    editor.querySelectorAll('form').forEach(function (form) {
      if (!baseline.has(form)) baseline.set(form, values(form));
    });
  }

  function changed(event) {
    var form = event.target.form;
    if (!form || !baseline.has(form)) return;
    if (values(form) !== baseline.get(form)) dirty.add(form);
    else dirty.delete(form);
    notice(form, dirty.has(form) ? 'Unsaved changes' : '');
  }

  async function refreshDays() {
    var response = await fetch(location.pathname, { credentials: 'same-origin', cache: 'no-store' });
    if (!response.ok || response.redirected) throw new Error('Could not refresh days');
    var documentCopy = new DOMParser().parseFromString(await response.text(), 'text/html');
    var next = documentCopy.querySelector('[data-event-days]');
    var current = editor.querySelector('[data-event-days]');
    if (!next || !current) throw new Error('Could not refresh days');
    // Reuse unsaved forms themselves so edits in another day survive too.
    next.querySelectorAll('[data-event-form]').forEach(function (form) {
      var original = Array.from(current.querySelectorAll('[data-event-form]')).find(function (candidate) {
        return candidate.dataset.eventForm === form.dataset.eventForm;
      });
      if (original && dirty.has(original)) form.replaceWith(original);
    });
    current.replaceWith(next);
    initialize();
  }

  editor.addEventListener('input', changed);
  editor.addEventListener('change', changed);
  editor.addEventListener('submit', async function (event) {
    var form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (saving) {
      event.preventDefault();
      return;
    }
    if (!form.hasAttribute('data-inline-save')) {
      // Other actions still navigate. Ask before their POST, so declining
      // cannot leave the user unsure whether the action already happened.
      var others = Array.from(dirty).some(function (candidate) { return candidate !== form; });
      if (others && !window.confirm('Other sections have unsaved changes. Leave and discard those changes?')) {
        event.preventDefault();
        return;
      }
      leaving = true;
      return;
    }

    event.preventDefault();
    saving = true;
    var body = values(form);
    var controls = Array.from(form.elements).filter(function (control) { return !control.disabled; });
    controls.forEach(function (control) { control.disabled = true; });
    form.setAttribute('aria-busy', 'true');
    notice(form, 'Saving…');
    try {
      var response = await fetch(form.action, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Accept': 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body
      });
      if (response.redirected || !(response.headers.get('Content-Type') || '').includes('application/json')) {
        throw new Error('Could not save. Your entries are still here. If your session expired, sign in in another tab and retry.');
      }
      var result = await response.json();
      if (!response.ok || result.ok !== true) throw new Error(result.message || 'Could not save. Please try again.');
      baseline.set(form, body);
      dirty.delete(form);
      notice(form, result.message);
      if (form.closest('[data-event-days]')) {
        try {
          await refreshDays();
          // The submitted form may now be replaced by the saved day/new-day form.
          var replacement = Array.from(editor.querySelectorAll('[data-event-form]')).find(function (candidate) {
            return candidate.dataset.eventForm === form.dataset.eventForm;
          });
          if (replacement) notice(replacement, result.message);
        } catch (_) {
          notice(form, result.message + ' Save your other edits before reloading to refresh the day list.');
        }
      }
    } catch (error) {
      var message = error instanceof TypeError || error instanceof SyntaxError
        ? 'Could not save. Your entries are still here. Check your connection and retry.'
        : error.message || 'Could not save. Your entries are still here.';
      notice(form, message, true);
    } finally {
      controls.forEach(function (control) { control.disabled = false; });
      form.removeAttribute('aria-busy');
      saving = false;
    }
  });

  window.addEventListener('beforeunload', function (event) {
    if (!leaving && (dirty.size || saving)) {
      event.preventDefault();
      event.returnValue = '';
    }
  });
  window.addEventListener('pageshow', function () { leaving = false; });
  initialize();
})();
