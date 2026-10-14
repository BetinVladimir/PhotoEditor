import {readFileSync} from 'node:fs';
import {it,expect,vi} from 'vitest';
import {applySemanticReview,type SemanticApplyRequest} from './semantic-apply';
const captured=process.env.PHOTO_TEST_V27_APPLY_RESPONSE_FILE;
it.skipIf(!captured)('submits the real native HTTP Apply request and decodes its admitted child',async()=>{
 const fixture=JSON.parse(readFileSync(captured!,'utf8')) as {tenant:string;intent:string;key:string;request:SemanticApplyRequest;response:unknown};
 const request=vi.fn().mockResolvedValue(fixture.response);
 const result=await applySemanticReview({request},fixture.tenant,fixture.intent,fixture.request,fixture.key);
 expect(result.child?.state).toBe('queued');expect(result.remaining).toBe(0);
 expect(result.revision).toBe((BigInt(fixture.request.expected_revision)+1n).toString());
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${fixture.tenant}/semantic-intents/${fixture.intent}/apply`,expect.objectContaining({method:'POST',body:fixture.request,idempotencyKey:fixture.key}));
});
