(function () {
  const clock = document.querySelector('[data-timezone]');
  if (!clock) return;
  const time = clock.querySelector('time');
  const zone = clock.querySelector('.clock__zone');
  let formatter;
  function setTimezone(name) {
    // h23 is explicitly 00–23, avoiding a client locale's 24:00 midnight.
    const next = new Intl.DateTimeFormat('en-GB', {
      timeZone: name, hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
    });
    formatter = next;
    clock.dataset.timezone = name;
    zone.textContent = name;
  }
  function render() {
    const now = new Date();
    time.textContent = formatter.format(now);
    time.dateTime = now.toISOString();
  }
  setTimezone(clock.dataset.timezone);
  function tick() {
    render();
    window.setTimeout(tick, 1000 - Date.now() % 1000);
  }
  tick();
  // Follow the next event automatically during long-running broadcasts.
  window.setInterval(async function () {
    try {
      const response = await fetch('/widgets/clock/status', {cache:'no-store', credentials:'omit'});
      if (!response.ok) return;
      const data = await response.json();
      if (data.timezone && data.timezone !== clock.dataset.timezone) {
        setTimezone(data.timezone);
        render();
      }
    } catch (_) {} // Keep the last known conference timezone during outages.
  }, 60000);
  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) render();
  });
})();
