// UI helpers are pure; these tests require Node only for development/CI.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {compactNumber} from '../internal/admin/ui/numbers.js';
test('compact counts preserve scale and rounding across large catalogues',()=>{
 for(const [n,want] of [[0,'0'],[999,'999'],[1000,'1.0K'],[1400,'1.4K'],[99999,'100K'],[100000,'100K'],[140000,'140K'],[1400000,'1M'],[1400000000,'1B'],[1400000000000,'1T']])assert.equal(compactNumber(n),want,String(n));
 assert.equal(compactNumber(NaN),'—');assert.equal(compactNumber(Infinity),'—');
});

import {withDeadline} from '../internal/admin/ui/request.js';
test('hung requests time out and release the refresh loop',async()=>{
 await assert.rejects(withDeadline(signal=>new Promise((resolve,reject)=>{
  signal.addEventListener('abort',()=>reject(new Error('aborted')));
 }),10),/Request timed out/);
 assert.equal(await withDeadline(async()=>42,10),42);
 await assert.rejects(withDeadline(async()=>{throw new Error('original')},10),/original/);
});

import {profilePath, profileChannels, profileLabel} from '../internal/admin/ui/profiles.js';
test('lineup operations always name their tuner, including reserved characters', () => {
 assert.equal(profilePath('/api/lineup', 'uk-london'), '/api/lineup?profile_id=uk-london');
 assert.equal(profilePath('/api/channels?enabled=1', 'uk & london'), '/api/channels?enabled=1&profile_id=uk+%26+london');
 assert.equal(profilePath('/api/lineup?profile_id=primary', 'london'), '/api/lineup?profile_id=london');
 assert.throws(() => profilePath('/api/channels/bulk', ''), /Choose a tuner/);
 assert.throws(() => profilePath('/api/lineup', undefined), /Choose a tuner/);
});
test('overlapping channel numbers stay in their own market', () => {
 const channels = [
  {id:'ny-cbs', profile_id:'primary', number:'101'},
  {id:'london-bbc', profile_id:'london', number:'101'},
  {id:'orphan', number:'101'}
 ];
 assert.deepEqual(profileChannels(channels, 'primary').map(c=>c.id), ['ny-cbs']);
 assert.deepEqual(profileChannels(channels, 'london').map(c=>c.id), ['london-bbc']);
 assert.deepEqual(profileChannels(channels, 'missing'), []);
 const profiles=[{id:'primary',name:'USA - New York City'},{id:'london',name:'UK - London'}];
 assert.equal(profileLabel(profiles,'london'),'UK - London');
 assert.equal(profileLabel(profiles,'missing'),'Unavailable tuner');
});

import {currentStep, tunerOptions} from '../internal/admin/ui/setup.js';
test('setup lists installed tuners and only the city presets still available to add', () => {
  const profiles = [
    {id:'primary',name:'My tuner',enabled:true,preset_id:''},
    {id:'local-uk',name:'My London lineup',enabled:false,preset_id:'uk-london'}
  ];
  const presets = [
    {id:'uk-london',name:'London',channels:5},
    {id:'in-mumbai',name:'Mumbai',channels:10}
  ];
  const options = tunerOptions(profiles, presets);
  assert.deepEqual(options.map(({value, label}) => [value, label]), [
    ['profile:primary', 'My tuner'], ['profile:local-uk', 'My London lineup'], ['preset:in-mumbai', 'Mumbai']
  ]);
  assert.match(options[1].detail, /enables this tuner/);
  assert.match(options[2].detail, /10 channels/);
});

test('setup resumes the saved step only while its selected tuner and source requirements hold', () => {
  const profiles = [{id:'primary',enabled:true},{id:'second',enabled:false}];
  const saved = {state:'active',step:'plex',profile_id:'primary',has_sources:true};
  assert.equal(currentStep(saved, profiles), 'plex');
  assert.equal(currentStep({...saved, step:'tuner'}, profiles), 'tuner');
  assert.equal(currentStep({...saved, step:'sources'}, profiles), 'sources');
  assert.equal(currentStep({...saved, has_sources:false}, profiles), 'sources');
  for (const id of ['second','removed','']) assert.equal(currentStep({...saved, profile_id:id}, profiles), 'tuner');
  for (const state of ['complete','dismissed']) assert.equal(currentStep({...saved, state}, profiles), '');
  assert.equal(currentStep({state:'pending',step:'tuner',profile_id:'',has_sources:false}, []), 'tuner');
});

