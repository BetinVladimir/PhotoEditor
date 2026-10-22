import {it,expect,vi} from 'vitest';
import {applySemanticReview,decodeSemanticApplyResult} from './semantic-apply';
const id='11111111-1111-4111-8111-111111111111';
const request=()=>({expected_revision:'9007199254740990',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'approve' as const}]});
const response=()=>({workflow_id:id,revision:'9007199254740991',remaining:0,batch_id:id,child:{run_id:id,state:'queued'}});
it('owns decisions and carries retry key and cancellation',async()=>{
 const input=request(),signal=new AbortController().signal;
 const transport=vi.fn().mockImplementation(async()=>{input.target_decisions.length=0;return response()});
 const result=await applySemanticReview({request:transport},id,id,input,'stable-key',signal);
 expect(result.child?.state).toBe('queued');
 expect(transport.mock.calls[0][1]).toEqual({method:'POST',body:request(),idempotencyKey:'stable-key',signal});
});
it.each(['duplicate','empty','extra','overflow','scope'])('rejects %s before IO',async(kind)=>{
 const input=request();if(kind==='duplicate')input.target_decisions.push(input.target_decisions[0]);if(kind==='empty')input.target_decisions=[];if(kind==='extra')Object.assign(input,{run_id:id});if(kind==='overflow')input.expected_revision='9223372036854775807';
 const transport=vi.fn();await expect(applySemanticReview({request:transport},kind==='scope'?'bad':id,id,input,'key')).rejects.toThrow('SEMANTIC_APPLY_REQUEST_INVALID');expect(transport).not.toHaveBeenCalled();
});
it('reject-only result has no child and preserves exact revision',async()=>{
 const input={...request(),target_decisions:[{target_id:id,decision:'reject' as const}]};
 const transport=vi.fn().mockResolvedValue({...response(),child:null});
 expect((await applySemanticReview({request:transport},id,id,input,'key')).child).toBeNull();
 transport.mockResolvedValue(response());await expect(applySemanticReview({request:transport},id,id,input,'key')).rejects.toThrow('SEMANTIC_APPLY_RESPONSE_INVALID');
});
it.each(['missing','state','remaining','revision','extra'])('rejects %s result',kind=>{
 const value:Record<string,unknown>=response();if(kind==='missing')delete value.child;if(kind==='state')value.child={run_id:id,state:'completed'};if(kind==='remaining')value.remaining=5001;if(kind==='revision')value.revision='9223372036854775808';if(kind==='extra')value.object_key='private';expect(()=>decodeSemanticApplyResult(value)).toThrow('SEMANTIC_APPLY_RESPONSE_INVALID');
});

it.each(['9007199254740991','9007199254740992','9223372036854775806'])('refuses non-incrementable revision %s before transport',async revision=>{
 const transport=vi.fn();await expect(applySemanticReview({request:transport},id,id,{...request(),expected_revision:revision},'key')).rejects.toThrow('SEMANTIC_APPLY_REQUEST_INVALID');expect(transport).not.toHaveBeenCalled();
});
it.each(['9007199254740992','9223372036854775807'])('refuses response revision outside SQL range %s',revision=>{
 expect(()=>decodeSemanticApplyResult({...response(),revision})).toThrow('SEMANTIC_APPLY_RESPONSE_INVALID');
});
