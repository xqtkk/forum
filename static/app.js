document.querySelectorAll('[data-password-toggle]').forEach(button => {
 button.addEventListener('click', () => {
  const input = document.getElementById(button.dataset.passwordToggle);
  const show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  button.textContent = show ? 'Hide' : 'Show';
  button.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
  button.setAttribute('aria-pressed', String(show));
 });
});
const error = document.querySelector('.form-error');
if (error) error.focus({preventScroll: true});
document.querySelectorAll('.site-nav a').forEach(link => {
 if (link.pathname === location.pathname) link.setAttribute('aria-current', 'page');
});
const notificationBadge=document.querySelector('[data-notification-count]');
if(notificationBadge){
 const notificationLink=document.querySelector('[data-notification-link]');
 const refreshNotifications=async()=>{
  if(document.hidden)return;
  try{const response=await fetch('/notifications/count',{headers:{Accept:'application/json'},credentials:'same-origin'});if(!response.ok)return;const data=await response.json();const count=Number(data.count)||0;notificationBadge.hidden=count===0;notificationBadge.textContent=count>99?'99+':String(count);if(notificationLink){notificationLink.classList.toggle('has-unread',count>0);notificationLink.setAttribute('aria-label',count?`Notifications, ${count} unread`:'Notifications');}document.querySelector('.account-menu summary')?.setAttribute('aria-label',count?`Account menu, ${count} unread notifications`:'Account menu');}catch{}
 };
 refreshNotifications();window.setInterval(()=>{if(!document.hidden)refreshNotifications();},15000);document.addEventListener('visibilitychange',()=>{if(!document.hidden)refreshNotifications();});
}
document.querySelectorAll('form').forEach(form => {
 if (new URL(form.action).searchParams.get('action') === 'delete') {
  form.dataset.confirm = 'Delete this item? This cannot be undone.';
 }
});


// Reaction forms retain their server fallback; JS avoids a document navigation.
document.querySelectorAll('[data-reactions] form').forEach(form => {
 const action = new URL(form.action).searchParams.get('action');
 if (!['like','dislike'].includes(action)) return;
 form.addEventListener('submit', async event => {
  event.preventDefault();
  const group = form.closest('[data-reactions]');
  if (group.dataset.pending) return;
  group.dataset.pending = 'true';
  const buttons = [...group.querySelectorAll('form')].filter(f => ['like','dislike'].includes(new URL(f.action).searchParams.get('action'))).map(f=>f.querySelector('button'));
  buttons.forEach(button=>button.disabled=true);
  let status = group.querySelector('[data-reaction-status]');
  if (!status) {status=document.createElement('span');status.dataset.reactionStatus='';status.className='reaction-status';status.setAttribute('role','status');group.append(status);}
  status.textContent='';
  try {
   const response = await fetch(form.action,{method:'POST',body:new URLSearchParams(new FormData(form)),headers:{Accept:'application/json'},credentials:'same-origin'});
   if (!response.ok) throw new Error(response.status===429?'Please wait before reacting again.':response.status===401?'Your session expired. Sign in again to react.':'Unable to save reaction. Please retry.');
   const state = await response.json();
   buttons.forEach(button=>{const kind=new URL(button.form.action).searchParams.get('action');button.textContent=(kind==='like'?'Like (':'Dislike (')+(kind==='like'?state.likes:state.dislikes)+')';button.setAttribute('aria-pressed',String(state.reaction===kind));});
  } catch(error) {status.textContent=error.message;}
  finally {delete group.dataset.pending;buttons.forEach(button=>button.disabled=false);}
 });
});
const confirmation = document.createElement('dialog');
confirmation.className='confirmation-dialog';confirmation.setAttribute('aria-labelledby','confirmation-title');
confirmation.innerHTML='<h2 id="confirmation-title">Confirm action</h2><p data-confirmation-message></p><div class="editor-actions"><button type="button" data-confirmation-cancel>Cancel</button><button type="button" class="primary" data-confirmation-accept>Confirm</button></div>';
document.body.append(confirmation);
let pendingConfirmation;
confirmation.querySelector('[data-confirmation-cancel]').addEventListener('click',()=>{pendingConfirmation=null;confirmation.close();});
confirmation.addEventListener('cancel',()=>{pendingConfirmation=null;});
confirmation.querySelector('[data-confirmation-accept]').addEventListener('click',()=>{const pending=pendingConfirmation;pendingConfirmation=null;confirmation.close();if(pending){pending.form.dataset.confirmed='yes';pending.form.requestSubmit(pending.submitter);}});
document.querySelectorAll('[data-confirm]').forEach(form=>form.addEventListener('submit',event=>{
 if(form.dataset.confirmed==='yes'){delete form.dataset.confirmed;return;}
 event.preventDefault();pendingConfirmation={form,submitter:event.submitter};confirmation.querySelector('[data-confirmation-message]').textContent=form.dataset.confirm;confirmation.showModal();
}));
document.querySelectorAll('[data-cancel-editor]').forEach(button=>button.addEventListener('click',()=>{const editor=button.closest('details');editor.querySelector('form').reset();editor.open=false;}));
document.querySelectorAll('[data-image-input]').forEach(input=>{
 const form=input.form,preview=form.querySelector('[data-image-preview]'),image=form.querySelector('[data-preview-image]'),name=form.querySelector('[data-preview-name]'),error=form.querySelector('[data-image-error]');
 let objectURL;
 const clear=()=>{if(objectURL)URL.revokeObjectURL(objectURL);objectURL=null;input.value='';image.removeAttribute('src');preview.hidden=true;name.textContent='';error.textContent='';};
 input.addEventListener('change',()=>{
  if(objectURL)URL.revokeObjectURL(objectURL);preview.hidden=true;error.textContent='';
  const file=input.files[0];if(!file)return;
  if(file.size>20*1024*1024||!['image/jpeg','image/png','image/gif'].includes(file.type)){clear();error.textContent='Choose a PNG, JPEG, or GIF image of at most 20 MiB.';return;}
  objectURL=URL.createObjectURL(file);image.src=objectURL;name.textContent=file.name+' · '+(file.size/1024).toFixed(0)+' KB';preview.hidden=false;
 });
 form.querySelector('[data-clear-image]').addEventListener('click',clear);
 image.addEventListener('error',()=>{clear();error.textContent='This file could not be previewed. Choose a valid image.';});
});
// Non-JS-style POST redirects, including updates, restore the reader's position.
const scrollKey='forum-scroll:'+location.pathname+location.search;
try {const saved=sessionStorage.getItem(scrollKey);if(location.hash){sessionStorage.removeItem(scrollKey);}else if(saved!==null){sessionStorage.removeItem(scrollKey);requestAnimationFrame(()=>window.scrollTo(0,Number(saved)));}}catch{}
// Keep the profile activity tabs at the reader's current position across navigation.
const profileTabScrollKey='forum-profile-tab-scroll';
if(location.pathname==='/profile'){
 try{const saved=sessionStorage.getItem(profileTabScrollKey);if(saved!==null){sessionStorage.removeItem(profileTabScrollKey);requestAnimationFrame(()=>window.scrollTo(0,Number(saved)));}}catch{}
 document.querySelectorAll('.profile-tabs a').forEach(link=>link.addEventListener('click',()=>{try{sessionStorage.setItem(profileTabScrollKey,String(window.scrollY));}catch{}}));
}
document.querySelectorAll('form').forEach(form=>form.addEventListener('submit',event=>{if(!event.defaultPrevented&&form.method.toLowerCase()==='post'){try{sessionStorage.setItem(scrollKey,String(window.scrollY));}catch{}}}));
