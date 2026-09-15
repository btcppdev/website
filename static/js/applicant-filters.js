    // Proposal-card filter bar — hide cards whose
    // data-{status,placed,search} don't match the three inputs.
    // Pure DOM filter: no XHR, no server reload; cheap for the
    // applicants page's bounded proposal count.
    (function () {
      var q        = document.getElementById('filter-q');
      var status   = document.getElementById('filter-status');
      var placed   = document.getElementById('filter-placed');
      var talktype = document.getElementById('filter-talktype');
      var clear    = document.getElementById('filter-clear');
      var counter  = document.getElementById('filter-count');
      var quickFilters = Array.prototype.slice.call(document.querySelectorAll('.quick-filter-btn'));
      if (!q || !status || !placed) return;
      var cards = Array.prototype.slice.call(
        document.querySelectorAll('.proposal-card')
      );
      var total = cards.length;
      // Keep conference filters through saves, status changes, and navigation.
      // sessionStorage isolates this state to the current browser tab.
      var storageKey = 'btcpp:applicant-filters:' + window.location.pathname;
      var fields = {q: q, status: status, placed: placed, talktype: talktype};
      try {
        var saved = JSON.parse(sessionStorage.getItem(storageKey) || '{}');
        Object.keys(fields).forEach(function (key) {
          if (fields[key] && saved && typeof saved[key] === 'string') {
            fields[key].value = saved[key];
          }
        });
      } catch (_) { /* Filtering also works when browser storage is unavailable. */ }
      function remember() {
        var state = {};
        Object.keys(fields).forEach(function (key) {
          if (fields[key]) state[key] = fields[key].value;
        });
        try { sessionStorage.setItem(storageKey, JSON.stringify(state)); } catch (_) {}
      }

      function updateQuickFilters() {
        var active = placed.value === '1' ? 'scheduled' : (placed.value === '0' ? 'unscheduled' : 'all');
        quickFilters.forEach(function (btn) {
          var selected = btn.dataset.quickFilter === active;
          btn.classList.toggle('bg-gray-900', selected);
          btn.classList.toggle('text-white', selected);
          btn.classList.toggle('text-gray-700', !selected);
          btn.classList.toggle('hover:bg-gray-50', !selected);
        });
      }

      function apply() {
        var needle = (q.value || '').trim().toLowerCase();
        var st     = status.value;
        var pl     = placed.value;
        var tt     = talktype ? talktype.value : '';
        var shown = 0;
        for (var i = 0; i < cards.length; i++) {
          var c = cards[i];
          var matchSt = !st || c.dataset.status === st;
          var matchPl = !pl || c.dataset.placed === pl;
          var matchTT = !tt || c.dataset.talktype === tt;
          var matchQ  = !needle || (c.dataset.search || '').toLowerCase().indexOf(needle) !== -1;
          var visible = matchSt && matchPl && matchTT && matchQ;
          c.style.display = visible ? '' : 'none';
          if (visible) shown++;
        }
        if (counter) {
          counter.textContent = shown === total
            ? ''
            : '· ' + shown + ' of ' + total + ' shown';
        }
        updateQuickFilters();
        remember();
      }

      q.addEventListener('input', apply);
      status.addEventListener('change', apply);
      placed.addEventListener('change', apply);
      if (talktype) talktype.addEventListener('change', apply);
      if (clear) {
        clear.addEventListener('click', function () {
          q.value = '';
          status.value = '';
          placed.value = '';
          if (talktype) talktype.value = '';
          apply();
          q.focus();
        });
      }
      quickFilters.forEach(function (btn) {
        btn.addEventListener('click', function () {
          if (btn.dataset.quickFilter === 'scheduled') {
            placed.value = '1';
          } else if (btn.dataset.quickFilter === 'unscheduled') {
            placed.value = '0';
          } else {
            placed.value = '';
          }
          apply();
        });
      });
      apply();
    })();
