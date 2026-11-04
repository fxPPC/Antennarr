import { confirmAction } from './ui/core.js';
import {
  $,
  api,
  notice,
  setAuthorization,
  download,
  help
} from './ui/core.js';
import * as input from './ui/input.js';
import * as output from './ui/output.js';
import * as status from './ui/status.js';
const staticHelp = [
  ['#import-form textarea', 'Enter list-page URLs, or Base64-encoded URLs, one per line. Antennarr reads them only when you import; it does not scan them automatically. Keep the full #key on encrypted paste links.', 'List URLs'],
  ['#channel-form [name=number]', 'The channel number advertised to Plex. Keep it stable to avoid disturbing existing guide mappings.', 'Channel number'],
  ['#channel-form [name=name]', 'The consistent channel name shown to Plex, regardless of which provider supplies it.', 'Plex name'],
  ['#channel-form [name=region]', 'Country code or English name, for example US, NZ, GBR, or Brazil. This separates country versions; aliases and negative keywords select regional feeds.', 'Country'],
  ['#channel-form [name=aliases]', 'Add alternative provider names, one per line. Names are case-insensitive and tolerate country and quality tags. Include accepted regional names as aliases, such as CNN East. Exclude unwanted feeds with -west, -pacific, or other keywords. Similar spelling alone is not enough. Prefix a whole word or consecutive phrase with - to exclude it, for example -international. Exclusions check both provider names and channel IDs, and override aliases and learned mappings.', 'Other provider names'],
  ['#channel-form [name=enabled]', 'Enable to offer this channel to Plex. Disable to hide it and stop its background media checks without deleting the channel configuration.', 'Offered to Plex'],
  ['#clear-import-queue', 'Removes unassigned queued streams and orphan accounts after confirmation. Current rosters and in-use accounts are protected; inactive saved UHD rosters are not. No backup is created automatically.', 'Clear import queue'],
  ['#backup', 'Creates and downloads a manual backup of settings, accounts, queue, rosters, history, and tuner identity. Includes passwords: store it privately. Plex’s own database and guide data are not included.', 'Full backup'],
  ['#restore-form input', 'Choose an Antennarr ZIP backup. Restore replaces saved application state and restarts Antennarr, so stop viewing first. No backup is made automatically. Keep the same tuner URL and Plex server to retain the existing Live TV setup.', 'Restore backup']
];
for (const [selector, text, name] of staticHelp) {
  const n = $(selector), label = n?.closest('label');
  if (label && n.matches('input,textarea,select')) n.setAttribute('aria-label', label.firstChild?.textContent.trim() || name);
  help(label || n, text, name)
}
help($('#live-streams').previousElementSibling, 'Outgoing viewers share one channel pipeline when they watch the same channel. Each active channel maintains a primary feed and a continuously receiving hot backup when sources are available.', 'Outgoing streams');
help($('#current-work').previousElementSibling, 'Live startup, backup replacement, imports, and background checks appear here while running. Background checks yield to playback.', 'Working on');
help($('#upcoming-title'), 'The next work depends on roster vacancies, continuous check passes, and provider pacing. Routine checks pause during playback; optional replenishment serves playing channels only.', 'Up next');
let adminLoaded = false;
const admin = {
  init: async () => {
    if (adminLoaded) return;
    const ls=await api('/api/settings');const lf=$('#logging-settings');lf.elements.logging_level.value=ls.logging_level;
    help(lf,'None suppresses application logs. Normal logs warnings and errors only. Debug includes source IDs, check results, and recovery diagnostics. Credentials and stream URLs remain hidden.','Docker logging');
    lf.onchange=async()=>{try{await api('/api/settings','POST',{logging_level:lf.elements.logging_level.value});$('#logging-result').textContent='Saved.'}catch(e){notice(e.message)}};
    const v = await api('/api/login');
    $('#login-form').elements.username.value = v.username;
    adminLoaded = true
  },
  refresh: async () => {
    if(!$('#system-info').open)return;
    const v=await api('/api/system');const root=$('#system-info-content');root.replaceChildren();
    const line=(label,value)=>{const p=document.createElement('p');p.textContent=label+': '+value;root.append(p)};
    line('uname -a',v.uname||'Unavailable');line('Go',v.go);line('Available CPU threads',v.cpus);line('Application uptime',Math.floor(v.uptime_seconds/60)+' minutes');line('Disk free', (v.disk_free_bytes/1e9).toFixed(2)+' GB of '+(v.disk_total_bytes/1e9).toFixed(2)+' GB');
    for(const [key,label] of [['background','Average host load while checking'],['streaming','Average host load while streaming']])line(label,v[key].samples?v[key].load.toFixed(2)+' ('+v[key].samples+' samples)':'No samples yet');line('Measurement',v.note);
  }
};
const pages = {
  status,
  input,
  output,
  admin
};
let current = '',
  busy = false,
  navigation = 0;
