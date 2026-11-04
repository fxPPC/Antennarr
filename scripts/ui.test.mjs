// UI helpers are pure; these tests require Node only for development/CI.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {compactNumber} from '../internal/admin/ui/numbers.js';
test('compact counts preserve scale and rounding across large catalogues',()=>{
 for(const [n,want] of [[0,'0'],[999,'999'],[1000,'1.0K'],[1400,'1.4K'],[99999,'100K'],[100000,'100K'],[140000,'140K'],[1400000,'1M'],[1400000000,'1B'],[1400000000000,'1T']])assert.equal(compactNumber(n),want,String(n));
 assert.equal(compactNumber(NaN),'—');assert.equal(compactNumber(Infinity),'—');
});
