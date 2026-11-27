import {$, api} from './core.js';
import {showImport} from './source-import.js';
import {clientConnections} from './connections.js';
import {tunerSlotSummary} from './listeners.js';

const steps = ['tuner', 'sources', 'plex'];
const headings = ['Choose a tuner', 'Add a source', 'Connect your media server'];
let profiles = [], presets = [], saved, step, lastOptions, listeners;
let visiting = false, working = false, focusOnOpen = false, generation = 0;
let invalidateTuners, connectionKey = '';

// Installed tuners can have custom names and may have no city preset at all.
export function tunerOptions(profiles, presets, availableSlots) {
  const installed = new Set(profiles.map(p => p.preset_id));
  return [
    ...profiles.map(p => ({value: 'profile:' + p.id, label: p.name, detail: p.enabled ? 'This tuner is enabled.' : 'Next enables this tuner.'})),
    ...presets.filter(p => !installed.has(p.id)).map(p => ({
      value: 'preset:' + p.id, label: p.name, disabled: availableSlots === 0,
      detail: 'Next adds this tuner with ' + p.channels + ' channels and enables it.'
    }))
  ];
}

export function currentStep(setup, profiles) {
  if (!['active', 'pending'].includes(setup.state)) return '';
  const profile = profiles.find(p => p.id === setup.profile_id && p.enabled);
  if (!profile || !steps.includes(setup.step)) return 'tuner';
  if (setup.step === 'plex' && !setup.has_sources) return 'sources';
  return setup.step;
}

function moveImport(target) {
  const form = $('#import-form');
  if (form.parentNode !== target) target.prepend(form);
  const portal = $('#portal-source');
  if (portal.parentNode !== target) target.append(portal);
}

function hide() {
  moveImport($('#source-import'));
  $('#setup-guide').hidden = true;
  step = '';
}

function controls() {
  const profile = profiles.find(p => p.id === saved?.profile_id);
  $('#setup-next').disabled = working || (step === 'tuner' ? !$('#setup-tuner').value || $('#setup-tuner').selectedOptions[0]?.disabled : !saved?.has_sources);
  $('#setup-finish').disabled = working || !profile?.tuner_url || !profile?.enabled || !saved?.has_sources;
  for (const id of ['setup-back','setup-skip','setup-tuner','setup-resume']) $('#' + id).disabled = working;
}

function selectHint() {
  const selected = tunerOptions(profiles, presets, listeners?.available_slots).find(p => p.value === $('#setup-tuner').value);
  $('#setup-city-hint').textContent = selected?.detail || (listeners?.available_slots === 0 ? 'Choose an installed tuner to continue.' : 'Choose a city for your first channel lineup.');
  controls();
}

function render(focus = false) {
  const next = currentStep(saved, profiles);
  if (!next || !visiting) { hide(); return; }
  const changed = step !== next;
  step = next;
  const index = steps.indexOf(step);
  const selected = profiles.find(p => p.id === saved.profile_id);
  $('#setup-guide').hidden = false;
  $('#setup-outcome').textContent = '';
  $('#setup-step').textContent = 'Set up live TV · Step ' + (index + 1) + ' of ' + steps.length;
  $('#setup-heading').textContent = headings[index];
  $('#setup-city').hidden = step !== 'tuner';
  $('#setup-sources').hidden = step !== 'sources';
  $('#setup-plex').hidden = step !== 'plex';
  $('#setup-back').hidden = step === 'tuner';
  $('#setup-next').hidden = step === 'plex';
  $('#setup-finish').hidden = step !== 'plex';
  moveImport($(step === 'sources' ? '#setup-import' : '#source-import'));
  if (changed || focus) $('#setup-message').textContent = '';
  if (step === 'tuner') {
    const select = $('#setup-tuner'), options = tunerOptions(profiles, presets, listeners.available_slots);
    $('#setup-capacity span').textContent = tunerSlotSummary(listeners);
    const signature = JSON.stringify(options);
    const previous = select.value;
    const optionsChanged = signature !== lastOptions;
    if (optionsChanged) {
      select.replaceChildren(new Option('Choose a tuner…', ''), ...options.map(o => {
        const option = new Option(o.label, o.value);
        option.disabled = o.disabled === true;
        return option;
      }));
      lastOptions = signature;
    }
    if (changed || optionsChanged) {
      select.value = (!changed && options.some(o => o.value === previous && !o.disabled)) ? previous : selected ? 'profile:' + selected.id : '';
    }
    selectHint();
  } else if (step === 'sources') {
    $('#setup-source-hint').textContent = saved.has_sources
      ? 'Accounts are saved. Continue to connect a media server.'
      : 'Import an authorised list or add your portal account to continue.';
  } else {
    const key = JSON.stringify(selected);
    if (key !== connectionKey) {
      $('#setup-clients').replaceChildren(clientConnections(selected));
      connectionKey = key;
    }
  }
  controls();
  if (focus) $('#setup-heading').focus();
}

