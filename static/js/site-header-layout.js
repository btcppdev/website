// Sticky rows must follow the rendered header, including wrapped text and menus.
(function () {
  var root = document.documentElement;
  var ticker = document.querySelector('[data-site-ticker]');
  var nav = document.querySelector('.site-nav');
  var apiBar = document.querySelector('.api-reference__bar');
  if (!ticker || !nav) return;

  function measure() {
    root.style.setProperty('--btcpp-site-ticker-height', ticker.getBoundingClientRect().height + 'px');
    root.style.setProperty('--btcpp-site-nav-height', nav.getBoundingClientRect().height + 'px');
    if (apiBar) root.style.setProperty('--btcpp-api-bar-height', apiBar.getBoundingClientRect().height + 'px');
  }

  measure();
  if (typeof ResizeObserver !== 'undefined') {
    var observer = new ResizeObserver(measure);
    observer.observe(ticker);
    observer.observe(nav);
    if (apiBar) observer.observe(apiBar);
  } else {
    window.addEventListener('resize', measure);
    nav.addEventListener('change', measure);
    if (document.fonts) document.fonts.ready.then(measure);
  }
})();
