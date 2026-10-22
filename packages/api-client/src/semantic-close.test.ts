import {describe,it,expect,vi} from 'vitest';
import {closeSemanticReview,decodeSemanticReviewClosed} from './semantic-close';
const tenant='11111111-1111-1111-1111-111111111111',intent='22222222-2222-2222-2222-222222222222';
const body={expected_revision:'9007199254740990',plan_sha:'a'.repeat(64)};
const response=()=>({run_id:tenant,workflow_id:intent,revision:'9007199254740991',state:'completed',outputs:[],error_code:null});
describe('semantic close',()=>{
 it('preserves safe boundary revision and sends retry identity and cancellation',async()=>{
  const signal=new AbortController().signal;
  const request=vi.fn().mockResolvedValue(response());
  const result=await closeSemanticReview({request},tenant,intent,body,'stable-key',signal);
  expect(result.revision).toBe('9007199254740991');
  expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${tenant}/semantic-intents/${intent}/close-review`,{method:'POST',body,idempotencyKey:'stable-key',signal});
 });
 it.each(['scope','overflow','key','extra'])('rejects %s before IO',async(kind)=>{
  const request=vi.fn();const changed={...body};
  if(kind==='overflow')changed.expected_revision='9223372036854775807';
  if(kind==='extra')Object.assign(changed,{target_id:intent});
  await expect(closeSemanticReview({request},kind==='scope'?'bad':tenant,intent,changed,kind==='key'?'bad key':'key')).rejects.toThrow('SEMANTIC_CLOSE_REQUEST_INVALID');
  expect(request).not.toHaveBeenCalled();
 });
 it.each(['state','outputs','error','missing','overflow','child'])('rejects %s response',kind=>{
  const changed:Record<string,unknown>=response();
  if(kind==='state')changed.state='waiting_review';
  if(kind==='outputs')changed.outputs=null;
  if(kind==='error')changed.error_code='failed';
  if(kind==='missing')delete changed.error_code;
  if(kind==='overflow')changed.revision='9223372036854775808';
  if(kind==='child')changed.child={run_id:tenant};
  expect(()=>decodeSemanticReviewClosed(changed)).toThrow('SEMANTIC_CLOSE_RESPONSE_INVALID');
 });
 it('rejects a success for another revision',async()=>{
  const request=vi.fn().mockResolvedValue({...response(),revision:'2'});
  await expect(closeSemanticReview({request},tenant,intent,body,'key')).rejects.toThrow('SEMANTIC_CLOSE_RESPONSE_INVALID');
 });
});

describe('review parent status',()=>{
 it('keeps resource intent ID distinct from returned workflow identity',async()=>{
  const {readSemanticReviewStatus}=await import('./semantic-close');
  const request=vi.fn().mockResolvedValue({run_id:tenant,workflow_id:tenant,revision:'1',state:'queued',plan_sha:null,remaining:0,can_close:false});
  expect((await readSemanticReviewStatus({request},tenant,intent)).workflow_id).toBe(tenant);
 });
 it('rejects contradictory closure authority',async()=>{
  const {decodeSemanticReviewStatus}=await import('./semantic-close');
  const valid={run_id:tenant,workflow_id:intent,revision:body.expected_revision,state:'waiting_review',plan_sha:body.plan_sha,remaining:0,can_close:true};
  expect(decodeSemanticReviewStatus(valid).revision).toBe(body.expected_revision);
  for(const change of [{remaining:1},{state:'completed'},{plan_sha:null},{revision:'9223372036854775807'},{remaining:5001},{object_key:'private'}])expect(()=>decodeSemanticReviewStatus({...valid,...change})).toThrow('SEMANTIC_REVIEW_STATUS_INVALID');
 });
});
