(() => {
  "use strict";
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  function celebrate() {
    if (reducedMotion.matches || document.querySelector('.community-pool-confetti')) return;
    const canvas = document.createElement('canvas');
    canvas.className = 'community-pool-confetti'; canvas.setAttribute('aria-hidden', 'true');
    document.body.append(canvas);
    const ctx = canvas.getContext('2d'); if (!ctx) { canvas.remove(); return; }
    const w = innerWidth, h = innerHeight, ratio = Math.min(devicePixelRatio || 1, 2);
    canvas.width = w * ratio; canvas.height = h * ratio; ctx.scale(ratio, ratio);
    const colors = ['#f9af5e', '#fa6a19', '#ffffff', '#111111'];
    const pieces = Array.from({length:90}, () => ({x:w/2,y:h*.65,vx:(Math.random()-.5)*16,vy:-5-Math.random()*12,c:colors[Math.floor(Math.random()*colors.length)],r:Math.random()*6}));
    const start = performance.now(); let previous=start;
    function draw(now) {
      if (now-start>2200 || document.hidden || reducedMotion.matches) {canvas.remove();return;}
      const dt=Math.min((now-previous)/16.67,2);previous=now;
      ctx.clearRect(0,0,w,h);
      pieces.forEach(p=>{p.x+=p.vx*dt;p.y+=p.vy*dt;p.vy+=.24*dt;p.r+=.06*dt;ctx.save();ctx.translate(p.x,p.y);ctx.rotate(p.r);ctx.fillStyle=p.c;ctx.fillRect(-4,-2,8,4);ctx.restore();});
      requestAnimationFrame(draw);
    }
    requestAnimationFrame(draw);
  }
  document.querySelectorAll('[data-community-pool]').forEach(pool => {
    if(pool.dataset.initialized) return; pool.dataset.initialized='true';
    let visible=false, busy=false, latest=Number(pool.dataset.milestone), celebrated=-1;
    const key='community-pool:'+pool.dataset.communityPool;
    try {celebrated=Number(sessionStorage.getItem(key) ?? -1);}catch (_) {}
    const burst=()=>{if(visible && latest>celebrated){celebrate();celebrated=latest;try{sessionStorage.setItem(key,String(latest));}catch(_){}}};
    const observer=new IntersectionObserver(entries=>{visible=entries[0].isIntersecting;if(visible)burst();reconcile();},{threshold:.25});observer.observe(pool);
    const dialog=pool.querySelector('dialog');
    const contribute=pool.querySelector('[data-contribute]');
    let oldOverflow='';
    if(dialog){
      contribute.addEventListener('click',()=>{oldOverflow=document.body.style.overflow;dialog.showModal();document.body.style.overflow='hidden';reconcile();});
      dialog.querySelector('[data-close-dialog]').addEventListener('click',()=>dialog.close());
      dialog.addEventListener('click',event=>{if(event.target!==dialog)return;const r=dialog.getBoundingClientRect();if(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom)dialog.close();});
      dialog.addEventListener('close',()=>{document.body.style.overflow=oldOverflow;reconcile();});
    }
    pool.querySelectorAll('[data-copy-address],[data-copy-bip353],[data-copy-offer]').forEach(button=>button.addEventListener('click',async()=>{
      const value=button.dataset.copyValue;
      try {
        await navigator.clipboard.writeText(value);
        dialog.querySelector('output').textContent=(button.hasAttribute('data-copy-address')?'Lightning address':button.hasAttribute('data-copy-bip353')?'BIP353 address':'BOLT12 offer')+' copied. Paste it into your wallet.';
      }catch(_){
        const text=button.hasAttribute('data-copy-offer')?dialog.querySelector('[data-offer-preview]'):button.querySelector('span');
        text.textContent=value;text.classList.add('is-selectable');
        const range=document.createRange();range.selectNodeContents(text);const selection=window.getSelection();selection.removeAllRanges();selection.addRange(range);
        dialog.querySelector('output').textContent='Copy unavailable. Select and copy the highlighted text.';
      }
    }));
    let source=null, healthy=false, retryTimer, watchdog, polling, lastEvent=0, revoked=false;
    let request=null, version=0, lastData=null, receivedAt=0, paused=false;
    const active=()=>!paused && !document.hidden && (visible || dialog?.open) && !revoked;
    const showStatus=()=>{
      if(!lastData)return;
      const age=Date.now()-receivedAt;
      const stale=lastData.stale || (Number.isFinite(lastData.syncedAt) && (lastData.syncedAt===0 || (lastData.serverTime-lastData.syncedAt)*1000+age>90000));
      pool.querySelector('[data-pool-status]').textContent=stale?'Updates delayed · showing confirmed funding':lastData.status==='open'?'Lightning only · confirmed contributions':'Funding closed · previously issued payments still tracked';
    };
    const apply=d=>{
      if(typeof d.sats!=='string'||!Number.isFinite(d.progress)||!Number.isFinite(d.milestone)||!Number.isFinite(d.count)||typeof d.goal!=='string'||!['open','closing','closed'].includes(d.status))throw new Error('invalid response');
      lastData=d;receivedAt=Date.now();version++;
      pool.querySelector('[data-pool-sats]').textContent=d.sats;
      pool.querySelector('[data-pool-goal]').textContent=d.goal+' sats';
      pool.querySelector('[data-pool-goal-label]').textContent=d.milestone>=10?'Goal reached':'Goal';
      pool.querySelector('progress').value=d.progress;
      pool.querySelector('[data-pool-count]').textContent=d.count+' contributions';
      showStatus();
      if(d.status!=='open'){if(dialog?.open)dialog.close();contribute?.remove();dialog?.remove();}
      latest=d.milestone;burst();
    };
    const refresh=async()=>{
      if(!active() || busy || healthy)return;busy=true;
      const current=version;const controller=new AbortController();request=controller;
      const timeout=setTimeout(()=>controller.abort(),8000);
      try{
        const response=await fetch(pool.dataset.statusUrl,{signal:controller.signal,cache:'no-store',headers:{Accept:'application/json'}});
        if(response.status===404 && active() && !healthy && version===current){revoked=true;if(dialog?.open)dialog.close();pool.hidden=true;stop();return;}
        if(!response.ok)throw new Error('unavailable');const d=await response.json();
        // A pending fallback response must never overwrite a newer SSE snapshot.
        if(active() && !healthy && version===current)apply(d);
      }catch(_){if(active()&&!healthy)pool.querySelector('[data-pool-status]').textContent='Updates delayed · showing confirmed funding';}
      finally{clearTimeout(timeout);busy=false;if(request===controller)request=null;}
    };
    const closeSource=()=>{if(source)source.close();source=null;healthy=false;};
    const retry=(delay)=>{clearTimeout(retryTimer);retryTimer=setTimeout(()=>{retryTimer=null;if(active())connect();},delay);};
    const fallback=()=>{closeSource();if(active()){refresh();retry(15000+Math.random()*3000);}};
    const connect=()=>{
      if(!active()||source)return;
      if(!window.EventSource || !pool.dataset.streamUrl){refresh();return;}
      lastEvent=Date.now();
      try{source=new EventSource(pool.dataset.streamUrl);}catch(_){fallback();return;}
      const connection=source;
      source.addEventListener('funding',event=>{
        if(source!==connection)return;
        try{apply(JSON.parse(event.data));healthy=true;lastEvent=Date.now();request?.abort();}catch(_){fallback();}
      });
      source.addEventListener('heartbeat',()=>{if(source===connection){lastEvent=Date.now();showStatus();}});
      source.addEventListener('reconnect',()=>{if(source===connection){closeSource();retry(500+Math.random()*1000);}});
      source.addEventListener('unavailable',()=>{if(source===connection)fallback();});
      source.addEventListener('revoked',()=>{
        if(source!==connection)return;revoked=true;
        if(dialog?.open)dialog.close();pool.hidden=true;stop();
      });
      source.onerror=()=>{if(source===connection)fallback();};
    };
    const stop=()=>{clearTimeout(retryTimer);retryTimer=null;clearInterval(polling);polling=null;clearInterval(watchdog);watchdog=null;closeSource();request?.abort();version++;};
    function reconcile(){
      if(!active()){stop();return;}
      if(!polling)polling=setInterval(()=>{showStatus();refresh();},20000);
      if(!watchdog)watchdog=setInterval(()=>{if(source && Date.now()-lastEvent>(healthy?40000:8000))fallback();},2000);
      if(!source&&!retryTimer)connect();
    }
    document.addEventListener('visibilitychange',reconcile);
    window.addEventListener('pagehide',()=>{paused=true;stop();});
    window.addEventListener('pageshow',()=>{paused=false;reconcile();});
  });
})();
