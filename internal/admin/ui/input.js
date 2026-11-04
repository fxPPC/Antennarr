import { compactNumber } from './core.js';
import { routeParams, pageURL, idLink } from './navigation.js';
import { confirmAction } from './core.js';
import {
  $,
  el,
  api,
  notice,
  button,
  field,
  autosave,
  patchSettings,
  numeric,
  date,
  help
} from './core.js';
let ready = false,
  offset = 0,
  query = '',
  lastImport = '';
export async function init() {
  if (ready) return;
  const [s, p] = await Promise.all([api('/api/settings'), api('/api/removal-policy')]);
  const f = $('#input-settings');
  f.replaceChildren();
  const keys = ['probe_seconds', 'check_api_seconds', 'check_global_seconds', 'check_stream_seconds', 'check_stream_global_seconds', 'catalogue_hours'];
  const labels = [
    ['Media probe duration, seconds', 3, 10],
    ['Provider API spacing, seconds', 1, 600],
    ['Global API spacing, seconds', 1, 60],
    ['Provider stream spacing, seconds', 5, 3600],
    ['Global stream spacing, seconds', 5, 60],
    ['Channel-list cache, hours', 12, 168]
  ];
  keys.forEach((k, i) => field(f, k, labels[i][0], s[k], labels[i][1], labels[i][2]));
  autosave(f, () => patchSettings(numeric(f, keys)));
  const r = $('#retirement-settings');
  r.replaceChildren();
  field(r, 'account_failures', 'Failed account checks before deletion', p.account_failures, 0, 100);
  field(r, 'stream_failures', 'Stream failures before deletion', p.stream_failures, 0, 100);
  const fullLabel = el('label'), full = el('input');
  Object.assign(full, {type: 'checkbox', name: 'count_full_connections_as_failures', checked: s.count_full_connections_as_failures === true});
  fullLabel.append(full, document.createTextNode(' Count full connections as failures'));
  help(fullLabel, 'When enabled, a valid account with all connection slots occupied counts towards the failed-account deletion threshold. This can cause working but temporarily full accounts to be dropped. Leave unchecked to return full accounts to the queue without adding a failure or discarding verified roster entries.', 'Count full connections as failures');
  r.append(fullLabel);
  autosave(r, async () => {
    await api('/api/removal-policy', 'POST', numeric(r, ['account_failures', 'stream_failures']));
    await patchSettings({count_full_connections_as_failures: full.checked});
  });
  $('#import-form').onsubmit = async e => {
    e.preventDefault();
    const f = e.currentTarget,
      b = f.querySelector('button:not(.help-button)');
    b.disabled = true;
    try {
      await api('/api/import', 'POST', {
        text: f.elements.text.value,
        authorised: f.elements.authorised.checked
      });
      f.elements.text.value = '';
      await refresh()
    } catch (e) {
      notice(e.message)
    } finally {
      b.disabled = false
    }
  };
  $('#clear-import-queue').onclick = async () => {
    if (!await confirmAction('Clear the import queue? Unassigned accounts and streams will be removed, including queued entries referenced by saved UHD rosters. Current rosters and in-use accounts are kept. No backup is created; make a manual backup first if you may need these entries.')) return;
    const b = $('#clear-import-queue');
    b.disabled = true;
    try {
      const v = await api('/api/input/clear', 'POST', {
        confirmed: true
      });
      $('#clear-queue-result').textContent = `Removed ${v.removed.accounts} accounts and ${v.removed.streams} queued streams.`;
      offset = 0;
      await queue();
      await refresh()
    } catch (e) {
      $('#clear-queue-result').textContent = e.message
    } finally {
      b.disabled = false
    }
  };
  $('#queue-filter').onsubmit = e => {
    e.preventDefault();
    location.hash = pageURL('input', {q:e.currentTarget.elements.q.value.trim()});
  };
  ready = true;
  await queue()
}
// Preserve deliberate folds during refresh, but show every account on a new page.
let queueRequest = 0, queueLoadedAt = 0, queuePageKey = null, queueRendered = null;
const providerExpanded = new Map();
async function queue() {
  const request = ++queueRequest;
  const page = await api('/api/input/accounts?offset=' + offset + '&q=' + encodeURIComponent(query));
  if (request !== queueRequest) return; // A slower previous search cannot overwrite a new page.
  if(offset!==page.offset && location.hash.split('?')[0]==='#input')history.replaceState(null,'',pageURL('input',{q:query,offset:page.offset}));
  offset = page.offset;
  const pageKey = JSON.stringify([query, offset]);
  if (pageKey !== queuePageKey) { providerExpanded.clear(); queuePageKey = pageKey; }
  queueLoadedAt = Date.now();
  // Unchanged polls must not replace controls while someone is reading/clicking.
  const signature = JSON.stringify(page);
  if (signature === queueRendered) return;
  queueRendered = signature;
  const root = $('#queue-list'); root.replaceChildren();
  const groups = new Map();
  for (const a of page.items) {
    if (!groups.has(a.provider)) {
      const d = el('details'), summary = el('summary');
      d.open = providerExpanded.get(a.provider) !== false;
      d.ontoggle = () => { if (d.isConnected) providerExpanded.set(a.provider, d.open); };
      d.append(summary); root.append(d);
      groups.set(a.provider, { details: d, summary, count: 0 });
    }
    const row = el('div', undefined, 'roster-row');
    row.dataset.accountId = a.id;
    const title=el('strong');title.append(idLink('account',a.id),document.createTextNode(' · '+a.state));
    row.append(title, el('small', `Added ${date(a.created)} · ${a.streams} known streams · ${a.roster} roster places`));
    const group = groups.get(a.provider);
    group.count++;
    group.summary.textContent = `${a.provider} · ${group.count} ${group.count === 1 ? 'account' : 'accounts'} on this page`;
    group.details.append(row);
  }
  if (!page.items.length) root.append(el('p', /^(account|stream)\s*[:#]/i.test(query) ? 'This account/stream has been removed or is unavailable.' : 'No matching accounts.'));
  if (/^(account|stream)\s*[:#]/i.test(query)) root.querySelectorAll('.roster-row').forEach(row => row.classList.add('search-match'));
  const previous=button('Previous',async()=>{location.hash=pageURL('input',{q:query,offset:Math.max(0,offset-50)})}); previous.disabled=offset===0;
  const next=button('Next',async()=>{location.hash=pageURL('input',{q:query,offset:offset+50})}); next.disabled=!page.has_next;
  $('#queue-pages').replaceChildren(previous,el('span',`${page.total ? offset+1 : 0}–${offset+page.items.length} of ${page.total} accounts`),next);
}
export async function refresh() {
  const [s, j] = await Promise.all([api('/api/roster?queue=1'), api('/api/input/import')]);
  const root = $('#input-summary');
  root.replaceChildren();
  for (const [n,label] of [[s.queued_accounts,'Accounts waiting in queue'],[s.queued_streams,'Known queued streams'],[s.ready_streams,'Verified roster places'],[s.deficit,'Roster places to fill'],[s.accounts_retired_24h,'Accounts retired in last 24 hours']]) {
    const box=el('article',undefined,'status-metric'); box.append(el('strong',compactNumber(n||0)),el('span',label));root.append(box);
    if(label.includes('retired'))help(box,'Automatically removed accounts, including accounts without remaining matches. Counts cover the last 24 hours.','Accounts retired');
  }
  if(s.retired_details_missing_24h)root.append(el('p', `${s.retired_details_24h} retained deletion notices; ${s.retired_details_missing_24h} deletions lack retained detail. Totals include all automatic deletions in the time window.`, 'muted status-explanation'));
  let text = j.state === 'running' ? 'Reading your list in the background…' : j.state === 'imported' ? `Import complete · ${j.processed} entries processed.` : j.state === 'idle' ? '' : j.summary;
  try {
    const v = JSON.parse(j.summary);
    if (v.warnings?.length) text += ' ' + v.warnings.join(' ')
  } catch {}
  $('#import-result').textContent = text;
  const version = String(j.job_id) + ':' + j.state;
  if (lastImport !== version && j.state === 'imported') {
    offset = 0;
    await queue()
  }
  lastImport = version;
  if (Date.now()-queueLoadedAt>15000 && $('#queue-list').closest('details').open) await queue()
}

export async function route() {
  const p=routeParams();query=p.get('q')||'';offset=Math.max(0,Number.parseInt(p.get('offset')||'0',10)||0);
  $('#queue-filter').elements.q.value=query;
  if(query)$('#queue-list').closest('details').open=true;
  await queue();
}
