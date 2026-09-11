(function () {
  if (window.btcppSponsorMarquee) {
    window.btcppSponsorMarquee.init(document);
    return;
  }
  const controllers = new WeakMap();

  function init(root) {
    root.querySelectorAll('.sponsor-banner__viewport').forEach(function (viewport) {
      if (controllers.has(viewport)) return;
      const track = viewport.querySelector('.sponsor-marquee-track');
      const original = viewport.querySelector('[data-sponsor-original]');
      const copy = viewport.querySelector('[data-sponsor-copy]');
      if (!track || !original || !copy) return;
      const items = Array.from(original.children);
      let frame = 0;
      let previous = '';
      let disposed = false;

      function fill() {
        frame = 0;
        if (disposed || !viewport.isConnected) return;
        const listWidth = items.reduce(function (width, item) {
          const style = getComputedStyle(item);
          return width + item.getBoundingClientRect().width +
            (parseFloat(style.marginLeft) || 0) + (parseFloat(style.marginRight) || 0);
        }, 0);
        if (!listWidth || !viewport.clientWidth) return;
        const repeats = Math.max(1, Math.ceil(viewport.clientWidth / listWidth));
        const signature = repeats + ':' + listWidth;
        if (signature === previous) return;
        previous = signature;
        original.querySelectorAll('[data-sponsor-fill]').forEach(node => node.remove());
        for (let i = 1; i < repeats; i++) {
          items.forEach(function (item) {
            const clone = item.cloneNode(true);
            clone.dataset.sponsorFill = '';
            clone.setAttribute('aria-hidden', 'true');
            clone.setAttribute('tabindex', '-1');
            original.appendChild(clone);
          });
        }
        copy.replaceChildren(...Array.from(original.children, item => item.cloneNode(true)));
        copy.querySelectorAll('a').forEach(link => link.setAttribute('tabindex', '-1'));
        // Keep approximately the same pixel speed at every viewport width.
        track.style.animationDuration = Math.max(12, listWidth * repeats / 40) + 's';
      }
      function schedule() {
        if (!disposed && !frame) frame = requestAnimationFrame(fill);
      }
      const observer = new ResizeObserver(schedule);
      observer.observe(viewport);
      const images = Array.from(original.querySelectorAll('img'));
      images.forEach(function (img) {
        img.addEventListener('load', schedule);
        img.addEventListener('error', schedule);
      });
      if (document.fonts) document.fonts.ready.then(schedule);
      controllers.set(viewport, function () {
        disposed = true;
        observer.disconnect();
        cancelAnimationFrame(frame);
        images.forEach(function (img) {
          img.removeEventListener('load', schedule);
          img.removeEventListener('error', schedule);
        });
      });
      schedule();
    });
  }
  function dispose(root) {
    root.querySelectorAll('.sponsor-banner__viewport').forEach(function (viewport) {
      controllers.get(viewport)?.();
      controllers.delete(viewport);
    });
  }
  window.btcppSponsorMarquee = {init, dispose};
  init(document);
})();
