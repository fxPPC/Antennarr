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
