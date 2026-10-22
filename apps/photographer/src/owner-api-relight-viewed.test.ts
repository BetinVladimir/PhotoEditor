import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import type {Review} from './catalog-types';
import {OwnerAPI} from './owner-api';
const id='11111111-1111-4111-8111-111111111111';
const review:Review={id,run_id:id,kind:'image.relight',state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id,sha256:'a'.repeat(64)},base:{artifact_id:'22222222-2222-4222-8222-222222222222',sha256:'b'.repeat(64)},reasons:[],created_at:'2026-10-16T00:00:00Z'};
it('transports the displayed pair and forwards the resulting token on decision only',async()=>{
 const receipt={version:1,token:'viewed.signed',expires_at:new Date(Date.now()+60000).toISOString()};
 const request=vi.fn().mockResolvedValueOnce(receipt).mockResolvedValueOnce({...review,state:'decided',revision:2,decision:'accept',decision_route:'candidate',decision_hash:'c'.repeat(64)});
 const api=new OwnerAPI({request} as unknown as ApiClient,id),signal=new AbortController().signal;
 expect(await api.acknowledgeRelightViewed(review,'before.signed','after.signed',signal)).toEqual(receipt);
 expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${id}/reviews/${id}/relight-viewed`);
 expect(request.mock.calls[0][1]).toMatchObject({method:'POST',body:{before_token:'before.signed',after_token:'after.signed'},signal});
 await api.decide(review,'accept',[],[],'decision-key',signal,receipt.token);
 expect(request.mock.calls[1][1]).toMatchObject({body:{viewed_token:receipt.token,expected_revision:1,candidate_hash:review.candidate_hash,decision:'accept'},signal,idempotencyKey:'decision-key'});
});
it('refuses acknowledgement for a decided review before contacting the server',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id);
 await expect(api.acknowledgeRelightViewed({...review,state:'decided',revision:2,decision:'accept',decision_route:'candidate',decision_hash:'c'.repeat(64)},'before.signed','after.signed')).rejects.toThrow('PAIR_INVALID');
 expect(request).not.toHaveBeenCalled();
});
