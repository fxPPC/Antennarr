import { routeParams, pageURL, idLink, linkedText } from './navigation.js';
import { confirmAction } from './core.js';
import {
  $,
  el,
  api,
  notice,
  button,
  field,
  help,
  select,
  autosave,
  patchSettings,
  numeric,
  date,
} from './core.js';
let ready = false,
  channels = [];
const channelCards = new Map();
let searchGeneration=0, searchMatches=new Set(), searchSignature=null, searchQuery=null;
const preference = [
  ['stability', 'Prefer stream stability'],
  ['diversity', 'Prefer source diversity']
];
export async function init() {
  if (ready) return;
  $('#export-lineup').onclick=async()=>{try{const v=await api('/api/lineup');const u=URL.createObjectURL(new Blob([JSON.stringify(v,null,2)],{type:'application/json'}));const a=el('a');a.href=u;a.download='antennarr-channels.json';a.click();setTimeout(()=>URL.revokeObjectURL(u),1000)}catch(e){notice(e.message)}};
  $('#lineup-import').onsubmit=async e=>{e.preventDefault();const f=e.currentTarget,button=f.querySelector('button');try{
   const file=f.elements.lineup.files[0];if(!file || file.size>2*1024*1024)throw new Error('Choose a channel JSON file up to 2 MiB.');
   let lineup;try{lineup=JSON.parse(await file.text())}catch{throw new Error('This is not a valid channel lineup JSON file.')}
   if(!await confirmAction('Replace all channels? Rosters are not included and will rebuild. Accounts and settings are retained.'))return;
   button.disabled=true;$('#lineup-result').textContent='Replacing channels…';await api('/api/lineup','POST',{confirmed:true,lineup});await loadChannels();$('#lineup-result').textContent='Lineup replaced. Rosters are rebuilding.';
  }catch(e){$('#lineup-result').textContent=e.message}finally{button.disabled=false}};
  const [s, links] = await Promise.all([api('/api/settings'), api('/api/connection')]);
  const root = $('#tuner-links');
  root.replaceChildren();
  for (const [key, title] of [
      ['tuner_url', 'Recommended · private Docker network'],
      ['private_tuner_url', 'Private address alternative'],
      ['stream_fallback_url', 'Stream-only fallback · not a tuner registration URL']
    ]) {
    if (!links[key]) continue;
    const box = el('div', undefined, 'tuner-link'),
      input = el('input');
    input.value = links[key];
    input.readOnly = true;
    input.setAttribute('aria-label', title);
    box.append(el('strong', title), input, button('Copy', () => navigator.clipboard.writeText(input.value)));
    root.append(box)
  }

  const f = $('#output-settings');
  f.before(el('p','Roster checks run continuously in passes. Once each eligible stream has had a turn, the next pass begins. Provider pacing and playback priority still apply.'));
  f.replaceChildren();
  const keys = ['roster_target', 'startup_race', 'max_outgoing_streams', 'global_connections', 'provider_connections'];
  const labels = [
    ['Roster refill threshold', 2, 200],
    ['Newest startup candidates', 2, 10],
    ['Maximum outgoing viewer streams', 1, 64],
    ['Total upstream connections', 6, 256],
    ['Connections per provider', 1, 256]
  ];
  keys.forEach((k, i) => field(f, k, labels[i][0], s[k], labels[i][1], labels[i][2]));
  select(f, 'selection_preference', 'Source preference', s.selection_preference, preference);
  const ul = el('label'),
    u = el('input');
  Object.assign(u, {
    type: 'checkbox',
    name: 'allow_uhd',
    checked: s.allow_uhd === true
  });
  ul.append(u, document.createTextNode(' Enable UHD'));
  help(ul, 'For fast LAN viewing only. Off excludes names containing UHD. On prefers UHD and avoids switching quality class unless necessary. Changing modes rebuilds the roster or restores its saved assignments if less than one hour old.', 'Enable UHD');
  u.setAttribute('aria-label', 'Enable UHD');
  f.append(ul);
 const rl=el('label'),ri=el('input');Object.assign(ri,{type:'checkbox',name:'replenish_while_playing',checked:s.replenish_while_playing===true});
 rl.append(ri,document.createTextNode(' Replenish playing channels in the background'));
 help(rl,'Fill vacancies only for channels currently playing. Uses extra bandwidth and upstream connection slots. Routine checks and idle-channel recruitment remain paused during playback.','Live replenishment');f.append(rl);
  autosave(f, async () => {
    const v = {
      ...numeric(f, keys),
      selection_preference: f.elements.selection_preference.value,
      allow_uhd: u.checked, replenish_while_playing: ri.checked
    };
    const result = await patchSettings(v);
    if (result.settings && Number(f.elements.global_connections.value) === v.global_connections) f.elements.global_connections.value = result.settings.global_connections
  });
  $('#channel-form').onsubmit = async e => {
    e.preventDefault();
    const f = e.currentTarget;
    try {
      await api('/api/channels', 'POST', {
        id: f.elements.id.value,
        number: f.elements.number.value,
        name: f.elements.name.value,
        region: f.elements.region.value,
        aliases: f.elements.aliases.value.split('\n').map(s => s.trim()).filter(Boolean),
        enabled: f.elements.enabled.checked
      });
      $('#channel-result').textContent = 'Channel saved.';
      await loadChannels()
    } catch (e) {
      $('#channel-result').textContent = e.message
    }
  };
  $('#clear-channel').onclick = () => {
    $('#channel-form').reset();
    $('#channel-form').elements.id.value = ''
  };
  $('#output-search').onsubmit=e=>{e.preventDefault();location.hash=pageURL('output',{q:e.currentTarget.elements.q.value.trim()})};
  await loadChannels();
  for (const [id,defaults] of [['clear-all-channels',false],['reset-us-channels',true]]) {
    $('#'+id).onclick=async()=>{
      if(!await confirmAction(defaults?'Replace all channels and rosters with the 143 default US channels? Custom channels and rules will be removed. Accounts and login are kept; no backup is created.':'Remove all channels and their roster assignments? Accounts and streams are kept. No backup is created.'))return;
      try {await api('/api/channels/bulk','POST',{confirmed:true,default_us:defaults});$('#bulk-result').textContent='Channel selection updated.';await loadChannels()}catch(e){$('#bulk-result').textContent=e.message}
    };
  }
  ready = true
}
async function loadChannels() {
  const [cs, summary] = await Promise.all([api('/api/channels'), api('/api/roster')]);
  channels = cs;
  const root = $('#output-channels');
  root.replaceChildren();
  channelCards.clear();
  for (const ch of cs) {
    const r = summary.channels.find(r => r.channel_id === ch.id);
    const card = el('details', undefined, 'card'),
      head = el('summary');
    head.append(el('strong', ch.number + ' · ' + ch.name), el('span', r ? `${r.count}/${r.policy.target} verified · ${r.providers} provider${r.providers === 1 ? '' : 's'}` : 'Not offered to Plex', 'count-pill'));
    head.lastChild.dataset.channelCount = ch.id;
    if (r) paintRoster(head.lastChild, r);
    card.append(head);
    const controls = el('div', undefined, 'row');
    controls.append(button('Refresh sources', () => entries(ch, list)), button('Edit channel', () => {
      const f = $('#channel-form');
      for (const k of ['id', 'number', 'name', 'region']) f.elements[k].value = ch[k] || '';
      f.elements.aliases.value = (ch.aliases || []).join('\n');
      f.elements.enabled.checked = ch.enabled;
      $('#edit-channel').open = true;
      $('#edit-channel').scrollIntoView({
        block: 'nearest'
      })
    }), button('Remove channel', async () => {
      if (await confirmAction('Remove ' + ch.name + ' from Plex?')) {
        await api('/api/channels/' + encodeURIComponent(ch.id), 'DELETE');
        await loadChannels()
      }
    }));
    card.append(controls);
    const custom = el('details');
    custom.append(el('summary', 'Channel policy overrides'));
    const f = el('form', undefined, 'settings-grid'),
      o = r?.override || {};
    field(f, 'target', 'Roster refill threshold (0 inherits default)', o.target || 0, 0, 200);
    field(f, 'race', 'Startup candidates (0 inherits default)', o.race || 0, 0, 10);
    for (const key of ['target', 'race']) {
      const n = f.elements[key];
      n.addEventListener('input', () => n.setCustomValidity(Number(n.value) === 1 ? 'Use 0 to inherit, or 2 or more.' : ''))
    }
    select(f, 'preference', 'Source preference', o.preference || '', [
      ['', 'Use output default'], ...preference
    ]);
    autosave(f, () => api(`/api/roster/${encodeURIComponent(ch.id)}/policy`, 'POST', {
      ...numeric(f, ['target', 'race']),
      preference: f.elements.preference.value
    }));
    custom.append(f);
    card.append(custom);
    const list = el('div');
    card.append(list);
    card.addEventListener('toggle', () => {
      if (card.open) entries(ch, list).catch(e => notice(e.message))
    });
    channelCards.set(ch.id,{card,list,ch});
    root.append(card)
  }
  if (!cs.length) root.append(el('p', 'Add a channel to begin.'));
  searchSignature=null;
  if(ready)await route();
}
const entryLoads = new WeakMap();
function entries(ch, root) {
  if(entryLoads.has(root))return entryLoads.get(root);
  const load=renderEntries(ch,root).finally(()=>entryLoads.delete(root));
  entryLoads.set(root,load);return load;
}
async function renderEntries(ch, root) {
  const [rows, events] = await Promise.all([api('/api/roster/' + encodeURIComponent(ch.id)), api('/api/events')]);
  root.replaceChildren(el('h3', 'Recent verified streams'));
  for (const r of rows) {
    const row = el('div', undefined, 'roster-row');
    row.append(el('strong', r.name), linkedText(`${r.provider} · account #${r.account_id} · stream #${r.stream_id}`), el('small', `Verified ${date(r.verified)} · checked in continuous passes · ${r.codec||'codec pending'}${r.height?' / '+r.height+'p':''}`), button('Exclude from this channel', async () => {
      await api('/api/mappings', 'POST', {
        channel_id: ch.id,
        stream_id: r.stream_id,
        enabled: false
      });
      await entries(ch, root)
    }));
    row.dataset.streamId=r.stream_id;
    if(searchMatches.has(r.stream_id))row.classList.add('search-match');
    root.append(row)
  }
  if (!rows.length) root.append(el('p', 'No verified streams yet. Queued candidates will be auditioned as capacity permits.'));
  const excluded = el('details');
  excluded.append(el('summary', 'Excluded streams · restore a source'));
  excluded.addEventListener('toggle', async () => {
    if (!excluded.open) return;
    try {
      const rows = await api('/api/channels/' + encodeURIComponent(ch.id) + '/excluded');
      const body = el('div');
      for (const r of rows) {
        if (r.state === 'retired') continue;
        const row = el('div', undefined, 'roster-row');
        row.append(el('strong', r.name), linkedText(r.provider + ' · account #' + r.account_id + ' · stream #' + r.id), button('Restore source', async () => {
          await api('/api/mappings', 'POST', {
            channel_id: ch.id,
            stream_id: r.stream_id,
            enabled: true
          });
          await entries(ch, root)
        }));
        body.append(row)
      }
      if (!rows.length) body.append(el('p', 'No manually excluded streams.'));
      while (excluded.children.length > 1) excluded.lastChild.remove();
      excluded.append(body)
    } catch (e) {
      notice(e.message)
    }
  });
  root.append(excluded);
  const changes = events.filter(e => e.channel_id === ch.id && ['failover', 'roster_removed', 'roster_added', 'roster_rebuild', 'roster_restored'].includes(e.kind)).slice(0, 8);
  if (changes.length) {
    root.append(el('h3', 'Recent source changes'));
    for (const e of changes) root.append(linkedText(date(e.happened) + ' · ' + e.kind.replaceAll('_', ' ') + ' · ' + e.reason + (e.stream_id?' · stream #'+e.stream_id:'')))
  }
}
export async function refresh() {
  const s = await api('/api/roster');
  for (const r of s.channels) {
    const n = Array.from(document.querySelectorAll('[data-channel-count]')).find(n => n.dataset.channelCount === r.channel_id);
    if (n) paintRoster(n, r)
  }
  if(routeParams().get('q'))await route();
}

