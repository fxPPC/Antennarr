import { compactNumber } from './core.js';
import { linkedText } from './navigation.js';
import {
  $,
  el,
  api,
  date,
  countdown,
  button
} from './core.js';
let names = new Map(),
  ready = false;
export async function init() {
  if (ready) return;
  const cs = await api('/api/channels');
  names = new Map(cs.map(c => [c.id, c.name]));
  ready = true
}
const age = t => t ? date(t) : 'No saved result yet';
const stat = (root, value, label, cls = '') => {
  const box = el('article', undefined, 'status-metric ' + cls);
  box.append(el('strong', compactNumber(value)), el('span', label)); root.append(box)
};
function taskLabel(j, result=false) {
 let text=j.message;
 for(const [kind,id] of [['account',j.account_id],['stream',j.stream_id]])if(id&&!text.toLowerCase().includes(kind+' #'+id))text+=' · '+kind+' #'+id;
 if(result)text+=' · '+(j.outcome==='timeout'?'Timed out':j.outcome);
 if(j.failure_kind)text+=' · '+[j.phase,j.failure_kind.replaceAll('_',' '),j.http_status?'HTTP '+j.http_status:''].filter(Boolean).join(' · ');
 return text;
}
export async function refresh() {
  const [snapshot, events] = await Promise.all([api('/api/monitor'), api('/api/events')]);
  const { playback: status, live: liveData, work, checker, database: db } = snapshot;
  const live = liveData || [];
  const c = db.coverage, q = db.queue, p = db.progress;
  for (const [id, name] of Object.entries(db.channels)) names.set(id, name);
  const need = c.vacancies ?? Math.max(0, c.target - c.verified);
  $('#status-hero').replaceChildren(el('span', status.viewers ? 'PLAYBACK NOW' : 'NO ACTIVE VIEWERS'), el('h3', `${status.viewers} / ${status.max_outgoing_streams} viewers · ${status.active_channels} channel${status.active_channels === 1 ? '' : 's'} streaming`));
  const coverage = $('#channel-coverage'); coverage.replaceChildren();
  const partial = Math.max(0, c.total - c.empty - c.full);
  stat(coverage, c.empty, 'Roster empty', c.empty ? 'bad' : '');
  stat(coverage, partial, 'Partial roster', partial ? 'warning' : '');
  stat(coverage, c.full, 'Full roster', 'good');
  const root = $('#live-streams'); root.replaceChildren();
  let unprotected = 0;
  for (const out of status.outgoing_streams || []) {
    const v = live.find(v => v.channel_id === out.channel_id) || {}, row = el('div', undefined, 'stream-card');
    const stop = button('×', async () => { await api('/api/outgoing/' + encodeURIComponent(out.channel_id) + '/stop', 'POST', {}); await refresh() });
    stop.className = 'stop-stream'; stop.title = 'Stop this channel for all its viewers'; stop.setAttribute('aria-label', 'Stop ' + (names.get(out.channel_id) || 'channel') + ' for all viewers');
    const backup = v.standby === 'receiving' ? 'Backup receiving media' : v.ghost ? 'Backup connected · checking media' : 'No ready backup · recruiting';
    if (v.standby !== 'receiving') unprotected++;
    row.append(stop, el('h4', names.get(out.channel_id) || out.channel_id), el('p', `${out.viewers} viewer${out.viewers === 1 ? '' : 's'} · ${v.primary ? 'Primary connected' : 'Starting / recovering'}`), el('p', backup, v.standby === 'receiving' ? 'good-text' : 'warning-text'), linkedText(`Primary ${v.primary ? 'account #'+v.primary : 'pending'} · backup ${v.ghost ? 'account #'+v.ghost : 'pending'}`), el('small', `${v.codec || 'Connecting'}${v.width ? ' · ' + v.width + '×' + v.height : ''} · ${v.failovers || 0} failovers`));
    if (v.recovery) row.append(el('p', v.recovery)); root.append(row)
  }
  if (!root.childNodes.length) root.append(el('p', 'No outgoing streams.'));
  const tasks = $('#current-work'); tasks.replaceChildren();
  const labels = { checks_waiting: 'Remaining roster checks are waiting for provider retry windows or connection slots.', paused: 'Checks paused for an administration operation.', playback_priority: 'Background media checks yield to playback.', probe_cooldown: 'Waiting for the global media-check interval.', rosters_full: 'Rosters full; recruitment paused.', waiting_candidates: 'Waiting for eligible candidates or provider cooldowns.', selecting: 'Selecting the next eligible task.', checking: 'Checking a stream.', discovering: 'Checking an account and its channel catalogue.' };
  for (const j of work.active || []) tasks.append(linkedText(taskLabel(j)));
  tasks.append(el('p', status.viewers ? (checker.mode==='live_replenishment'?'Playing-channel replenishment enabled; routine checks remain paused.':labels.playback_priority) : labels[checker.state] || 'Checker starting.'));
  if (checker.next && !status.viewers) tasks.append(el('small', 'Next eligible check ' + countdown(checker.next)));
  tasks.append(el('small', 'Last saved media result: ' + age(p.last_check)), el('small', 'Last successful media result: ' + age(p.last_success)), el('small', 'Last channel discovery: ' + age(p.last_discovery)), el('small','Last account access check: '+age(p.last_access)));
  const recruitment = $('#recruitment'), queueExpanded = recruitment.querySelector('details')?.open || false; recruitment.replaceChildren();
  recruitment.append(el('h3', `${compactNumber(c.verified)} / ${compactNumber(c.target)} verified roster places`), el('p', `${compactNumber(need)} vacancies · ${db.check_pass?.remaining || 0} streams left in this check pass`), el('p', `Past hour: ${compactNumber(p.successful)} successful media results · ${compactNumber(p.failed)} failed results · ${compactNumber(p.added)} roster additions · ${compactNumber(p.removed)} removals`));
  const stages = el('div', undefined, 'status-metrics');
  stat(stages, q.access, 'Awaiting access check'); stat(stages, q.discovery, 'Awaiting channel discovery'); stat(stages, q.available, 'Examined reserve accounts'); stat(stages, q.resting, 'Provider / access waiting'); stat(stages, q.in_rosters, 'Accounts supplying rosters');
  const details = el('details'); details.open = queueExpanded; details.append(el('summary', 'Account stages and queued streams'), stages, el('p', `${compactNumber(q.streams)} known streams waiting outside rosters. Streams and accounts are different units; each eligible account appears in exactly one stage. Accounts can supply several channels.`)); recruitment.append(details);
  const attention = $('#needs-attention'); attention.replaceChildren();
 if(snapshot.database_error)attention.append(el('p',snapshot.database_error,'warning-text'));
  if (unprotected) attention.append(el('p', `${unprotected} active channel${unprotected === 1 ? '' : 's'} without a confirmed receiving backup.`, 'warning-text'));
  const queued=q.access+q.discovery+q.available+q.resting;
  if (need && !queued && !db.eligible_media && !db.eligible_discovery) attention.append(el('p','New account lists are needed: channel rosters have vacancies and no accounts are waiting in the input queue.','warning-text'));
  else if (need && queued<10) attention.append(el('p',`Input queue is low: ${queued} accounts remain for ${compactNumber(need)} roster vacancies. Consider importing more authorised lists.`,'warning-text'));
  else if (c.empty && !db.eligible_media && !db.eligible_discovery && !q.resting) attention.append(el('p','No eligible sources currently match the empty channels. Import more accounts or review channel aliases, exclusions, country, and UHD settings.','warning-text'));
  if (!c.total) attention.append(el('p','No enabled channels. Add channels under Output, or use Bulk operations to restore the default US selection.','warning-text'));
  if (q.resting && need) attention.append(el('p',`${compactNumber(q.resting)} accounts are waiting for provider pacing or access retry limits. Stream requeueing itself has no cooldown.`));
  const pass=db.check_pass || {};
  if (pass.generation && c.total) recruitment.append(el('p',`Check pass ${compactNumber(pass.generation)}: ${compactNumber(pass.checked)} / ${compactNumber(pass.total)} unique streams checked · ${compactNumber(pass.remaining)} pending · ${compactNumber(pass.deferred||0)} temporarily deferred · ${compactNumber(pass.released)} released for replacement.`));
  if (pass.last_seconds) recruitment.append(el('small',`Previous pass took ${pass.last_seconds<60 ? pass.last_seconds+' seconds' : (pass.last_seconds/60).toFixed(1)+' minutes'}: ${pass.last_checked} checked, ${pass.last_released} released for replacement.`));
  if (c.empty) attention.append(el('p', `${c.empty} channels have no verified source yet.`, 'warning-text'));
  if (checker.last_error) attention.append(el('p', checker.last_error + ' failed at ' + date(checker.error_at) + '. Check Docker logs and Admin → System info.', 'warning-text'));
  const completedChecks=(work.recent||[]).filter(j=>['stream_check','account_check','account_discovery'].includes(j.kind)).map(j=>j.finished||0);
  const lastResult = Math.max(p.last_check, p.last_discovery,p.last_access||0,...completedChecks);
  if(checker.state==='checks_waiting' && pass.remaining)attention.append(el('p',`${compactNumber(pass.remaining)} pending checks are waiting for providers.${checker.next?' Earliest retry '+countdown(checker.next)+'.':''}`));
  if (!status.viewers && (db.eligible_media || (need>0 && db.eligible_discovery>0)) && lastResult && snapshot.collected - lastResult > 3600 && checker.state !== 'paused') attention.append(el('p', 'Eligible work remains, but no media result or catalogue discovery has been saved for over an hour. The checker needs investigation.', 'warning-text'));
  if (!attention.childNodes.length) attention.append(el('p', 'No additional operational warnings. This is not a guarantee that every source is working.'));
  $('#status-collected').textContent = 'Updated ' + date(snapshot.collected) + ' · catalogue snapshot ' + date(db.collected);
  const next = $('#upcoming'); next.replaceChildren();
  if (need) next.append(el('article', `${compactNumber(need)} roster vacancies. ${compactNumber(db.eligible_discovery)} accounts eligible for initial discovery now. Media auditions retain global and provider pacing.`, 'schedule-card'));
  if (db.next_discovery / 1000 > snapshot.collected) next.append(el('article', 'Earliest initial-discovery provider slot ' + countdown(db.next_discovery / 1000), 'schedule-card'));
  if (!next.childNodes.length) next.append(el('p', c.total ? 'Rosters are full; continuous check passes continue.' : 'Add channels under Output to begin recruitment.'));
  const recent = $('#recent-events'); recent.replaceChildren();
  const records = [...(work.recent || []).filter(j => (!['deferred', 'paused'].includes(j.outcome)||['rate_limited','oversized_catalogue','oversized_response'].includes(j.failure_kind))).map(j => ({time: j.finished, text: taskLabel(j,true)})), ...events.map(e => ({time: e.happened, text: [names.get(e.channel_id),e.kind.replaceAll('_',' '),e.reason,e.stream_id?'stream #'+e.stream_id:''].filter(Boolean).join(' · ')}))].sort((a,b) => b.time-a.time);
  const seen = new Set();
  for (const r of records) { if (seen.has(r.text)) continue; seen.add(r.text); const row=el('div', undefined, 'timeline-item'); row.append(linkedText(r.text),el('small',date(r.time)));recent.append(row);if(seen.size>=15)break }
  if (!seen.size) recent.append(el('p', 'No recent completed tasks. Cooldown deferrals are grouped under background work.'))
}
