import {api, button, el} from './core.js';

export const guideStates = {
  missing: 'Missing guide', single: 'Single source', confirmed: 'Confirmed',
  conflict: 'Conflicting listings', pinned: 'Pinned station', cached: 'Cached listings',
};

export function matchesChannelFilter(filter, guide, roster, live) {
  if (filter === 'missing') return !guide || guide.state === 'missing';
  if (filter === 'conflict') return guide?.state === 'conflict';
  if (filter === 'backup') return live ? live.standby !== 'receiving' : (roster?.count || 0) < 2;
  return true;
}

export function standbyLabel(roster,live) {
  if (live?.standby === 'receiving') return 'Standby live';
  if (live) return 'Awaiting standby';
  if ((roster?.count || 0) > 1) return 'Backup candidate';
  return 'No backup';
}

const timeFormat = new Intl.DateTimeFormat(undefined,{hour:'numeric',minute:'2-digit'});
const dayFormat = new Intl.DateTimeFormat(undefined,{weekday:'short',hour:'numeric',minute:'2-digit'});
function when(ts, clock = timeFormat) {
  return ts ? clock.format(new Date(ts * 1000)) : '';
}
function listing(p) {
  return p?.title ? when(p.start) + ' · ' + p.title : '—';
}
function statusText(data) {
  const state = guideStates[data?.state] || guideStates.missing;
  return data?.until ? state + ' · to ' + when(data.until,dayFormat) : state;
}

// Keep each row mounted during polling so disclosure state and focus stay put.
export function paintChannelGuide(cells,data) {
  cells.now.textContent = data?.now?.title || '—';
  cells.next.textContent = listing(data?.next);
  cells.guide.textContent = statusText(data);
  cells.guide.dataset.state = data?.state || 'missing';
}

export function guideInspector(channel, onChange) {
  const root = el('details',undefined,'guide-inspector');
  const summary = el('summary','Compare guide sources');
  const content = el('div'), status = el('p','','save-status');
  status.setAttribute('role','status');
  root.append(summary,content,status);
  const query = new URLSearchParams({profile_id:channel.profile_id,id:channel.id});
  const search = el('form',undefined,'guide-feed'), label = el('label','Find another guide station');
  const term = el('input');
  Object.assign(term,{type:'search',maxLength:120,required:true});
  const find = el('button','Find station'); find.type = 'submit';
  label.append(term); search.append(label,find);
  const matches = el('div',undefined,'guide-candidates');
  root.append(search,matches);
  let busy = false, sequence = 0, searchSequence = 0;
  function candidateCard(c, selected) {
    const item = el('article',undefined,'candidate');
    item.append(el('strong',c.name || c.station || 'Unnamed station'),el('small',[c.station,c.source].filter(Boolean).join(' · ')),
      el('p','Now · ' + listing(c.now)),el('p','Next · ' + listing(c.next)));
    const pin = button(selected ? 'Pinned station' : 'Pin station',()=>save(c.id));
    pin.disabled = selected || busy;
    pin.setAttribute('aria-label',(selected ? 'Pinned ' : 'Pin ') + c.station + ' from ' + c.source);
    item.append(pin);
    return item;
  }
  search.onsubmit = async event => {
    event.preventDefault();
    const ticket = ++searchSequence;
    if (!term.value.trim()) return;
    const q = new URLSearchParams({profile_id:channel.profile_id,q:term.value.trim()});
    try {
      const data = await api('/api/guide/stations?' + q);
      if(ticket !== searchSequence || !root.open) return;
      matches.replaceChildren(...(data.candidates || []).map(c=>candidateCard(c,false)));
      if (!matches.childElementCount) matches.append(el('p','No matching guide stations.'));
    } catch(e) { if(ticket===searchSequence)status.textContent=e.message; }
  };
  async function render() {
    const ticket = ++sequence;
    const data = await api('/api/guide/channel?' + query);
    if (ticket !== sequence || !root.open) return;
    const reason = {
      confirmed: (data.origins || 0) + ' independent sources agree on the published programme.',
      conflict: 'Sources disagree on one or more slots. A tie retains an existing listing.',
      single: 'One independent guide supplies the published programme.',
      cached: 'Previously accepted listings remain available until their scheduled end.',
      missing: 'There are no current or upcoming listings for this channel.'
    };
    const text = data.pin ? 'Guide selection is pinned to one station.' : reason[data.state] || 'Guide selection is automatic.';
    content.replaceChildren(el('p',text));
    const candidates = data.candidates || [];
    if (!candidates.length) content.append(el('p', 'No current candidates. Check the tuner’s guide feed or wait for a roster update.', 'muted'));
    const grid = el('div',undefined,'guide-candidates');
    for (const c of candidates) {
      grid.append(candidateCard(c,data.pin === c.id));
    }
    content.append(grid);
    if (data.pin) content.append(button('Use automatic selection',()=>save('')));
  }
  async function save(id) {
    if (busy) return;
    busy = true;
    status.textContent = 'Saving selection…';
    try {
      await api('/api/guide/pin','POST',{profile_id:channel.profile_id,channel_id:channel.id,candidate_id:id});
      status.textContent = id ? 'Guide station pinned.' : 'Automatic selection restored.';
      busy = false;
      searchSequence++; matches.replaceChildren(); term.value = '';
      await Promise.all([render(),onChange()]);
      if (root.open) summary.focus({preventScroll:true});
    } catch (e) { status.textContent = e.message; }
    finally { busy = false; }
  }
  root.addEventListener('toggle',async()=>{
    if (!root.open) { sequence++; searchSequence++; return; }
    try { await render(); } catch (e) { status.textContent = e.message; }
  });
  return root;
}

export function guideSettings(profile, onChange) {
  const root = el('details',undefined,'guide-settings');
  root.append(el('summary','Programme guide settings'));
  const feed = el('p','Loading guide settings…','muted');
  const form = el('form',undefined,'guide-feed');
  const label = el('label','External XMLTV URL');
  const input = el('input');
  Object.assign(input,{type:'url',required:true,autocomplete:'off',spellcheck:false});
  label.append(input);
  const save = el('button','Save feed'); save.type = 'submit';
  const clear = button('Remove feed',()=>update(''));
  const refresh = button('Refresh listings',async()=>{
    await api('/api/guide/refresh','POST',{profile_id:profile.id});
    status.textContent = 'Guide refresh requested.';
    await onChange();
  });
  const status = el('p','','save-status'); status.setAttribute('role','status');
  form.append(label,save);
  root.append(feed,form,clear,refresh,status);
  let busy = false;
  async function read() {
    const data = await api('/api/guide?' + new URLSearchParams({profile_id:profile.id}));
    feed.textContent = data.feed_configured ? 'External feed · ' + data.feed_host : 'Using listings supplied by roster sources.';
    clear.hidden = !data.feed_configured;
  }
  clear.hidden = true;
  async function update(url) {
    if (busy) return;
    busy = save.disabled = clear.disabled = true;
    try {
      await api('/api/guide/feed','POST',{profile_id:profile.id,url});
      if (input.value.trim() === url) input.value = '';
      status.textContent = url ? 'External guide saved.' : 'External guide removed.';
      await Promise.all([read(),onChange()]);
    } catch (e) { status.textContent=e.message; }
    finally { busy = save.disabled = clear.disabled = false; }
  }
  form.onsubmit = event=>{event.preventDefault();void update(input.value.trim());};
  root.addEventListener('toggle',()=>{if(root.open) read().catch(e=>{feed.textContent=e.message;});});
  return root;
}
