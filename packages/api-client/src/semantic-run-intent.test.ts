import {it,expect,vi} from 'vitest';
import {decodeSemanticRunIntent,readSemanticRunIntent} from './semantic-close';
const tenant='11111111-1111-1111-1111-111111111111',run='22222222-2222-2222-2222-222222222222',workflow='33333333-3333-3333-3333-333333333333',intent='44444444-4444-4444-4444-444444444444';
const response=()=>({run_id:run,workflow_id:workflow,intent_id:null as string|null,revision:'9007199254740993',state:'queued'});
it('preserves pending identity and abort signal without a mutation',async()=>{
 const signal=new AbortController().signal,request=vi.fn().mockResolvedValue(response());
 expect(await readSemanticRunIntent({request},tenant,run,signal)).toEqual(response());
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${tenant}/runs/${run}/semantic-intent`,{signal});
});
it('accepts independently generated intent and workflow identities',()=>{
 expect(decodeSemanticRunIntent({...response(),intent_id:intent,state:'waiting_review'}).intent_id).toBe(intent);
});
it.each([{intent_id:workflow.slice(1)},{revision:'9223372036854775808'},{state:'draft'},{extra:true},{intent_id:undefined}])('rejects malformed resolution %j',change=>{
 expect(()=>decodeSemanticRunIntent({...response(),...change})).toThrow('SEMANTIC_RUN_INTENT_INVALID');
});
it('rejects another parent response',async()=>{
 await expect(readSemanticRunIntent({request:vi.fn().mockResolvedValue({...response(),run_id:workflow})},tenant,run)).rejects.toThrow('SEMANTIC_RUN_INTENT_INVALID');
});
