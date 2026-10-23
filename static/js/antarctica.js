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
