import {readFileSync} from 'node:fs';
import {it,expect} from 'vitest';
import {decodeRunOutputs} from './run-outputs';
const capture=process.env.PHOTO_TEST_V27_OUTPUTS_RESPONSE_FILE;
it.skipIf(!capture)('decodes real accepted Apply preview HTTP outputs',()=>{
 const fixture=JSON.parse(readFileSync(capture!,'utf8'));
 const page=decodeRunOutputs(fixture.response);
 expect(page.items.length===4).toBe(true);
 expect(page.items.filter(i=>i.role==='derivative').length===2).toBe(true);
 expect(page.items.every(i=>Date.parse(i.expires_at)>Date.now())).toBe(true);
});
