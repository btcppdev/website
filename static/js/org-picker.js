(function () {
  function initPicker(root) {
    const input = root.querySelector('[data-org-picker-input]');
    if (!input) return;

    const fieldName = root.dataset.orgPickerName || 'OrgID';
    let hidden = root.querySelector('[data-org-picker-hidden]');
    if (!hidden) {
      hidden = document.createElement('input');
      hidden.type = 'hidden';
      hidden.name = fieldName;
      hidden.setAttribute('data-org-picker-hidden', '');
      root.appendChild(hidden);
    }

    const flash = root.querySelector('[data-org-picker-flash]');
    const list = document.createElement('ul');
    list.className = 'absolute left-0 right-0 z-20 mt-1 hidden max-h-64 overflow-auto rounded-md border border-gray-200 bg-white shadow-lg';
    list.style.maxHeight = '16rem';
    list.style.overflowY = 'auto';
    list.style.overscrollBehavior = 'contain';
    list.setAttribute('role', 'listbox');
    root.appendChild(list);

    let debounceTimer = null;
    let activeFetch = 0;

    function isRequired() {
      return root.dataset.orgPickerRequired === 'true';
    }

    function hideList() {
      list.classList.add('hidden');
      list.replaceChildren();
    }

    function showFlash(message) {
      if (!flash) return;
      flash.textContent = message || '';
      flash.classList.toggle('hidden', !message);
    }

    function selectOrg(org) {
      input.value = org.name || '';
      hidden.value = org.id || '';
      hidden.dispatchEvent(new Event('change', { bubbles: true }));
      showFlash('');
      hideList();
    }

    function renderHits(hits) {
      list.replaceChildren();
      if (!hits || hits.length === 0) {
        hideList();
        return;
      }
      for (const org of hits) {
        const item = document.createElement('li');
        item.className = 'cursor-pointer px-3 py-2 text-sm hover:bg-indigo-50';
        item.setAttribute('role', 'option');

        const name = document.createElement('div');
        name.className = 'truncate font-medium text-gray-900';
        name.textContent = org.name || 'Unnamed organization';
        item.appendChild(name);

        if (org.website) {
          const website = document.createElement('div');
          website.className = 'mt-0.5 truncate text-xs text-gray-500';
          website.textContent = org.website;
          item.appendChild(website);
        }

        item.addEventListener('mousedown', (event) => {
          event.preventDefault();
          selectOrg(org);
        });
        list.appendChild(item);
      }
      list.classList.remove('hidden');
    }

    function searchOrgs(query) {
      const fetchID = ++activeFetch;
      fetch('/api/orgs/search?q=' + encodeURIComponent(query) + '&limit=50', { credentials: 'same-origin' })
        .then((response) => response.ok ? response.json() : [])
        .then((hits) => {
          if (fetchID !== activeFetch) return;
          renderHits(hits);
        })
        .catch(() => {});
    }

    input.addEventListener('input', () => {
      hidden.value = '';
      hidden.dispatchEvent(new Event('change', { bubbles: true }));
      showFlash('');
      if (debounceTimer) clearTimeout(debounceTimer);
      const query = input.value.trim();
      debounceTimer = setTimeout(() => searchOrgs(query), 200);
    });

    input.addEventListener('focus', () => {
      searchOrgs(input.value.trim());
    });

    input.addEventListener('blur', () => {
      setTimeout(hideList, 150);
    });

    const form = input.closest('form');
    if (form) {
      form.addEventListener('submit', (event) => {
        if (!isRequired() && input.value.trim() === '') return;
        if (hidden.value.trim() !== '') return;
        event.preventDefault();
        showFlash('Choose a sponsor organization from the search results before submitting.');
        input.focus();
      });
    }

    input.setAttribute('autocomplete', 'off');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-autocomplete', 'list');
  }

  document.querySelectorAll('[data-org-picker]').forEach(initPicker);
})();
