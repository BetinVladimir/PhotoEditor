import {expect,it,vi} from 'vitest';
import {closeSemanticReview,decodeSemanticReviewClosed,decodeSemanticReviewStatus} from './semantic-close';
const tenant='11111111-1111-1111-1111-111111111111',intent='22222222-2222-2222-2222-222222222222';
const response={run_id:tenant,workflow_id:intent,revision:'9007199254740991',state:'completed',outputs:[],error_code:null};
it.each(['9007199254740991','9007199254740992','9223372036854775806'])('refuses nonincrementable close revision %s before transport',async revision=>{
 const request=vi.fn();
 await expect(closeSemanticReview({request},tenant,intent,{expected_revision:revision,plan_sha:'a'.repeat(64)},'key')).rejects.toThrow('SEMANTIC_CLOSE_REQUEST_INVALID');
 expect(request).not.toHaveBeenCalled();
});
it.each(['1','9007199254740992','9223372036854775807'])('refuses invalid terminal revision %s',revision=>{
 expect(()=>decodeSemanticReviewClosed({...response,revision})).toThrow('SEMANTIC_CLOSE_RESPONSE_INVALID');
});
it('allows terminal safe maximum while disabling further close authority',()=>{
 expect(decodeSemanticReviewClosed(response).revision).toBe('9007199254740991');
 const status={run_id:tenant,workflow_id:intent,revision:'9007199254740991',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:0,can_close:false};
 expect(decodeSemanticReviewStatus(status).can_close).toBe(false);
 expect(()=>decodeSemanticReviewStatus({...status,can_close:true})).toThrow('SEMANTIC_REVIEW_STATUS_INVALID');
});
it('freezes request while transport waits',async()=>{
 const body={expected_revision:'2',plan_sha:'a'.repeat(64)};
 let complete!:(value:unknown)=>void;
 const request=vi.fn().mockImplementation(()=>new Promise(resolve=>{complete=resolve}));
 const pending=closeSemanticReview({request},tenant,intent,body,'key');
 body.expected_revision='4';body.plan_sha='b'.repeat(64);
 complete({...response,revision:'3'});
 await expect(pending).resolves.toMatchObject({revision:'3'});
 expect(request.mock.calls[0][1].body).toEqual({expected_revision:'2',plan_sha:'a'.repeat(64)});
});
