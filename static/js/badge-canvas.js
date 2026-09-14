(() => {
  const selector = document.querySelector('[data-canvas-selector]');
  const preview = document.querySelector('[data-canvas-preview]');
  if (!selector || !preview) return;
  selector.addEventListener('change', () => {
    const option = selector.selectedOptions[0];
    const artwork = option && option.dataset.artwork;
    if (!artwork || !/^https?:\/\//i.test(artwork)) return;
    preview.src = artwork;
    preview.alt = `${option.textContent} canvas preview`;
  });
})();