async function load(ticket, focus = false) {
  const data = await api('/api/setup');
  if (ticket !== generation || !visiting) return;
  if (!['active', 'pending'].includes(data.state)) {
    saved = data;
    hide();
    return;
  }
  const [p, templates, ports, job] = await Promise.all([
    api('/api/profiles'),
    presets.length ? presets : api('/api/presets'),
    api('/api/listeners'),
    data.step === 'sources' || (data.step === 'plex' && !data.has_sources) ? api('/api/input/import') : null
  ]);
  if (ticket !== generation || !visiting) return;
  saved = data;
  profiles = p;
  listeners = ports;
  presets = templates;
  if (job) showImport(job);
  render(focus || focusOnOpen);
  focusOnOpen = false;
}

function report(e) {
  const target = visiting && !$('#setup-guide').hidden ? $('#setup-message') : visiting ? $('#setup-outcome') : $('#setup-resume-result');
  target.textContent = e.message;
}

async function refresh(focus = false) {
  const ticket = ++generation;
  try { await load(ticket, focus); } catch (e) { if (ticket === generation) report(e); }
}

export function visit(page) {
  visiting = page === 'status';
  generation++;
  if (visiting) { void refresh(); } else { hide(); }
}

let polling = false;
export async function poll() {
  if (!visiting || working || polling || (saved && !step)) return;
  polling = true;
  try { await refresh(); } finally { polling = false; }
}

// Form edits and keyboard focus survive each dashboard poll.
async function act(action) {
  if (working) return;
  working = true;
  generation++;
  controls();
  $('#setup-message').textContent = '';
  $('#setup-resume-result').textContent = '';
  try { await action(); } catch (e) { report(e); }
  finally { working = false; controls(); }
}

async function save(patch) {
  saved = await api('/api/setup', 'POST', patch);
}

async function next() {
  if (step === 'tuner') {
    const value = $('#setup-tuner').value;
    const installed = await api('/api/profiles');
    let chosen = value.startsWith('profile:') ? installed.find(p => 'profile:' + p.id === value)
      : installed.find(p => 'preset:' + p.preset_id === value);
    if (!chosen && value.startsWith('preset:')) {
      chosen = await api('/api/profiles', 'POST', {preset_id: value.slice('preset:'.length)});
      invalidateTuners();
    }
    if (!chosen) throw new Error('This tuner is no longer available. Choose another tuner.');
    if (!chosen.enabled) {
      await api('/api/profiles/' + encodeURIComponent(chosen.id), 'POST', {enabled: true});
      invalidateTuners();
    }
    await save({state:'active', step:'sources', profile_id:chosen.id});
  } else {
    await save({state:'active', step:'plex'});
  }
  if (visiting) await refresh(true);
}

async function close(state) {
  await save({state});
  hide();
  if (visiting) {
    $('#setup-outcome').textContent = state === 'complete' ? 'Setup finished.' : 'Setup saved. Reopen it from Admin.';
    $('#status-heading').focus();
  }
}

export function bind(onTunerChange) {
  invalidateTuners = onTunerChange;
  $('#setup-tuner').onchange = selectHint;
  $('#setup-next').onclick = () => act(next);
  $('#setup-back').onclick = () => act(async () => {
    await save({state:'active',step:steps[steps.indexOf(step) - 1]});
    if (visiting) await refresh(true);
  });
  $('#setup-skip').onclick = () => act(() => close('dismissed'));
  $('#setup-finish').onclick = () => act(() => close('complete'));
  $('#setup-resume').onclick = () => act(async () => {
    await save({state:'active'});
    $('#setup-outcome').textContent = '';
    focusOnOpen = true;
    location.hash = 'status';
  });
}
