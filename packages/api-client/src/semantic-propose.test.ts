import {it,expect,vi} from 'vitest';
import {proposeSemanticIntent,decodeSemanticEditIntent,type SemanticEditIntent} from './semantic-propose';
const id='11111111-1111-4111-8111-111111111111',sha='a'.repeat(64);
const input:SemanticEditIntent={schema_version:1,kind:'local_exposure',reference:{asset_version_id:id,source:{artifact_id:id,sha256:sha},canvas_hash:sha},reference_mask:{artifact_id:id,sha256:sha},target_snapshot_id:id,strength:.5,ev:0,preserve_persons:true,prompt:null};
it('posts an owned intent and validates the queued ticket',async()=>{
 const ticket={run_id:id,workflow_id:id,state:'queued'},request=vi.fn().mockResolvedValue(ticket),signal=new AbortController().signal;
 const result=await proposeSemanticIntent({request},id,input,'stable-key',signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id}/semantic-intents`,{method:'POST',body:input,idempotencyKey:'stable-key',signal});
 expect(request.mock.calls[0][1].body).not.toBe(input);expect(result).toEqual(ticket);expect(result).not.toBe(ticket);
});
it.each([{ev:null},{strength:NaN},{preserve_persons:false},{prompt:''},{prompt:'\ud800'},{prompt:'😀'.repeat(1025)},{reference:{...input.reference,extra:1}},{kind:'remove_object',ev:0},{extra:1}])('rejects invalid intent before network IO: %j',async(change)=>{
 const request=vi.fn();await expect(proposeSemanticIntent({request},id,{...input,...change} as SemanticEditIntent,'key')).rejects.toThrow('SEMANTIC_PROPOSE_REQUEST_INVALID');expect(request).not.toHaveBeenCalled();
});
it('accepts a 1024-codepoint prompt and all non-exposure kinds with null EV',()=>{
 expect(decodeSemanticEditIntent({...input,prompt:'😀'.repeat(1024)}).prompt).toBeTruthy();
 for(const kind of ['remove_object','background_cleanup','retouch_region'])expect(decodeSemanticEditIntent({...input,kind,ev:null}).kind).toBe(kind);
});
it('rejects malformed job tickets',async()=>{
 const request=vi.fn().mockResolvedValue({run_id:id,workflow_id:id,state:'completed'});
 await expect(proposeSemanticIntent({request},id,input,'key')).rejects.toThrow('SEMANTIC_PROPOSE_RESPONSE_INVALID');
});