const initialising = new Map();
async function initialise(key) {
  if (!initialising.has(key)) {
    const p = pages[key].init().catch(e => {
      initialising.delete(key);
      throw e
    });
    initialising.set(key, p)
  }
  await initialising.get(key)
}
async function navigate() {
  const generation = ++navigation;
  const aliases = {
    sources: 'input',
    channels: 'output',
    overview: 'status',
    activity: 'status',
    settings: 'admin',
    administration: 'admin'
  };
  let key = location.hash.slice(1).split('?')[0] || 'status';
  key = aliases[key] || key;
  if (!pages[key]) key = 'status';
  current = key;
  for (const name of Object.keys(pages)) $('#' + name).hidden = name !== key;
  for (const a of document.querySelectorAll('nav a')) a.setAttribute('aria-current', a.hash === '#' + key ? 'page' : 'false');
  try {
    await initialise(key);
    if (generation !== navigation) return;
    await pages[key].route?.();
    if (generation !== navigation) return;
    await pages[key].refresh();
    if (generation === navigation) notice('')
  } catch (e) {
    if (generation === navigation) notice(e.message)
  }
}
window.addEventListener('hashchange', navigate);
navigate();
setInterval(async () => {
  if (busy || document.hidden || !current) return;
  busy = true;
  const key = current;
  try {
    await initialise(key);
    if (key !== current) return;
    await pages[key].refresh();
    if (key === current) notice('')
  } catch (e) {
    if (key === current) notice(e.message)
  } finally {
    busy = false
  }
}, 5000);
$('#login-form').onsubmit = async e => {
  e.preventDefault();
  const f = e.currentTarget;
  if (f.elements.password.value !== f.elements.confirm_password.value) {
    $('#login-result').textContent = 'New passwords do not match.';
    return
  }
  try {
    const username = f.elements.username.value,
      password = f.elements.password.value,
      current = f.elements.current_password.value;
    await api('/api/login', 'POST', {
      username,
      password,
      current_password: current
    });
    setAuthorization(username, password || current);
    f.elements.password.value = '';
    f.elements.confirm_password.value = '';
    f.elements.current_password.value = '';
    $('#login-result').textContent = 'Login changed.'
  } catch (e) {
    $('#login-result').textContent = e.message
  }
};
$('#backup').onclick = async () => {
  const b = $('#backup');
  b.disabled = true;
  try {
    const v = await api('/api/backup', 'POST', {});
    await download(v.download)
  } catch (e) {
    notice(e.message)
  } finally {
    b.disabled = false
  }
};
$('#restore-form').onsubmit = async e => {
  e.preventDefault();
  const f = e.currentTarget; // DOM event.currentTarget is cleared after an await.
  if (!await confirmAction('Replace Antennarr’s saved state with this backup?')) return;
  try {
    await api('/api/restore', 'POST', new FormData(f));
    $('#restore-result').textContent = 'Backup accepted. Antennarr is restarting; refresh shortly.'
  } catch (e) {
    $('#restore-result').textContent = e.message
  }
};

$('#system-info').addEventListener('toggle',()=>{if($('#system-info').open)admin.refresh().catch(e=>notice(e.message))});
$('#reset-antennarr').onclick=async()=>{
 if(!await confirmAction('Factory-reset Antennarr? This permanently removes configuration, accounts, queue, channels, rosters, history, and the current login and tuner identity. Fresh login will be admin/admin. You will need to configure Plex again. Existing manual backup archives are kept; no automatic backup is created. Stop playback first.'))return;
 try{await api('/api/reset','POST',{confirmed:true});$('#reset-result').textContent='Reset accepted. Antennarr is restarting. Refresh and sign in as admin/admin; change the password before public use.'}catch(e){$('#reset-result').textContent=e.message}
};
