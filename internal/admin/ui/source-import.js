import {$, api} from './core.js';

let submitting = false;
let lastJob = 0;
let lastState = '';
let submittedText;
let submittedJob;
let submitError = '';

// Both entry points use this form and the same asynchronous import job.
export function bindImport() {
  const form = $('#import-form');
  form.onsubmit = async event => {
    event.preventDefault();
    if (submitting || !form.reportValidity()) return;
    submitting = true;
    submitError = '';
    const submit = form.querySelector('button[type=submit]');
    submit.disabled = true;
    const text = form.elements.text.value;
    try {
      const job = await api('/api/import', 'POST', {text, authorised: form.elements.authorised.checked});
      submittedText = text;
      submittedJob = job.job_id;
      showImport(job);
    } catch (e) {
      submitError = e.message;
      $('#import-result').textContent = submitError;
    } finally {
      submitting = false;
      submit.disabled = lastState === 'running';
    }
  };
}

export function showImport(job) {
  const id = job.job_id || 0;
  // A status poll started before submission can return after the new job.
  if (id < lastJob || (id === lastJob && lastState && lastState !== 'running' && job.state === 'running')) return;
  if (id > lastJob) submitError = '';
  lastJob = id;
  lastState = job.state;
  const form = $('#import-form');
  form.querySelector('button[type=submit]').disabled = submitting || job.state === 'running';
  let message = job.summary || '';
  if (job.state === 'running') message = 'Reading your list in the background…';
  if (job.state === 'imported') {
    message = 'Import complete · ' + job.processed + (job.processed === 1 ? ' entry processed.' : ' entries processed.');
    let warnings = [];
    try {
      const counts = JSON.parse(job.summary);
      if (Number.isInteger(counts.accounts_added)) {
        const added = counts.accounts_added;
        message = 'Import complete · ' + added + (added === 1 ? ' new account' : ' new accounts') + ' · ' + counts.accounts_existing + ' already saved.';
      }
      if (counts.duplicates > 0) message += ' ' + counts.duplicates + (counts.duplicates === 1 ? ' duplicate entry.' : ' duplicate entries.');
      warnings = counts.warnings || [];
      if (warnings.length) message += ' ' + warnings.join(' ');
    } catch {}
    if (submittedJob === id) {
      if (!warnings.length && form.elements.text.value === submittedText) form.elements.text.value = '';
      submittedText = submittedJob = undefined;
    }
  }
  const result = $('#import-result');
  if (result.textContent !== (submitError || message)) result.textContent = submitError || message;
}
