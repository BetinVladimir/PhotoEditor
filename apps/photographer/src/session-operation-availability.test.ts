import {expect,it,vi} from 'vitest';
import {requireSessionAnalysisAvailable} from './session-operation-availability';
const ready=(operation:string)=>({operation,resource_class:'cpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready' as const});
it('requires both descriptor and plan deployment capabilities',async()=>{
 const signal=new AbortController().signal;
 for(const operation of ['session.describe','session.plan']){
  const api={capabilities:vi.fn(async()=>({operations:[ready(operation)]}))};
  await expect(requireSessionAnalysisAvailable(api,signal)).rejects.toMatchObject({status:503});
 }
 await expect(requireSessionAnalysisAvailable({capabilities:async()=>({operations:[ready('session.describe'),ready('session.plan')]})},signal)).resolves.toBeUndefined();
});
it('rejects a late capability response after scope cancellation',async()=>{
 const controller=new AbortController();
 const api={capabilities:async()=>{controller.abort();return {operations:[ready('session.describe'),ready('session.plan')]}}};
 await expect(requireSessionAnalysisAvailable(api,controller.signal)).rejects.toMatchObject({name:'AbortError'});
});
