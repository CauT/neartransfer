'use strict';
const $ = id => document.getElementById(id);
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has('token')) {
  sessionStorage.setItem('near-token', fragment.get('token'));
  sessionStorage.setItem('near-admin', fragment.get('admin') || '');
}
const token = sessionStorage.getItem('near-token') || '';
const admin = sessionStorage.getItem('near-admin') || '';
history.replaceState(null, '', location.pathname);
let currentUpload = null, cancelled = false, busy = false, polling = false, itemsKey = '', previewURL = null;
const headers = {Authorization: `Bearer ${token}`};
const setNotice = text => { $('notice').textContent = text; $('notice').hidden = !text; };
function status(text, online) { $('status').textContent = text; $('status').classList.toggle('offline', !online); }
async function api(path, options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 15000);
  try {
    const response = await fetch(path, {...options, headers: {...headers, ...options.headers}, signal: controller.signal});
    if (!response.ok) throw new Error(await response.text());
    // Consume JSON within the same timeout, so stalled bodies are bounded too.
    return await response.json();
  } finally { clearTimeout(timer); }
}
function size(n) { return n < 1024 ? `${n} B` : n < 1048576 ? `${(n/1024).toFixed(1)} KB` : n < 1073741824 ? `${(n/1048576).toFixed(1)} MB` : `${(n/1073741824).toFixed(1)} GB`; }
function el(tag, className, text) { const n = document.createElement(tag); n.className = className; if (text !== undefined) n.textContent = text; return n; }
function button(text, action) { const b = el('button','secondary',text); b.onclick = async () => {b.disabled = true;try {await action();} catch (e) {setNotice(e.message || '操作失败，请重试');} finally {b.disabled = false;}};return b; }
function render(items) {
  const key = JSON.stringify(items);
  if (key === itemsKey) return;
  itemsKey = key;
  $('items').replaceChildren(); $('empty').hidden = items.length > 0; $('total').textContent = items.length;
  for (const item of [...items].reverse()) {
    const text = item.mime === 'text/plain';
    const image = ['image/png','image/jpeg','image/gif','image/webp','image/avif','image/bmp'].includes(item.mime);
    const row = el('article','item');
    row.append(el('div','item-icon',text ? '≡' : image ? '▧' : '↧'));
    const info = el('div','item-info');info.append(el('div','item-name',item.name));
    info.append(el('div','item-meta',`${item.origin === 'mac' ? 'Mac' : '浏览器'} · ${size(item.size)} · ${new Date(item.created).toLocaleString()}`));
    row.append(info);
    const actions = el('div','item-actions');
    if ((text && item.size <= 1024*1024) || (image && item.size <= 20*1024*1024)) actions.append(button(text ? '查看文字' : '预览',() => preview(item,text)));
    const form = document.createElement('form');form.method = 'POST';form.action = `/download/${item.id}`;
    const credential = document.createElement('input');credential.type = 'hidden';credential.name = 'token';credential.value = token;
    const save = el('button','secondary','保存');save.type = 'submit';form.append(credential,save);actions.append(form);
    row.append(actions);$('items').append(row);
  }
}
async function refresh() {
  if (polling || !token) return;
  polling = true;
  try {render(await api('/items'));status('● 已连接 · 每 3 秒同步',true);}
  catch (e) {status('连接中断 · 正在重试',false);setNotice(`连接失败：${e.name === 'AbortError' ? '请求超时' : e.message}。请确认 Mac 上的近传仍在运行；重启后需重新扫码。`);}
  finally {polling = false;}
}
async function preview(item, text) {
  const response = await fetch(`/items/${item.id}`,{headers,signal:AbortSignal.timeout(20000)});
  if (!response.ok) throw new Error(await response.text());
  $('previewTitle').textContent = item.name;$('previewBody').replaceChildren();
  if (text) {
    const content = await response.text();$('previewBody').append(el('pre','',content));
    $('previewBody').append(button('复制文字',() => copy(content)));
  } else {
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = URL.createObjectURL(await response.blob());
    const img = el('img','');img.src = previewURL;img.alt = item.name;$('previewBody').append(img);
  }
  $('preview').showModal();
}
async function copy(text) {
  if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
  else {const field = document.createElement('textarea');field.value = text;field.style.position = 'fixed';field.style.top = '0';const container = $('preview').open ? $('preview') : document.body;container.append(field);field.select();const ok = document.execCommand('copy');field.remove();if (!ok) throw new Error('浏览器不允许自动复制，请长按文字手动复制');}
  setNotice('已复制');
}
function upload(blob,name,mime) {
  return new Promise((resolve,reject) => {
    const xhr = new XMLHttpRequest();currentUpload = xhr;
    xhr.open('POST','/items');xhr.timeout = 30*60*1000;
    xhr.setRequestHeader('Authorization',headers.Authorization);
    xhr.setRequestHeader('X-File-Name',encodeURIComponent(name));
    xhr.setRequestHeader('X-Origin',admin ? 'mac' : 'browser');
    xhr.setRequestHeader('Content-Type',mime || 'application/octet-stream');
    $('progress').value = 0;$('progressText').textContent = `发送 ${name}`;
    xhr.upload.onprogress = e => {if(e.lengthComputable) {$('progress').value = e.loaded/e.total*100;$('progressText').textContent = `${name} · ${Math.round(e.loaded/e.total*100)}%${e.loaded === e.total ? ' · Mac 正在保存…' : ''}`;}};
    xhr.onload = () => xhr.status === 201 ? resolve() : reject(new Error(xhr.responseText || '发送失败'));
    xhr.onerror = () => reject(new Error('连接已断开，请确认 Mac 仍在运行后重试'));
    xhr.ontimeout = () => reject(new Error('传输超时，请重试'));
    xhr.onabort = () => reject(new Error('已取消；取消前已完成的文件仍保留。若服务器刚好完成保存，请先检查收件箱。'));
    xhr.send(blob);
  });
}
async function send(entries, afterSuccess) {
  if (busy || !entries.length || !token) return;
  busy = true;cancelled = false;setNotice('');$('transfer').hidden = false;
  for (const id of ['sendText','files','photos','choosePhotos']) $(id).disabled = true;
  let completed = 0;
  try {
    for (const entry of entries) {
      if (cancelled) break;
      if (entry.blob.size > 1073741824) throw new Error(`${entry.name} 超过单个文件 1 GiB 限制`);
      await upload(entry.blob,entry.name,entry.mime);completed++;await refresh();
    }
    if (!cancelled) {setNotice(`已发送 ${completed} 项，另一端将自动看到`);if(afterSuccess) afterSuccess();}
  } catch (e) {setNotice(`${completed ? `已完成 ${completed} 项。` : ''}${e.message}`);}
  finally {busy = false;currentUpload = null;$('transfer').hidden = true;for (const id of ['sendText','files','photos','choosePhotos']) $(id).disabled = false;await refresh();}
}
function sendFiles(files){return send(Array.from(files).map(file => ({blob:file,name:file.name,mime:file.type})));}
$('text').oninput = () => $('count').textContent = `${$('text').value.length.toLocaleString()} / 100,000`;
$('sendText').onclick = () => {const text = $('text').value;if(!text.trim()) return setNotice('先输入一段文字');const name = `文字-${new Date().toISOString().replace(/[:.]/g,'-')}.txt`;send([{blob:new Blob([text],{type:'text/plain;charset=utf-8'}),name,mime:'text/plain;charset=utf-8'}],() => {if($('text').value===text) {$('text').value='';$('text').oninput();}});};
for (const id of ['files','photos']) $(id).onchange = event => {sendFiles(event.target.files);event.target.value = '';};
$('choosePhotos').onclick = () => $('photos').click();
$('cancel').onclick = () => {cancelled=true;if(currentUpload) currentUpload.abort();};
$('drop').ondragover = event => {event.preventDefault();$('drop').classList.add('drag');};
$('drop').ondragleave = () => $('drop').classList.remove('drag');
$('drop').ondrop = event => {event.preventDefault();$('drop').classList.remove('drag');sendFiles(event.dataTransfer.files);};
$('refresh').onclick = () => {setNotice('');refresh();};
$('closePreview').onclick = () => $('preview').close();
$('preview').onclose = () => {if(previewURL) {URL.revokeObjectURL(previewURL);previewURL=null;}};
window.addEventListener('beforeunload',event => {if(busy){event.preventDefault();event.returnValue='';}});
document.addEventListener('visibilitychange',() => {if(!document.hidden) refresh();});
async function pairing() {
  if (!admin) {$('connected').hidden=false;return;}
  $('pairing').hidden=false;
  try {
    const peers = await api('/pairing',{headers:{'X-Admin':admin}});
    if (!peers.length) {$('qr').hidden=true;$('copyLink').disabled=true;setNotice('未检测到局域网 IPv4 地址。连接 Wi-Fi 后退出并重新打开近传。');return;}
    for (const [i,p] of peers.entries()) {const option = document.createElement('option');option.value=i;option.textContent=new URL(p.url).host;$('network').append(option);}
    const select = () => { const peer=peers[Number($('network').value)]; $('qr').src=peer.qr; $('connectionLink').value=peer.url; };
    $('network').onchange=select;select();$('copyLink').onclick=() => copy(peers[Number($('network').value)].url).catch(e=>setNotice(e.message));
  } catch(e) {setNotice(`二维码加载失败：${e.message}。请重新打开近传界面。`);}
}
if (!token) {status('尚未配对',false);setNotice('请扫描 Mac 上近传显示的二维码，或使用完整的连接链接打开本页。');for(const id of ['sendText','files','photos','choosePhotos']) $(id).disabled=true;}
else {pairing();refresh();setInterval(() => {if(!document.hidden) refresh();},3000);}