import {bindImport, showImport} from '../internal/admin/ui/source-import.js';
test('shared source form retains failed lists and prevents stale polls from reopening a finished job', async t => {
  const submit = {}, result = {textContent:''};
  const form = {
    elements:{text:{value:'list-one'},authorised:{checked:true}},
    reportValidity:() => true,
    querySelector:() => submit
  };
  const originalDocument = globalThis.document;
  globalThis.document = {querySelector:selector => selector === '#import-form' ? form : result};
  t.after(() => { if (originalDocument) globalThis.document = originalDocument; else delete globalThis.document; });
  let id = 0;
  t.mock.method(globalThis, 'fetch', async (path, options) => {
    assert.equal(path, '/api/import');
    assert.deepEqual(JSON.parse(options.body), {text:form.elements.text.value,authorised:true});
    return Response.json({job_id:++id,state:'running'}, {status:202});
  });
  bindImport();
  await form.onsubmit({preventDefault(){}});
  assert.equal(submit.disabled, true);
  showImport({job_id:1,state:'failed',summary:'Could not read list.'});
  assert.equal(form.elements.text.value, 'list-one');
  assert.equal(result.textContent, 'Could not read list.');
  assert.equal(submit.disabled, false);
  await form.onsubmit({preventDefault(){}});
  showImport({job_id:2,state:'imported',processed:1,summary:'{"accounts_added":1,"accounts_existing":0,"duplicates":0}'});
  assert.equal(form.elements.text.value, '');
  assert.match(result.textContent, /1 new account/);
  showImport({job_id:2,state:'running'});
  showImport({job_id:1,state:'failed',summary:'Old error'});
  assert.equal(submit.disabled, false);
  assert.match(result.textContent, /Import complete/);
});

import {listenerLabels, slotHelp} from '../internal/admin/ui/listeners.js';
test('the web UI reports configured ports and reserved capacity', () => {
  const range = {port_range:'40100-40106',admin_port:40100,tuner_ports:[40101,40102,40103,40104,40105,40106],available_slots:1};
  assert.deepEqual(listenerLabels(range), {range:'40100–40106',admin:'40100',tuners:'40101–40106',capacity:'1 of 6 tuner slots available.'});
  assert.match(slotHelp(range), /disabled/);
  assert.match(slotHelp({...range,available_slots:0}), /free a slot/);
  const legacy = {port_range:'',admin_port:0,tuner_ports:[9192,9200,9201,9202],available_slots:0};
  assert.deepEqual(listenerLabels(legacy), {range:'Individual ports (legacy configuration)',admin:'Shared with the primary tuner on 9192',tuners:'9192, 9200–9202',capacity:'0 of 4 tuner slots available.'});
  assert.match(slotHelp(legacy), /free a slot/);
});
test('a full allocation still lets setup enable an existing disabled tuner', () => {
  const profiles = [{id:'my-tuner',name:'Local lineup',preset_id:'',enabled:false}];
  const presets = [{id:'in-mumbai',name:'Mumbai',channels:23}];
  const full = tunerOptions(profiles,presets,0);
  assert.equal(!!full[0].disabled, false);
  assert.equal(full[1].disabled, true);
  assert.equal(tunerOptions(profiles,presets,1)[1].disabled, false);
});

import {clientAddresses} from '../internal/admin/ui/connections.js';
import {matchesChannelFilter,standbyLabel} from '../internal/admin/ui/guide.js';

test('client setup uses only the selected tuner address',()=>{
  assert.deepEqual(clientAddresses({tuner_url:'https://tuner.local:39193/'}),{
    plex:'https://tuner.local:39193',playlist:'https://tuner.local:39193/playlist.m3u',guide:'https://tuner.local:39193/guide.xml'
  });
  assert.deepEqual(clientAddresses({}),{plex:'',playlist:'',guide:''});
});
test('a playing channel needs a receiving standby even with a full roster',()=>{
  assert.equal(matchesChannelFilter('backup',null,{count:6},{standby:'checking'}),true);
  assert.equal(matchesChannelFilter('backup',null,{count:6},undefined),false);
  assert.equal(standbyLabel({count:9}, {standby:'receiving'}),'Standby live');
  assert.equal(standbyLabel({count:3},null),'Backup candidate');
  assert.equal(matchesChannelFilter('missing',undefined,{count:3},null),true);
  assert.equal(matchesChannelFilter('missing',{state:'confirmed'},{count:0},null),false);
  assert.equal(matchesChannelFilter('conflict',{state:'conflict'},null,null),true);
});

