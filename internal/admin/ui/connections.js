import {el, button} from './core.js';

// These guide names and locations match the verified city presets in MULTI-TUNER.md.
const lineups = new Map([
  ['CA/M5V 2T6','Bell Fibe TV'], ['ES/28001','Movistar TV'], ['FR/75001','Free'],
  ['IT/00184','Sky Q'], ['PL/00-001','Canal+'], ['PT/1100','MEO Fibra'], ['BR/01001','Net TV'],
  ['SE/11120','Tele2'], ['DK/1050','Norlys'], ['NO/0150','Telenor'],
  ['AR/C1064','Cablevisión - Capital Federal'], ['CO/110111','Claro TV Colombia (DTH)'],
  ['BE/1000','Proximus'], ['CH/8001','Sunrise TV (Deutschschweiz)']
]);

export function clientAddresses(profile) {
  const base = (profile.tuner_url || '').replace(/\/+$/, '');
  return {plex:base, playlist:base && base + '/playlist.m3u', guide:base && base + '/guide.xml'};
}

function address(root, title, value) {
  const label = el('label',title,'address-label'), field = el('input');
  field.value = value;
  field.readOnly = true;
  field.setAttribute('aria-label', title);
  const status = el('span', '', 'copy-status');
  status.setAttribute('role','status');
  const copy = button('Copy', async () => {
    try {
      await navigator.clipboard.writeText(value);
      status.textContent = 'Copied.';
    } catch {
      field.focus();
      field.select();
      status.textContent = 'Selected. Use your device’s copy command.';
    }
  });
  copy.setAttribute('aria-label','Copy ' + title);
  label.append(field);
  const row = el('div', undefined, 'copy-address');
  row.append(label, copy, status);
  root.append(row);
}

export function clientConnections(profile, initial = '') {
  const root = el('div', undefined, 'client-connections');
  const group = el('div', undefined, 'connect-controls');
  const pane = el('div', undefined, 'client-panel');
  const urls = clientAddresses(profile);
  const buttons = new Map();
  let active = '';
  root.append(group,pane);
  pane.hidden = true;
  function show(client) {
    active = client;
    for (const [key,b] of buttons) b.setAttribute('aria-expanded',String(key === active));
    pane.hidden = !active;
    pane.replaceChildren();
    if (!active) return;
    if (!profile.enabled) pane.append(el('p','Enable this tuner to serve its channels.', 'warning-text'));
    if (!urls.plex) {
      pane.append(el('p', profile.address_error || 'Set a reachable tuner address in the network settings.'));
      return;
    }
    if (client === 'Plex') {
      pane.append(el('p','In Plex, open Settings → Live TV & DVR and add this network tuner.'));
      address(pane,'Tuner address',urls.plex);
      const location = [profile.market, profile.country, profile.guide_location].filter(Boolean).join(' · ');
      const lineup = lineups.get(profile.country + '/' + profile.guide_location);
      pane.append(el('p',[location && 'Guide location: ' + location, lineup && 'Lineup: ' + lineup].filter(Boolean).join('. ') || 'Select the guide for this tuner’s channels.'));
      if (profile.private_tuner_url || profile.stream_fallback_url) {
        const extra = el('details');
        extra.append(el('summary','Alternative network addresses'));
        if (profile.private_tuner_url) address(extra,'Private tuner address',profile.private_tuner_url);
        if (profile.stream_fallback_url) address(extra,'Stream-only fallback',profile.stream_fallback_url);
        pane.append(extra);
      }
    } else {
      pane.append(el('p','In Jellyfin, open Dashboard → Live TV. Add an M3U tuner, then add an XMLTV guide provider for this tuner.'));
      address(pane,'M3U playlist URL',urls.playlist);
      address(pane,'XMLTV guide URL',urls.guide);
    }
    if (profile.address_error) pane.append(el('p',profile.address_error,'warning-text'));
  }
  for (const client of ['Plex','Jellyfin']) {
    const b = el('button','Connect to ' + client);
    b.type = 'button';
    b.setAttribute('aria-expanded','false');
    b.onclick = () => show(active === client ? '' : client);
    group.append(b);
    buttons.set(client,b);
  }
  if (initial) show(initial);
  return root;
}
