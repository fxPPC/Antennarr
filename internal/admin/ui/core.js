export const $ = s => document.querySelector(s);
export const el = (tag, text, cls) => {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = text;
  if (cls) n.className = cls;
  return n
};
export const date = t => t ? new Date(t * 1000).toLocaleString() : 'Not yet';
export const notice = s => {
  $('#notice').textContent = s || ''
};
let authorization = null;
export async function api(path, method = 'GET', body) {
  const headers = new Headers({
    'X-Antennarr-Request': '1'
  });
  if (authorization) headers.set('Authorization', authorization);
  let payload = body;
  if (body && !(body instanceof FormData)) {
    headers.set('Content-Type', 'application/json');
    payload = JSON.stringify(body)
  }
  let r;
  try {
    r = await fetch(path, {
      method,
      headers,
      body: payload
    })
  } catch {
    throw new Error('Cannot reach Antennarr. Check the saved values before retrying.')
  }
  if (!r.ok) {
    const text = await r.text();
    if (/<\/?(?:html|head|body|title|h1)\b/i.test(text) || text.length > 400) throw new Error(r.status === 504 ? 'The proxy timed out. Antennarr may still be working; retry shortly.' : `Request failed (${r.status}). Retry shortly.`);
    throw new Error(text.trim() || `Request failed (${r.status})`)
  }
  try {
    return await r.json()
  } catch {
    throw new Error('Antennarr returned an unexpected response. Retry shortly.')
  }
}
export const button = (text, fn) => {
  const b = el('button', text);
  b.type = 'button';
  b.onclick = async () => {
    b.disabled = true;
    try {
      await fn()
    } catch (e) {
      notice(e.message)
    } finally {
      b.disabled = false
    }
  };
  return b
};
// Shared plain-text help: hover, keyboard focus, or tap. One bubble stays
// outside cards so long explanations cannot be clipped by collapsed sections.
const helpText = {
  roster_target: 'Roster refill threshold for each channel. Lowering it keeps existing streams and their checks. Antennarr recruits only while the roster is below this threshold, and stops when it is reached.',
  startup_race: 'Race up to this many candidates at startup, within connection limits. Recently verified roster streams come first; queued candidates can be used if the roster is too small. The first validated feed plays; a second becomes the hot backup.',
  max_outgoing_streams: 'Maximum simultaneous outgoing viewer connections, not upstream feeds. Viewers of the same channel share one primary and backup pair. Lowering the limit keeps existing viewers; new connections are rejected until capacity is available.',
  global_connections: 'Total upstream connection budget for startup candidates, primary feeds, backups, and checks. Leave room for two feeds per active channel plus startup candidates.',
  provider_connections: 'Maximum connections to one provider hostname, across its accounts. Must not exceed the total upstream budget. Each account’s own connection limit still applies.',
  selection_preference: 'Stability favours similar replacement streams. Diversity favours as many different provider hostnames as possible. Both respect media health and connection limits.',
  preference: 'Override source selection for this channel only, or inherit the Output default.',
  target: 'Lowering this threshold keeps existing streams; recruitment resumes below it. Verified roster size for this channel only. Use 0 to inherit the Output default; otherwise use at least 2.',
  race: 'Startup candidate count for this channel only. Use 0 to inherit the Output default; otherwise use at least 2.',
  probe_seconds: 'Target duration of the media sample in a background check; connection and analysis can add time. Longer samples use more bandwidth. A successful short probe does not guarantee hours of playback.',
  check_api_seconds: 'Minimum spacing between background API requests to the same provider. Larger values reduce pressure on that provider. Must be at least the global API spacing.',
  check_global_seconds: 'Minimum spacing between background API requests across all providers. This is separate from live stream startup.',
  check_stream_seconds: 'Minimum spacing between background media probes on the same provider. Must be at least the global stream spacing. This does not pace live startup or backup recruitment.',
  check_stream_global_seconds: 'Minimum spacing between background media probe starts across all providers. Account budgets and playback priority still apply.',
  catalogue_hours: 'How long provider channel-list metadata may be reused before refresh. Verified roster streams are checked in continuous passes.',
  account_failures: 'Consecutive failed login or channel-discovery attempts before permanent deletion during idle cleanup. A complete successful discovery resets the count; a login alone does not hide catalogue failures. Use 0 to disable failure-based deletion. Unexamined accounts stay queued; examined accounts with no remaining channel matches are deleted.',
  stream_failures: 'Consecutive stream failures before permanent deletion during idle cleanup. A roster failure removes that roster place immediately, even when this threshold is higher. Use 0 to disable retirement. Examined accounts with no remaining channel matches are also deleted.'
};
let helpBubble, helpOwner, helpPinned = false, helpHideTimer;
function hideHelp() {
  clearTimeout(helpHideTimer);
  if (helpOwner) helpOwner.removeAttribute('aria-describedby');
  helpOwner = null;
  helpPinned = false;
  if (helpBubble) helpBubble.hidden = true
}
function showHelp(owner, text) {
  clearTimeout(helpHideTimer);
  if (!helpBubble) {
    helpBubble = el('div', undefined, 'help-tooltip');
    helpBubble.id = 'setting-help';
    helpBubble.setAttribute('role', 'tooltip');
    document.body.append(helpBubble);
    helpBubble.addEventListener('pointerenter', () => clearTimeout(helpHideTimer));
    helpBubble.addEventListener('pointerleave', () => { if (!helpPinned && document.activeElement !== helpOwner) hideHelp() });
    document.addEventListener('keydown', e => { if (e.key === 'Escape') hideHelp() });
    document.addEventListener('pointerdown', e => {
      if (e.target !== helpOwner && !helpBubble.contains(e.target)) hideHelp()
    });
    window.addEventListener('resize', hideHelp);
    window.addEventListener('scroll', hideHelp, true)
  }
  if (helpOwner && helpOwner !== owner) hideHelp();
  helpOwner = owner;
  helpBubble.textContent = text;
  helpBubble.hidden = false;
  owner.setAttribute('aria-describedby', helpBubble.id);
  const r = owner.getBoundingClientRect(), b = helpBubble.getBoundingClientRect();
  helpBubble.style.left = Math.max(12, Math.min(r.left, window.innerWidth - b.width - 12)) + 'px';
  helpBubble.style.top = Math.max(12, r.bottom + b.height + 12 > window.innerHeight ? r.top - b.height - 8 : r.bottom + 8) + 'px'
}
export function help(target, text, name) {
  if (!target || !text) return;
  const b = el('button', '?', 'help-button');
  b.type = 'button';
  b.setAttribute('aria-label', 'Help: ' + (name || target.textContent.trim()));
  b.addEventListener('pointerenter', () => showHelp(b, text));
  b.addEventListener('pointerleave', () => { if (!helpPinned && document.activeElement !== b) helpHideTimer = setTimeout(hideHelp, 200) });
  b.addEventListener('focus', () => showHelp(b, text));
  b.addEventListener('blur', hideHelp);
  b.addEventListener('click', e => {
    e.preventDefault();
    e.stopPropagation();
    if (helpOwner === b && helpPinned) hideHelp();
    else { showHelp(b, text); helpPinned = true }
  });
  if (/^(LABEL|H[1-6])$/.test(target.tagName)) target.append(b);
  else target.after(b);
  return b
}
export function field(form, name, title, value, min, max) {
  const label = el('label', title);
  help(label, helpText[name], title);
  const n = el('input');
  n.setAttribute('aria-label', title);
  Object.assign(n, {
    name,
    value: value ?? '',
    type: min === undefined ? 'text' : 'number',
    required: true
  });
  if (min !== undefined) {
    n.min = min;
    n.max = max
  }
  label.append(n);
  form.append(label);
  return n
}
export function select(form, name, title, value, options) {
  const label = el('label', title),
    n = el('select');
  help(label, helpText[name], title);
  n.setAttribute('aria-label', title);
  n.name = name;
  for (const [v, t] of options) {
    const o = el('option', t);
    o.value = v;
    n.append(o)
  }
  n.value = value;
  label.append(n);
  form.append(label);
  return n
}
export function autosave(form, save, message = 'Saved · active now') {
  const status = el('p', 'Changes save automatically.', 'save-status');
  form.append(status);
  let timer, running = false,
    pending = false;
  const run = async () => {
    if (running) {
      pending = true;
      return
    }
    if (!form.reportValidity()) return;
    running = true;
    status.textContent = 'Saving…';
    try {
      await save();
      status.textContent = message
    } catch (e) {
      status.textContent = 'Not saved: ' + e.message
    } finally {
      running = false;
      if (pending) {
        pending = false;
        run()
      }
    }
  };
  form.onsubmit = e => {
    e.preventDefault();
    clearTimeout(timer);
    run()
  };
  form.oninput = () => {
    clearTimeout(timer);
    status.textContent = 'Unsaved changes…';
    timer = setTimeout(run, 650)
  };
  form.onchange = () => {
    clearTimeout(timer);
    run()
  }
}
let saves = Promise.resolve();
export function patchSettings(patch) {
  const task = saves.catch(() => {}).then(async () => {
    return api('/api/settings', 'POST', patch)
  });
  saves = task;
  return task
}
export function numeric(form, keys) {
  return Object.fromEntries(keys.map(k => [k, Number(form.elements[k].value)]))
}
export function countdown(due) {
  if (!due) return 'Not scheduled';
  const seconds = Math.ceil(due - Date.now() / 1000);
  return seconds <= 0 ? 'Due now' : seconds < 60 ? `in ${seconds} seconds` : seconds < 3600 ? `in ${Math.ceil(seconds/60)} minutes` : `in ${Math.ceil(seconds/3600)} hours`
}
export function setAuthorization(username, password) {
  authorization = 'Basic ' + btoa(unescape(encodeURIComponent(username + ':' + password)))
}
export async function download(path) {
  const headers = new Headers();
  if (authorization) headers.set('Authorization', authorization);
  const r = await fetch(path, {
    headers
  });
  if (!r.ok) throw new Error('Backup download failed. Retry shortly.');
  const url = URL.createObjectURL(await r.blob()),
    link = el('a');
  link.href = url;
  link.download = path.split('/').pop();
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// In-page modal confirmations work with touch, keyboard, and browser automation.
// Cancellation is the default, and untrusted text is never interpreted as HTML.
export function confirmAction(message) {
  return new Promise(resolve => {
    const previous=document.activeElement, dialog=el('dialog',undefined,'confirm-dialog');
    const title=el('h2','Confirm operation');title.id='confirm-operation-title';dialog.setAttribute('aria-labelledby',title.id);
    const cancel=button('Cancel',()=>dialog.close('cancel'));cancel.autofocus=true;
    const accept=button('Confirm',()=>dialog.close('confirm'));accept.className='danger';
    const actions=el('div',undefined,'row');actions.append(cancel,accept);dialog.append(title,el('p',message),actions);
    dialog.addEventListener('close',()=>{const accepted=dialog.returnValue==='confirm';dialog.remove();if(previous?.isConnected)previous.focus();resolve(accepted)},{once:true});
    document.body.append(dialog);dialog.showModal();cancel.focus();
  });
}

export {compactNumber} from './numbers.js';