test('failed tuner switches keep old controls hidden and retry from normal polling', async t => {
  const {route,refresh} = await import('../internal/admin/ui/output.js?retry');
  const nodes = new Map();
  t.mock.method(globalThis,'fetch',async path=>{
    const url = new URL(path,'http://ui.test');
    if(url.pathname === '/api/guide') {
      attempts.push(url.searchParams.get('profile_id'));
      return new Response('Temporary failure',{status:503});
    }
    const data = {'/api/profiles':[{id:'other',enabled:true}],'/api/presets':[],'/api/listeners':{},
      '/api/roster':{channels:[]},'/api/channels':[],'/api/monitor':{live:[]}};
    return Response.json(data[url.pathname]);
  });
  const attempts = [], previousLocation=globalThis.location, previousDocument=globalThis.document;
  globalThis.location = {hash:'#output?tuner=other'};
  globalThis.document = {querySelector(key){
    if (!nodes.has(key)) nodes.set(key,{});
    return nodes.get(key);
  }};
  t.after(()=>{
    if(previousLocation) globalThis.location=previousLocation; else delete globalThis.location;
    if(previousDocument) globalThis.document=previousDocument; else delete globalThis.document;
  });
  for(const run of [route,refresh,route]) await assert.rejects(run(),/Temporary failure/);
  assert.deepEqual(attempts,['other','other','other']);
  assert.equal(nodes.get('#tuner-view').hidden,true);
  assert.match(nodes.get('#tuner-load-state').textContent,/Retrying/);
});

test('setup retries a failed return to Status and leaves completed setup idle', async t => {
  const {visit, poll} = await import('../internal/admin/ui/setup.js?return-retry');
  const nodes = new Map();
  const node = key => {
    if (!nodes.has(key)) nodes.set(key, {hidden:false, prepend(){}, append(){}, querySelector(){return {};}});
    return nodes.get(key);
  };
  const previous = globalThis.document;
  globalThis.document = {querySelector:node};
  t.after(()=>{if(previous) globalThis.document=previous; else delete globalThis.document;});
  const saved = {state:'active',step:'sources',profile_id:'one',has_sources:false};
  let attempts = 0;
  t.mock.method(globalThis,'fetch',async path=>{
    if(path === '/api/setup') {
      if([2,4,7].includes(++attempts)) return new Response('Temporarily unavailable',{status:503});
      return Response.json(attempts < 6 ? saved : {state:'complete'});
    }
    return Response.json({'/api/profiles':[{id:'one',enabled:true}],'/api/presets':[],
      '/api/listeners':{},'/api/input/import':{}}[path]);
  });
  const settled = ()=>new Promise(resolve=>setImmediate(resolve));
  visit('status'); await settled();
  assert.equal(node('#setup-source-hint').textContent,'Import an authorised list or add your portal account to continue.');
  visit('output'); visit('status'); await settled();
  assert.equal(node('#setup-guide').hidden,true);
  assert.match(node('#setup-outcome').textContent,/Temporarily unavailable/);
  await poll();
  assert.equal(attempts,3);
  assert.equal(node('#setup-guide').hidden,false);
  assert.equal(node('#setup-outcome').textContent,'');
  await poll();
  assert.match(node('#setup-message').textContent,/Temporarily unavailable/);
  await poll();
  assert.equal(node('#setup-message').textContent,'');
  await poll();
  assert.equal(node('#setup-guide').hidden,true);
  node('#setup-outcome').textContent = 'Setup finished.';
  await poll();
  assert.equal(attempts,6);
  assert.equal(node('#setup-outcome').textContent,'Setup finished.');
  visit('output'); visit('status'); await settled();
  assert.match(node('#setup-outcome').textContent,/Temporarily unavailable/);
  await poll();
  assert.equal(node('#setup-outcome').textContent,'');
  assert.equal(attempts,8);
});
