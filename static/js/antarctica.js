(() => {
  const choice = document.getElementById('berth-choice');
  const dollars = cents => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);
  if (choice) {
    const update = () => {
      const option = choice.selectedOptions[0];
      document.getElementById('full-standard-price').textContent = dollars(Number(option.dataset.priceCents));
      document.getElementById('full-savings').textContent = option.dataset.payInFullSavings;
      document.getElementById('full-payment-amount').textContent = option.dataset.payInFull;
      document.getElementById('deposit-amount').textContent = option.dataset.deposit;
      const cents = Number(option.dataset.priceCents);
      document.querySelectorAll('[data-final-installment]').forEach(el => { el.textContent = dollars(Math.floor(cents / 5) + cents % 5); });
      document.querySelectorAll('[data-installment-amount]').forEach(el => { el.textContent = option.dataset.deposit; });
    };
    choice.addEventListener('change', update);
    document.querySelectorAll('[data-berth-select]').forEach(link => link.addEventListener('click', () => {
      choice.value = link.dataset.berthSelect;
      update();
    }));
    update();
  }
})();

(() => {
  const section = document.querySelector('.southbound');
  if (!section) return;
  const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
  const room = window.matchMedia('(min-width: 701px) and (min-height: 850px)');
  const route = section.querySelector('.southbound-route');
  const ship = section.querySelector('.southbound-ship');
  const arrival = section.querySelector('.southbound-arrival');
  const hint = section.querySelector('.southbound-scroll');
  const length = route.getTotalLength();
  let scheduled = false;
  const render = () => {
    scheduled = false;
    const animated = !motion.matches && room.matches;
    section.classList.toggle('is-animated', animated);
    const distance = section.offsetHeight - window.innerHeight;
    const top = section.getBoundingClientRect().top;
    const progress = motion.matches ? 1 : Math.max(0, Math.min(1, animated ? -top / Math.max(1, distance) : (window.innerHeight - top) / section.offsetHeight));
    const crossing = Math.min(1, progress / .65);
    route.style.strokeDashoffset = String(1 - crossing);
    const point = route.getPointAtLength(length * crossing);
    ship.setAttribute('transform', `translate(${point.x} ${point.y})`);
    // Keep the link visible and keyboard-accessible throughout the journey.
    arrival.style.transform = animated ? `translateY(${(1 - progress) * 22}px)` : 'none';
    hint.style.opacity = String(1 - progress);
  };
  const schedule = () => { if (!scheduled) { scheduled = true; requestAnimationFrame(render); } };
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule, { passive: true });
  motion.addEventListener('change', schedule);
  render();
})();
