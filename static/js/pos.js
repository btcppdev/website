(() => {
 const body=document.body,base=body.dataset.posBase;
 const number=new Intl.NumberFormat();
 document.querySelectorAll("[data-format-sats]").forEach(e=>{e.textContent=number.format(Number(e.dataset.formatSats))+" sats"});
 function local(sats,rate=Number(body.dataset.posRate),currency=body.dataset.posCurrency){
  if(!rate||!currency)return 'Local estimate unavailable';
  try{return '≈ '+new Intl.NumberFormat(undefined,{style:'currency',currency}).format(sats*rate/1e8)}catch{return 'Local estimate unavailable'}
 }
 document.querySelectorAll('[data-local-sats]').forEach(e=>{e.textContent=local(Number(e.dataset.localSats),Number(e.dataset.rate||body.dataset.posRate),e.dataset.currency||body.dataset.posCurrency)});
 document.querySelectorAll('.stock-card input[data-pos-price]').forEach(e=>e.addEventListener('input',()=>{e.parentElement.querySelector('.local').textContent=local(Number(e.value))}));
 const cart=new Map(),form=document.querySelector('#checkout');
 const storageKey='pos-cart:'+base;
 let stored;try{stored=JSON.parse(sessionStorage.getItem(storageKey)||'null')}catch{}
 function update(){
  let total=0,count=0;cart.forEach(l=>{total+=l.price_sats*l.quantity;count+=l.quantity});
  document.querySelector('#cart-total').textContent=number.format(total)+' sats';
  document.querySelector('#cart-count').textContent=count+' item'+(count===1?'':'s');
  document.querySelector('#cart-local').textContent=local(total);
  document.querySelector('#cart-json').value=JSON.stringify([...cart.values()]);
  const charge=document.querySelector('#charge');charge.disabled=!count||Boolean(charge.dataset.unavailable)||total>500000000;
  try{sessionStorage.setItem(storageKey,JSON.stringify({request:form.elements.request_id.value,lines:[...cart.values()]}))}catch{}
 }
 if(form){
  if(stored?.request)form.elements.request_id.value=stored.request;
  document.querySelectorAll('[data-variant]').forEach(card=>{
   const id=card.dataset.variant,price=Number(card.dataset.price),max=Math.min(1000,Number(card.dataset.stock));
   let quantity=0;const old=stored?.lines?.find(l=>l.variant_id===id&&l.price_sats===price);if(old)quantity=Math.min(max,Math.max(0,old.quantity));
   function apply(){card.querySelector('output').textContent=quantity;card.querySelector('[data-delta="-1"]').disabled=quantity===0;const add=card.querySelector('[data-delta="1"]');if(!document.querySelector('#charge').dataset.unavailable)add.disabled=quantity>=max;if(quantity)cart.set(id,{variant_id:id,quantity,price_sats:price});else cart.delete(id);update()}
   card.querySelectorAll('[data-delta]').forEach(b=>b.addEventListener('click',()=>{quantity=Math.max(0,Math.min(max,quantity+Number(b.dataset.delta)));form.elements.request_id.value=crypto.randomUUID();apply()}));apply();
  });update();
  form.addEventListener('submit',()=>{const b=document.querySelector('#charge');b.disabled=true;b.textContent='Creating invoice…'});
 }
 const sale=document.querySelector('[data-sale]');if(!sale)return;
 try{sessionStorage.removeItem(storageKey)}catch{}
 document.querySelector('#copy-invoice')?.addEventListener('click',async e=>{try{await navigator.clipboard.writeText(e.currentTarget.dataset.invoice);e.target.textContent='Invoice copied'}catch{e.target.textContent='Unable to copy — scan QR instead'}});
 let status=sale.dataset.status;
 function expiry(){if(status!=='pending')return;const end=Date.parse(sale.dataset.expires);const seconds=Math.ceil((end-Date.now())/1000);if(seconds<=0){document.querySelector('#invoice').hidden=true;document.querySelector('#payment-message').textContent='Invoice time elapsed. Checking final payment status…'}else document.querySelector('#expiry').textContent=`Waiting for payment · ${Math.floor(seconds/60)}:${String(seconds%60).padStart(2,'0')} remaining`}
 expiry();const timer=setInterval(expiry,1000);
 async function poll(){
  if(!['pending','creating'].includes(status)){clearInterval(timer);return}
  try{
   const response=await fetch(base+'/status/'+sale.dataset.sale,{cache:'no-store'});
   if(response.status===401){location.href=base;return}
   if(!response.ok)throw Error();const data=await response.json();
   if(data.status!==status||data.handed_over_at){location.reload();return}
  }catch{document.querySelector('#payment-message').textContent='Connection interrupted. Do not charge again; we’re still checking this sale.'}
  setTimeout(poll,4000)
 }
 setTimeout(poll,2000);
})();
