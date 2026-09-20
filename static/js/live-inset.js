(() => {
  if (window.btcppLiveInsetLoaded) return;
  window.btcppLiveInsetLoaded = true;
  const inset = document.createElement('aside');
  inset.className = 'live-inset';
  inset.setAttribute('aria-label', 'bitcoin++ live video');
  inset.hidden = true;
  inset.innerHTML = `<div class="live-inset__bar"><span class="live-inset__badge">LIVE NOW</span><button type="button" data-close aria-label="Close live video">×</button></div>
    <video autoplay muted playsinline controls aria-label="Live broadcast"></video>
    <div class="live-inset__bar"><button type="button" class="live-inset__sound" data-sound>Turn sound on</button></div>
    <a class="live-inset__title" data-watch><strong></strong><span>OPEN LIVE PAGE ↗</span></a>`;
  document.body.appendChild(inset);
  const reopen = document.createElement('button');
  reopen.type = 'button';
  reopen.className = 'live-inset-reopen';
  reopen.textContent = '● Watch live';
  reopen.setAttribute('aria-label', 'Reopen live video');
  reopen.hidden = true;
  document.body.appendChild(reopen);
  const video = inset.querySelector('video');
  const link = inset.querySelector('[data-watch]');
  const sound = inset.querySelector('[data-sound]');
  let currentKey = '', dismissed = '', hls = null, generation = 0, hlsLoading, latestStatus;
  try { dismissed = sessionStorage.getItem('btcpp:live-dismissed') || ''; } catch (_) {}
  function stop() {
    generation++;
    if (hls) { hls.destroy(); hls = null; }
    video.pause();
    video.removeAttribute('src');
    video.load();
    inset.hidden = true;
    reopen.hidden = true;
    currentKey = '';
  }
  function loadHLS() {
    if (window.Hls) return Promise.resolve(window.Hls);
    if (!hlsLoading) hlsLoading = new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = 'https://cdn.jsdelivr.net/npm/hls.js@1';
      script.onload = () => resolve(window.Hls);
      script.onerror = () => { hlsLoading = null; script.remove(); reject(new Error('HLS unavailable')); };
      document.head.appendChild(script);
    });
    return hlsLoading;
  }
  async function play() {
    try { await video.play(); } catch (_) { sound.textContent = 'Play live video'; }
  }
  sound.addEventListener('click', () => { video.muted = !video.muted; play(); });
  video.addEventListener('volumechange', () => { sound.textContent = video.muted ? 'Turn sound on' : 'Mute'; });
  inset.querySelector('[data-close]').addEventListener('click', () => {
    dismissed = currentKey;
    try { sessionStorage.setItem('btcpp:live-dismissed', dismissed); } catch (_) {}
    stop();
    reopen.hidden = false;
    reopen.focus();
  });
  reopen.addEventListener('click', async () => {
    dismissed = '';
    try { sessionStorage.removeItem('btcpp:live-dismissed'); } catch (_) {}
    if (latestStatus) await render(latestStatus);
    if (!inset.hidden) inset.querySelector('[data-close]').focus();
    refresh();
  });
  async function render(status) {
    latestStatus = status;
    let watch, source;
    try {
      watch = new URL(status.watch_url, location.origin);
      source = new URL(status.hls_url);
    } catch (_) { stop(); return; }
    if (!status.live || !status.watch_url || !status.hls_url || watch.origin !== location.origin || !['http:', 'https:'].includes(source.protocol)
        || location.pathname.replace(/\/$/, '') === watch.pathname.replace(/\/$/, '') || document.getElementById('watch-live-video')) { stop(); return; }
    const key = `${watch.pathname}|${source.href}|${status.started_at || ''}`;
    if (key === dismissed) { stop(); reopen.hidden = false; return; }
    link.href = watch.pathname;
    link.querySelector('strong').textContent = status.title || 'bitcoin++ live';
    if (currentKey === key) return;
    stop();
    currentKey = key;
    const version = generation;
    inset.hidden = false;
    video.muted = true;
    sound.textContent = 'Turn sound on';
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = source.href;
      play();
    } else {
      try {
        const Hls = await loadHLS();
        if (version !== generation) return;
        if (!Hls || !Hls.isSupported()) { sound.textContent = 'Open live page to watch'; return; }
        hls = new Hls({lowLatencyMode: true});
        hls.loadSource(source.href);
        hls.attachMedia(video);
        hls.on(Hls.Events.MANIFEST_PARSED, play);
        hls.on(Hls.Events.ERROR, (_, data) => {
          if (data.fatal) { stop(); } // Next status refresh can reconnect.
        });
      } catch (_) { if (version === generation) stop(); }
    }
  }
  let refreshing = false;
  async function refresh() {
    if (document.hidden || refreshing) return;
    refreshing = true;
    try {
      const response = await fetch('/live/status', {credentials: 'omit', cache: 'no-store', signal: AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error('Live status unavailable');
      await render(await response.json());
    } catch (_) { await render({live: false}); }
    finally { refreshing = false; }
  }
  refresh();
  window.setInterval(refresh, 20000);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); });
  window.addEventListener('pagehide', stop);
  window.addEventListener('pageshow', event => { if (event.persisted) refresh(); });
})();
