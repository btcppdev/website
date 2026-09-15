document.querySelectorAll('.sponsor-prize-form').forEach((form) => {
  const type = form.elements.PrizeType;
  const title = form.elements.PrizeTitle;
  const amount = form.elements.PrizeValueText;
  const update = () => {
    const bitcoin = type.value === 'sats';
    form.querySelector('[data-prize-title]').hidden = bitcoin;
    title.disabled = bitcoin;
    title.required = !bitcoin;
    amount.required = bitcoin;
    form.querySelector('[data-prize-value-label]').textContent = bitcoin
      ? 'Amount per winner (sats)' : 'Estimated value per winner (sats, optional)';
    form.querySelector('[data-prize-value-help]').textContent = bitcoin
      ? 'Enter a whole number of sats, e.g. 1000000 for 1 million sats.'
      : 'Leave blank if you do not have a sats estimate for the physical prize.';
  };
  type.addEventListener('change', update);
  update();
});
