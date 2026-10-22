import {expect,it} from 'vitest';
import {decodeSemanticReviewStatus} from './semantic-close';
const status={run_id:'11111111-1111-4111-8111-111111111111',workflow_id:'22222222-2222-4222-8222-222222222222',revision:'1',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:1,can_close:false};
it('preserves legacy status without inferring kind',()=>expect(decodeSemanticReviewStatus(status).kind).toBeUndefined());
it.each(['remove_object','local_exposure','background_cleanup','retouch_region'])('reads verified kind %s',kind=>expect(decodeSemanticReviewStatus({...status,kind}).kind).toBe(kind));
it.each([null,1,'unknown',''])('rejects invalid optional kind %s',kind=>expect(()=>decodeSemanticReviewStatus({...status,kind})).toThrow('SEMANTIC_REVIEW_STATUS_INVALID'));
it('does not allow extra fields alongside kind',()=>expect(()=>decodeSemanticReviewStatus({...status,kind:'background_cleanup',extra:true})).toThrow('SEMANTIC_REVIEW_STATUS_INVALID'));