function paintRoster(node, r) {
  node.textContent = `${r.count}/${r.policy.target} verified · ${r.providers} provider${r.providers === 1 ? '' : 's'}`;
  node.className = 'count-pill ' + (r.count === 0 ? 'roster-empty' : r.count >= r.policy.target ? 'roster-full' : 'roster-partial')
}

// Search only saved metadata; displaying results never opens an upstream feed.
export async function route() {
 const generation=++searchGeneration, q=routeParams().get('q')||'';
 if(q!==searchQuery)$('#output-search').elements.q.value=q;
 searchQuery=q;
 const root=$('#output-search-results');
 if(!q){
  searchSignature=null;searchMatches=new Set();root.replaceChildren();
  for(const {card} of channelCards.values()){card.hidden=false;card.classList.remove('search-match')}
  return;
 }
 const v=await api('/api/output/search?q='+encodeURIComponent(q));
 if(generation!==searchGeneration)return;
 const signature=JSON.stringify([q,v]);if(signature===searchSignature)return;searchSignature=signature;
 root.replaceChildren();
 const visible=new Set(v.channels);searchMatches=new Set(v.streams.map(s=>s.stream_id));
 if(v.exact_kind&&!v.exists){root.append(el('p',`This ${v.exact_kind} has been removed or is unavailable.`));}
 else if(!visible.size&&!v.streams.length)root.append(el('p','No matching channels or streams.'));
 else root.append(el('p',`${visible.size} matching channels · ${v.streams.length}${v.truncated?'+':''} matching streams${v.truncated?' — narrow the search for more precise results':''}`));
 for(const [id,{card,list,ch}] of channelCards){
  card.hidden=!visible.has(id);
  if(!card.hidden){card.classList.add('search-match');if(visible.size<=5){card.open=true;await entries(ch,list);if(generation!==searchGeneration)return}}
 }
 for(const s of v.streams){
  const row=el('article',undefined,'roster-row search-result');row.dataset.searchStreamId=s.stream_id;
  row.append(el('strong',s.name),linkedText(`${s.provider} · account #${s.account_id} · stream #${s.stream_id}`),el('small',s.in_roster?'Verified roster member':`Outside current rosters · ${s.state}`));
  for(const id of s.channels){const target=channelCards.get(id);if(!target)continue;row.append(button('Show '+target.ch.name,async()=>{target.card.hidden=false;target.card.open=true;await entries(target.ch,target.list);target.card.scrollIntoView({block:'start'})}))}
  root.append(row);
 }
}
