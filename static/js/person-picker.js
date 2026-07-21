(function () {
  function initPicker(root) {
    const input = root.querySelector('[data-person-picker-input]');
    const flash = root.querySelector('[data-person-picker-flash]');
    if (!input) return;

    const required = root.dataset.personPickerRequired === 'true';
    const fieldName = root.dataset.personPickerName || 'PersonID';
    const searchURL = root.dataset.personPickerSearchUrl || '/api/people/search';
    const selected = new Map();
    const chips = document.createElement('div');
    chips.className = 'mb-2 flex flex-wrap gap-2';
    root.insertBefore(chips, input);

    const list = document.createElement('ul');
    list.className = 'absolute left-0 right-0 z-20 mt-1 hidden max-h-64 overflow-auto rounded-md border border-gray-200 bg-white shadow-lg';
    // Keep long result sets usable even when the Tailwind build has not seen
    // the dynamically-created max-height class.
    list.style.maxHeight = '16rem';
    list.style.overflowY = 'auto';
    list.style.overscrollBehavior = 'contain';
    list.setAttribute('role', 'listbox');
    root.appendChild(list);

    let debounceTimer = null;
    let activeFetch = 0;

    function hideList() {
      list.classList.add('hidden');
      list.replaceChildren();
    }

    function renderMessage(message) {
      list.replaceChildren();
      const item = document.createElement('li');
      item.className = 'px-3 py-2 text-sm text-gray-500';
      item.textContent = message;
      list.appendChild(item);
      list.classList.remove('hidden');
    }

    function showFlash(message) {
      if (!flash) return;
      flash.textContent = message || '';
      flash.classList.toggle('hidden', !message);
    }

    function personLabel(person) {
      return person.name || person.maskedEmail || 'Unnamed person';
    }

    function personDetails(person) {
      return [person.maskedEmail, person.company].filter(Boolean).join(' · ');
    }

    function renderChips() {
      chips.replaceChildren();
      for (const person of selected.values()) {
        const chip = document.createElement('span');
        chip.className = 'inline-flex items-center gap-2 rounded-md bg-indigo-50 px-2.5 py-1.5 text-sm font-medium text-indigo-900 ring-1 ring-indigo-200';

        const label = document.createElement('span');
        label.textContent = personLabel(person);
        chip.appendChild(label);

        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'text-indigo-500 hover:text-indigo-900';
        button.setAttribute('aria-label', 'Remove ' + personLabel(person));
        button.textContent = 'x';
        button.addEventListener('click', () => {
          selected.delete(person.id);
          renderChips();
          showFlash('');
        });
        chip.appendChild(button);

        const hidden = document.createElement('input');
        hidden.type = 'hidden';
        hidden.name = fieldName;
        hidden.value = person.id;
        chip.appendChild(hidden);

        chips.appendChild(chip);
      }
      chips.classList.toggle('hidden', selected.size === 0);
    }

    function selectPerson(person) {
      selected.set(person.id, person);
      renderChips();
      input.value = '';
      showFlash('Added ' + personLabel(person) + '.');
      hideList();
    }

    function renderHits(hits) {
      list.replaceChildren();
      if (!hits || hits.length === 0) {
        renderMessage('No results');
        return;
      }
      for (const person of hits) {
        const item = document.createElement('li');
        item.className = 'flex cursor-pointer items-center gap-3 px-3 py-2 text-sm hover:bg-indigo-50';
        item.setAttribute('role', 'option');

        const photo = document.createElement('img');
        photo.className = 'h-9 w-9 flex-none rounded-full bg-gray-100 object-cover ring-1 ring-gray-200';
        photo.src = person.photoUrl;
        photo.alt = '';
        photo.loading = 'lazy';
        item.appendChild(photo);

        const copy = document.createElement('div');
        copy.className = 'min-w-0';

        const top = document.createElement('div');
        top.className = 'truncate font-medium text-gray-900';
        top.textContent = personLabel(person);
        copy.appendChild(top);

        const details = personDetails(person);
        if (details) {
          const sub = document.createElement('div');
          sub.className = 'mt-0.5 truncate text-xs text-gray-500';
          sub.textContent = details;
          copy.appendChild(sub);
        }
        item.appendChild(copy);

        item.addEventListener('mousedown', (event) => {
          event.preventDefault();
          selectPerson(person);
        });
        list.appendChild(item);
      }
      list.classList.remove('hidden');
    }

    function searchPeople(query) {
      const fetchID = ++activeFetch;
      const url = new URL(searchURL, window.location.origin);
      url.searchParams.set('q', query);
      fetch(url.toString(), { credentials: 'same-origin' })
        .then((response) => {
          if (response.status === 401 || response.status === 403) {
            return { error: 'permission' };
          }
          if (!response.ok) {
            return { error: 'search' };
          }
          return response.json();
        })
        .then((hits) => {
          if (fetchID !== activeFetch) return;
          if (hits && hits.error === 'permission') {
            renderMessage('No permission');
            return;
          }
          if (hits && hits.error === 'search') {
            renderMessage('Search failed');
            return;
          }
          renderHits(hits);
        })
        .catch(() => {
          if (fetchID !== activeFetch) return;
          renderMessage('Search failed');
        });
    }

    input.addEventListener('input', () => {
      showFlash('');
      if (debounceTimer) clearTimeout(debounceTimer);
      const query = input.value.trim();
      if (query.length < 3) {
        hideList();
        return;
      }
      debounceTimer = setTimeout(() => searchPeople(query), 200);
    });

    input.addEventListener('blur', () => {
      setTimeout(hideList, 150);
    });

    const form = input.closest('form');
    if (form) {
      form.addEventListener('submit', (event) => {
        if (selected.size > 0 && input.value.trim() === '') return;
        if (!required && selected.size === 0 && input.value.trim() === '') return;
        event.preventDefault();
        showFlash('Choose a person from the search results before submitting.');
        input.focus();
      });
    }

    input.setAttribute('autocomplete', 'off');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-autocomplete', 'list');
    renderChips();
  }

  document.querySelectorAll('[data-person-picker]').forEach(initPicker);
})();
