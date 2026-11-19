import { routeParams, pageURL, linkedText } from './navigation.js';
import { confirmAction } from './core.js';
import { profilePath, profileChannels, profileLabel } from './profiles.js';
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
let ready = false, profiles = [], presets = [], channels = [], selectedProfile = '';
const channelCards = new Map(), profileCards = new Map();
let loadingProfiles, viewKey = '', editReturn = '';
let rosterByChannel = new Map();
function view() {
  const params = routeParams();
  return {tuner: params.get('tuner') || '', coverage: params.get('coverage') || '', searching: !!params.get('q')};
}
function channelURL(profile, extra = {}) {
  return pageURL('output', {tuner: profile, ...extra});
}
function navLink(label, href, cls) {
  const a = el('a', label, cls);
  a.href = href;
  return a;
}
const operationProfile = () => {
  if (!profiles.some(profile => profile.id === selectedProfile)) throw new Error('Choose a tuner first.');
  return selectedProfile;
};
let searchGeneration=0, searchMatches=new Set(), searchSignature=null, searchQuery=null;
const preference = [
  ['stability', 'Prefer stream stability'],
  ['diversity', 'Prefer source diversity']
];
export async function init() {
  if (ready) return;
  $('#export-lineup').onclick = async () => {
    const trigger = $('#export-lineup');
    trigger.disabled = true;
    try {
      const profileID = operationProfile();
      const lineup = await api(profilePath('/api/lineup', profileID));
      const url = URL.createObjectURL(new Blob([JSON.stringify(lineup, null, 2)], {type: 'application/json'}));
      const link = el('a');
      link.href = url;
      link.download = 'antennarr-' + profileID + '-channels.json';
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) { notice(error.message); }
    finally { trigger.disabled = false; }
  };
  $('#lineup-import').onsubmit = async event => {
    event.preventDefault();
    const form = event.currentTarget, submit = form.querySelector('button');
    try {
      const profileID = operationProfile(), name = profileLabel(profiles, profileID);
      const file = form.elements.lineup.files[0];
      if (!file || file.size > 2 * 1024 * 1024) throw new Error('Choose a channel JSON file up to 2 MiB.');
      let lineup;
      try { lineup = JSON.parse(await file.text()); } catch { throw new Error('This is not a valid channel lineup JSON file.'); }
      if (!await confirmAction('Replace all channels in ' + name + '? Its rosters will rebuild. Other tuners, accounts, and settings are kept.')) return;
      submit.disabled = true;
      $('#lineup-result').textContent = 'Replacing channels…';
      await api(profilePath('/api/lineup', profileID), 'POST', {confirmed: true, lineup});
      if (selectedProfile === profileID) selectProfile(profileID, true);
      await loadChannels();
      $('#lineup-result').textContent = 'Lineup replaced for ' + name + '. Its rosters will rebuild when enabled.';
    } catch (error) { $('#lineup-result').textContent = error.message; }
    finally { submit.disabled = false; }
  };
  help($('#channel-form').elements.station.parentElement, 'Optional exact station ID, such as WNBC. Both primary and backup sources must identify it in their name or guide ID. Generic network labels are insufficient; fewer feeds may qualify. Match the station to the guide selected in Plex.', 'Required station ID');
  $('#output-profile').onchange = event => selectProfile(event.target.value, true);
  const s = await api('/api/settings');
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
        profile_id: f.elements.profile_id.value || operationProfile(),
        number: f.elements.number.value,
        name: f.elements.name.value,
        region: f.elements.region.value,
        station: f.elements.station.value,
        aliases: f.elements.aliases.value.split('\n').map(s => s.trim()).filter(Boolean),
        enabled: f.elements.enabled.checked
      });
      $('#channel-result').textContent = 'Channel saved.';
      await loadChannels();
      const saved = channelCards.get(editReturn);
      if (saved) {
        $('#edit-channel').open = false;
        saved.card.querySelector('summary').focus();
        saved.card.scrollIntoView({block:'center'});
        notice('Channel saved.');
      }
    } catch (e) {
      $('#channel-result').textContent = e.message
    }
  };
  $('#clear-channel').onclick = () => {
    selectProfile(operationProfile(), true)
  };
  $('#output-search').onsubmit=e=>{e.preventDefault();location.hash=channelURL(view().tuner,{q:e.currentTarget.elements.q.value.trim()})};
  await loadChannels();
  for (const [id, reset] of [['clear-all-channels', false], ['reset-preset-channels', true]]) {
    $('#' + id).onclick = async () => {
      try {
        const profileID = operationProfile(), name = profileLabel(profiles, profileID);
        const message = reset
          ? 'Replace channels and rosters in ' + name + ' with its original preset? Custom channel edits in this tuner will be removed.'
          : 'Remove all channels and roster assignments from ' + name + '?';
        if (!await confirmAction(message + ' Other tuners, accounts, streams, and login are kept. No backup is created.')) return;
        await api(profilePath('/api/channels/bulk', profileID), 'POST', {confirmed: true, reset_preset: reset});
        if (selectedProfile === profileID) selectProfile(profileID, true);
        await loadChannels();
        $('#bulk-result').textContent = 'Channel selection updated for ' + name + '.';
      } catch (error) { $('#bulk-result').textContent = error.message; }
    };
  }
  ready = true
}
// Rebuilding after a mutation retains open tuner/channel sections. Periodic
// refreshes update counters in place and do not reset edits or keyboard focus.
function loadChannels() {
  if (loadingProfiles) return loadingProfiles;
  loadingProfiles = renderProfiles().finally(() => { loadingProfiles = null; });
  return loadingProfiles;
}
async function renderProfiles() {
  const openChannels = new Set([...channelCards].filter(([, value]) => value.card.open).map(([id]) => id));
  const [loadedProfiles, loadedPresets] = await Promise.all([
    api('/api/profiles'), api('/api/presets')
  ]);
  const {tuner: requested, coverage, searching} = view();
  const scoped = requested ? loadedProfiles.filter(p => p.id === requested) :
    coverage ? loadedProfiles.filter(p => p.enabled) : searching ? loadedProfiles : [];
  viewKey = JSON.stringify([requested, coverage, searching]);
  const [channelLists, savedRosters] = await Promise.all([
    Promise.all(scoped.map(profile => api(profilePath('/api/channels', profile.id)))),
    Promise.all(scoped.map(profile => api(profilePath('/api/roster', profile.id))))
  ]);
  // Disabled tuners still show saved counts and policy overrides. The default
  // roster summary remains restricted to work eligible for background checks.
  rosterByChannel = new Map(savedRosters.flatMap(r => r.channels).map(r => [r.channel_id,r]));
  profiles = loadedProfiles;
  presets = loadedPresets;
  channels = channelLists.flat();
  const editor = $('#channel-form');
  // Deleted/replaced channels must not remain editable under another tuner.
  // Keep genuine unsaved edits when their original channel still exists.
  if (editor.elements.id.value && !channels.some(channel => channel.id === editor.elements.id.value)) {
    editor.reset();
    editor.elements.id.value = '';
    $('#channel-result').textContent = '';
  }
  if (!profiles.some(profile => profile.id === selectedProfile)) selectedProfile = profiles[0]?.id || '';
  const selector = $('#output-profile');
  selector.replaceChildren();
  for (const profile of profiles) {
    const option = el('option', profile.name);
    option.value = profile.id;
    selector.append(option);
  }
  selector.value = selectedProfile;
  selector.disabled = !profiles.length;
  const active = profiles.find(p => p.id === requested);
  if (active) selectProfile(active.id, selectedProfile !== active.id);
  $('#output-profile-tools').hidden = !active;
  $('#tuners-title').textContent = active?.name || (coverage ? ({empty:'Empty rosters',partial:'Partial rosters',full:'Full rosters'}[coverage] || 'Channels') : 'Tuners');
  $('#tuners-intro').hidden = !!requested || !!coverage;
  $('#tuners-back').hidden = !requested && !coverage;
  $('#output-settings').closest('details').hidden = !!requested || !!coverage;
  $('#add-tuners').hidden = !!requested || !!coverage;
  $('#output-search').hidden = !!active;
  $('#output-search a').href = channelURL(requested,{coverage});
  const directory = $('#tuner-directory');
  directory.replaceChildren();
  if (!requested && !coverage && !searching) {
    for (const p of profiles) {
      const link = navLink('',channelURL(p.id),'market-link');
      link.append(el('strong',p.name),el('span',p.channel_count+' channels · '+(p.enabled?'Enabled':'Disabled')),el('b','→'));
      directory.append(link);
    }
    directory.append(navLink('Add tuner', pageURL('output',{add:1}),'add-market'));
  }
  if (requested && !active) directory.append(el('p','This tuner is no longer available.'));
  if (routeParams().has('add')) $('#add-tuners').open = true;
  $('#channel-form').elements.profile_id.value = selectedProfile;
  const root = $('#output-tuners');
  root.replaceChildren();
  profileCards.clear();
  channelCards.clear();
  for (const profile of scoped) {
    const tuner = el('details', undefined, 'tuner-card');
    tuner.dataset.profileId = profile.id;
    tuner.open = true;
    const head = el('summary'), state = el('span', profile.enabled ? 'Enabled' : 'Disabled', 'badge');
    state.dataset.profileState = profile.id;
    state.setAttribute('aria-live', 'polite');
    if (profile.enabled) state.classList.add('good');
    const ownChannels = profileChannels(channels, profile.id);
    const toggleLabel = el('label', undefined, 'tuner-toggle'), toggle = el('input');
    Object.assign(toggle, {type: 'checkbox', checked: profile.enabled});
    toggle.setAttribute('aria-label', 'Enable ' + profile.name);
    toggleLabel.append(toggle, document.createTextNode(' Enable'));
    // A checkbox in the always-visible summary operates independently of the
    // disclosure. Keep keyboard and label clicks from bubbling to the summary.
    toggleLabel.addEventListener('click', event => event.stopPropagation());
    toggleLabel.addEventListener('keydown', event => event.stopPropagation());
    help(toggleLabel, 'Disabled tuners keep their channels and rosters. They reject new viewing sessions and pause background recruitment and checks. Existing viewers may finish. Viewer and upstream limits are shared by all tuners.', 'Enable tuner');
    head.append(active ? el('strong','Lineup') : navLink(profile.name,channelURL(profile.id),'market-name'), toggleLabel, state, el('span', ownChannels.length + (ownChannels.length === 1 ? ' channel' : ' channels'), 'muted'));
    tuner.append(head);
    const result = el('p', '', 'save-status');
    result.setAttribute('role', 'status');
    toggle.onchange = async () => {
      const wanted = toggle.checked;
      toggle.disabled = true;
      result.textContent = 'Saving…';
      try {
        await api('/api/profiles/' + encodeURIComponent(profile.id), 'POST', {enabled: wanted});
        profile.enabled = wanted;
        state.textContent = wanted ? 'Enabled' : 'Disabled';
        state.className = 'badge' + (wanted ? ' good' : '');
        renderPresets();
        result.textContent = wanted ? 'Enabled. Available roster places can now be filled.' : 'Disabled. Channels and rosters are kept; existing viewers may finish.';
      } catch (error) {
        toggle.checked = profile.enabled;
        result.textContent = 'Not saved: ' + error.message;
        notice(result.textContent);
      } finally { toggle.disabled = false; }
    };
    tuner.append(result);
    const guideLocation = [profile.guide_location, profile.market, profile.country].filter(Boolean).join(' · ');
    const guide = el('p', guideLocation ? 'Plex guide location: ' + guideLocation + '. Set up this tuner separately in Plex and select a matching local guide.' : 'Choose the matching country, postal code, and guide in Plex.', 'muted');
    guide.classList.add('tuner-guide');
    tuner.append(guide);
    const resolution = select(tuner, 'preferred_height', 'Preferred resolution', String(profile.preferred_height || 0),
      [['0','Automatic'], ['720','Prefer 720p'], ['1080','Prefer 1080p']]);
    resolution.onchange = async () => {
      resolution.disabled = true;
      try {
        const height = Number(resolution.value);
        await api('/api/profiles/' + encodeURIComponent(profile.id), 'POST', {preferred_height: height});
        profile.preferred_height = height;
        result.textContent = 'Resolution preference saved.';
      } catch (error) {
        resolution.value = String(profile.preferred_height || 0);
        notice(error.message);
      } finally { resolution.disabled = false; }
    };
    if (profile.timezone) tuner.append(el('small', 'Programme time zone: ' + profile.timezone));
    const links = el('details');
    links.className='plex-connect';
    links.append(el('summary', 'Connect to Plex'));
    if (routeParams().has('connect')) links.open=true;
    appendTunerLinks(links, profile);
    tuner.append(links);
    const controls = el('div', undefined, 'row');
    controls.hidden = !active;
    controls.append(button('Add channel', () => {
      selectProfile(profile.id, true);
      $('#output-profile-tools').open = true;
      $('#edit-channel').open = true;
      $('#edit-channel').scrollIntoView({block: 'nearest'});
      $('#channel-form').elements.number.focus();
    }), button('Lineup tools', () => {
      selectProfile(profile.id, true);
      $('#output-profile-tools').open = true;
      $('#output-profile-tools').scrollIntoView({block: 'nearest'});
    }));
    tuner.append(controls);
    const list = el('div', undefined, 'tuner-channels');
    if (active) {
      const label = el('label','Find a channel');
      const filter = el('input');
      filter.type='search'; filter.placeholder='Channel name or number';
      filter.value=routeParams().get('filter') || '';
      filter.addEventListener('input',()=>{
        const params=routeParams();
        if(filter.value) params.set('filter',filter.value); else params.delete('filter');
        history.replaceState(null,'',pageURL('output',Object.fromEntries(params)));
        filterChannels().catch(e=>notice(e.message));
      });
      label.append(filter); tuner.append(label);
    }
    for (const channel of ownChannels) {
      const card = channelCard(channel, rosterByChannel.get(channel.id), tuner);
      card.open = openChannels.has(channel.id);
      list.append(card);
    }
    if (!ownChannels.length) list.append(el('p', 'No channels in this tuner. Add one or import a lineup.'));
    tuner.append(list);
    const settings = el('details');
    settings.append(el('summary', 'Tuner settings'));
    const nameForm = el('form', undefined, 'settings-grid');
    const nameInput = field(nameForm, 'name', 'Tuner name', profile.name);
    nameInput.maxLength = 100;
    autosave(nameForm, async () => {
      const name = nameInput.value.trim();
      await api('/api/profiles/' + encodeURIComponent(profile.id), 'POST', {name});
      profile.name = name;
      if(active) $('#tuners-title').textContent=name; else head.querySelector('.market-name').textContent=name;
      toggle.setAttribute('aria-label', 'Enable ' + name);
      const option = [...selector.options].find(option => option.value === profile.id);
      if (option) option.textContent = name;
      updateToolScope();
    });
    settings.append(nameForm, el('p', 'Removing this tuner deletes its channels and rosters. Re-adding its preset creates a new device that must be set up again in Plex.', 'muted'));
    const remove = button('Remove tuner', async () => {
      if (!await confirmAction('Remove ' + profile.name + ' and its channels and rosters? Other tuners and shared accounts are kept. Re-adding creates a new Plex device. No backup is created.')) return;
      await api('/api/profiles/' + encodeURIComponent(profile.id), 'DELETE', {confirmed: true});
      await loadChannels();
    });
    remove.className = 'danger';
    settings.append(remove);
    tuner.append(settings);
    profileCards.set(profile.id, tuner);
    root.append(tuner);
  }
  if (!profiles.length) root.append(el('p', 'No tuners configured. Add a preset below to begin.'));
  renderPresets();
  updateToolScope();
  searchSignature = null;
  await filterChannels();
}
function appendTunerLinks(root, profile) {
  if (profile.address_error) root.append(el('p', profile.address_error, 'warning-text'));
  let count = 0;
  for (const [key, title] of [
    ['tuner_url', 'Tuner address for Plex'],
    ['private_tuner_url', 'Private address alternative'],
    ['stream_fallback_url', 'Stream-only fallback · not a tuner registration URL']
  ]) {
    if (!profile[key]) continue;
    const box = el('div', undefined, 'tuner-link'), input = el('input');
    input.value = profile[key];
    input.readOnly = true;
    input.setAttribute('aria-label', profile.name + ' · ' + title);
    box.append(el('strong', title), input, button('Copy', () => navigator.clipboard.writeText(input.value)));
    root.append(box);
    count++;
  }
  if (!count) root.append(el('p', 'Tuner address unavailable. Check the listening address and Docker port mappings.'));
  if (profile.listen_port) root.append(el('p', 'TCP port ' + profile.listen_port + '. Plex must reach this port on the shared Docker network or through a private LAN port mapping.', 'muted'));
}
function renderPresets() {
  const root = $('#output-presets');
  root.replaceChildren();
  for (const preset of presets) {
    const id = preset.preset_id || preset.id;
    const added = profiles.find(profile => profile.preset_id === id);
    const row = el('article', undefined, 'preset-card');
    row.append(el('h3', preset.name), el('p', preset.channels + (preset.channels === 1 ? ' editable channel' : ' editable channels')), el('p', preset.guide_location || '', 'muted'));
    if (added) row.append(el('p', 'Already added · ' + (added.enabled ? 'enabled' : 'disabled'), 'muted'));
    else row.append(button('Add ' + preset.name, async () => {
      await api('/api/profiles', 'POST', {preset_id: id});
      await loadChannels();
    }));
    root.append(row);
  }
}
function selectProfile(profileID, clearEditor = false) {
  if (!profiles.some(profile => profile.id === profileID)) return;
  selectedProfile = profileID;
  $('#output-profile').value = profileID;
  if (clearEditor) {
    const form = $('#channel-form');
    form.reset();
    form.elements.id.value = '';
    form.elements.region.value = profiles.find(profile => profile.id === profileID).country || '';
    $('#channel-result').textContent = '';
  }
  $('#channel-form').elements.profile_id.value = profileID;
  updateToolScope();
}
function updateToolScope() {
  const profile = profiles.find(profile => profile.id === selectedProfile);
  $('#output-profile-scope').textContent = profile ? 'These tools affect ' + profile.name + ' only.' : 'Choose a tuner first.';
  $('#reset-preset-channels').disabled = !profile?.preset_id;
}
function editChannel(ch) {
  editReturn = ch.id;
  selectProfile(ch.profile_id, true);
  const f = $('#channel-form');
  for (const k of ['id', 'number', 'name', 'region', 'station']) f.elements[k].value = ch[k] || '';
  f.elements.aliases.value = (ch.aliases || []).join('\n');
  f.elements.enabled.checked = ch.enabled;
  $('#output-profile-tools').open = true;
  $('#edit-channel').open = true;
  $('#edit-channel').scrollIntoView({
    block: 'nearest'
  })
}
function channelCard(ch, r, parent) {
    const card = el('details', undefined, 'card'),
      head = el('summary');
    card.dataset.channelId = ch.id;
    head.append(el('strong', ch.number + ' · ' + ch.name), el('span', r ? `${r.count}/${r.policy.target} verified · ${r.providers} provider${r.providers === 1 ? '' : 's'}` : 'Not offered to Plex', 'count-pill'));
    head.lastChild.dataset.channelCount = ch.id;
    if (r) paintRoster(head.querySelector('[data-channel-count]'), r);
    if (!ch.enabled) head.append(el('span', 'Channel disabled', 'badge'));
    card.append(head);
    const list = el('div');
    channelCards.set(ch.id, {card, list, ch, parent});
    let built = false;
    card.addEventListener('toggle', () => {
      if (!card.open) return;
      if (!built) { buildBody(); built = true; }
      entries(ch,list).catch(e => notice(e.message));
    });
    return card;
    function buildBody() {
      const controls = el('div', undefined, 'row');
      controls.append(button('Refresh sources', () => entries(ch, list)), button('Edit channel', () => {
        if (view().tuner !== ch.profile_id) {
          location.hash = channelURL(ch.profile_id,{edit:ch.id});
          return;
        }
        editChannel(ch);
      }), button('Remove channel', async () => {
        if (await confirmAction('Remove ' + ch.name + ' from ' + profileLabel(profiles, ch.profile_id) + '?')) {
          await api('/api/channels/' + encodeURIComponent(ch.id), 'DELETE');
          if ($('#channel-form').elements.id.value === ch.id) selectProfile(ch.profile_id, true);
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
      card.append(list);
    }
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
  const saved = await Promise.all(profiles.filter(p => profileCards.has(p.id)).map(profile => api(profilePath('/api/roster', profile.id))));
  const counts = new Map([...document.querySelectorAll('[data-channel-count]')].map(node => [node.dataset.channelCount, node]));
  for (const r of saved.flatMap(roster => roster.channels)) {
    const n = counts.get(r.channel_id);
    rosterByChannel.set(r.channel_id,r);
    if (n) paintRoster(n, r)
  }
  if(view().coverage || routeParams().get('q'))await filterChannels();
}

function paintRoster(node, r) {
  node.textContent = `${r.count}/${r.policy.target} verified · ${r.providers} provider${r.providers === 1 ? '' : 's'}`;
  const verified = r.last_verified ? 'Last verified '+date(r.last_verified) : 'No saved verification';
  node.title = verified;
  let latest = node.parentElement?.querySelector('.last-verified');
  if (!latest && node.parentElement) {
    latest = el('small',undefined,'last-verified');
    node.parentElement.append(latest);
  }
  if(latest) latest.textContent=verified;
  node.textContent += r.count===1 ? ' · single source' : r.count>1 ? ' · alternatives available' : '';
  node.className = 'count-pill ' + (r.count === 0 ? 'roster-empty' : r.count >= r.policy.target ? 'roster-full' : 'roster-partial')
}

// Search only saved metadata; displaying results never opens an upstream feed.
export async function route() {
  const {tuner, coverage, searching} = view();
  const key=JSON.stringify([tuner,coverage,searching]);
  if (loadingProfiles) await loadingProfiles;
  if (key !== JSON.stringify(Object.values(view()))) return;
  if (viewKey !== key) await loadChannels();
  await filterChannels();
  if(routeParams().has('add')) { $('#add-tuners').open=true; $('#add-tuners').scrollIntoView(); }
  if(routeParams().has('connect')) document.querySelector('.plex-connect')?.setAttribute('open','');
  const row=channelCards.get(routeParams().get('edit'));
  if(row && row.ch.profile_id===tuner && $('#channel-form').elements.id.value!==row.ch.id){
    row.card.open=true;
    editChannel(row.ch);
  }
}
async function filterChannels() {
 const generation=++searchGeneration, q=routeParams().get('q')||'';
 if(q!==searchQuery)$('#output-search').elements.q.value=q;
 searchQuery=q;
 const root=$('#output-search-results');
 if(!q){
  searchSignature=null;searchMatches=new Set();root.replaceChildren();
  const coverage=view().coverage;
  const term=(routeParams().get('filter')||'').trim().toLowerCase();
  for(const {card,ch} of channelCards.values()){
    const r=rosterByChannel.get(ch.id);
    card.hidden=(term && !(ch.number+' '+ch.name).toLowerCase().includes(term)) || !!coverage && (!ch.enabled || !r || coverage!==(r.count===0?'empty':r.count>=r.policy.target?'full':'partial'));
    card.classList.remove('search-match');
  }
  for(const card of profileCards.values())card.hidden=!!coverage && !card.querySelector('.tuner-channels > details:not([hidden])');
  if((coverage || routeParams().get('filter')) && ![...channelCards.values()].some(v=>!v.card.hidden))root.append(el('p','No channels in this group.'));
  return;
 }
 const v=await api('/api/output/search?q='+encodeURIComponent(q));
 if(generation!==searchGeneration)return;
 const signature=JSON.stringify([q,v]);if(signature===searchSignature)return;searchSignature=signature;
 root.replaceChildren();
 const visible=new Set(v.channels.filter(id=>channelCards.has(id)));searchMatches=new Set(v.streams.map(s=>s.stream_id));
 if(v.exact_kind&&!v.exists){root.append(el('p',`This ${v.exact_kind} has been removed or is unavailable.`));}
 else if(!visible.size&&!v.streams.length)root.append(el('p','No matching channels or streams.'));
 else root.append(el('p',`${visible.size} matching channels · ${v.streams.length}${v.truncated?'+':''} matching streams${v.truncated?' — narrow the search for more precise results':''}`));
 for(const [id,card] of profileCards)card.hidden=!channels.some(channel=>channel.profile_id===id&&visible.has(channel.id));
 for(const [id,{card,list,ch,parent}] of channelCards){
  card.hidden=!visible.has(id);
  if(!card.hidden){card.classList.add('search-match');if(visible.size<=5){parent.open=true;card.open=true;await entries(ch,list);if(generation!==searchGeneration)return}}
 }
 for(const s of v.streams.filter(s=>!view().tuner || s.channels.some(id=>channelCards.has(id)))){
  const row=el('article',undefined,'roster-row search-result');row.dataset.searchStreamId=s.stream_id;
  row.append(el('strong',s.name),linkedText(`${s.provider} · account #${s.account_id} · stream #${s.stream_id}`),el('small',s.in_roster?'Verified roster member':`Outside current rosters · ${s.state}`));
  for(const id of s.channels){const target=channelCards.get(id);if(!target)continue;row.append(button('Show '+profileLabel(profiles,target.ch.profile_id)+' · '+target.ch.name,async()=>{target.parent.hidden=false;target.parent.open=true;target.card.hidden=false;target.card.open=true;await entries(target.ch,target.list);target.card.scrollIntoView({block:'start'})}))}
  root.append(row);
 }
}
