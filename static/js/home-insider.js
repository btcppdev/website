(() => {
  const preview = document.querySelector('[data-insider-preview]');
  if (!preview) return;
  const video = preview.querySelector('video');
  const button = preview.querySelector('[data-insider-play]');
  const label = button.querySelector('[data-insider-play-label]');
  const icon = button.querySelector('[data-insider-play-icon]');

  // Only fetch/play the silent excerpt after an explicit click.
  video.controls = false;
  button.hidden = false;
  const update = () => {
    label.textContent = video.paused ? 'Play preview' : 'Pause preview';
    icon.textContent = video.paused ? '▶' : 'Ⅱ';
    button.setAttribute('aria-label', video.paused ? 'Play silent News Hour preview' : 'Pause News Hour preview');
  };
  button.addEventListener('click', async () => {
    if (!video.paused) {
      video.pause();
      return;
    }
    try {
      await video.play();
    } catch {
      label.textContent = 'Retry preview';
    }
  });
  video.addEventListener('play', update);
  video.addEventListener('pause', update);
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) video.pause();
  });
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(([entry]) => {
      if (!entry.isIntersecting) video.pause();
    }).observe(video);
  }
  update();
})();
