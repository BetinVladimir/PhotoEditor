import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {RelightIntent} from './relight-intent';
import type {RelightAnalysisIntent} from './relight-analysis';

const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
function fixtures(){
 const ref={artifact_id:id,sha256:'a'.repeat(64)},base={asset_version_id:id,source:ref,canvas_hash:'b'.repeat(64)};
 const relight:RelightIntent={schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',params:{schema_version:1,base,strength:.5,depth:ref,subject:ref,protected:ref,calibration:ref,lights:[{x:.5,y:.5,radius:.3,ev:0}]}};
 const analysis:RelightAnalysisIntent={schema_version:1,snapshot_id:id,base,operation:'depth.predict'};
 return {relight,analysis};
}
for(const lane of ['relight','analysis'] as const){
 for(const replayOnly of [false,true]){
  it(`${lane} transport freezes original request/key and uses explicit replay-only header: ${replayOnly}`,async()=>{
   let finish!:(value:unknown)=>void;
   const request=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
   const api=new OwnerAPI({request} as unknown as ApiClient,other),signal=new AbortController().signal;
   const {relight,analysis}=fixtures(),body=lane==='relight'?relight:analysis,expected=structuredClone(body);
   const pending=lane==='relight'?api.submitRelight(id,relight,'original-key',signal,replayOnly):api.submitRelightAnalysis(id,analysis,'original-key',signal,replayOnly);
   body.snapshot_id=other;body.schema_version=1;
   const [path,options]=request.mock.calls[0];
   expect(path).toBe(`/api/v1/tenants/${other}/projects/${id}/${lane==='relight'?'relight-intents':'relight-analyses'}`);
   expect(options).toEqual({method:'POST',body:expected,idempotencyKey:'original-key',signal,...(replayOnly?{headers:{'Relight-Replay-Only':'true'}}:{})});
   const ticket={run_id:id,workflow_id:other,state:'queued'};finish(ticket);expect(await pending).toEqual(ticket);
  });
 }
 it(`${lane} replay does not turn malformed acknowledgement into accepted ticket`,async()=>{
  const request=vi.fn().mockResolvedValue({run_id:id,workflow_id:other,state:'completed'}),api=new OwnerAPI({request} as unknown as ApiClient,other);
  const {relight,analysis}=fixtures();
  await expect(lane==='relight'?api.submitRelight(id,relight,'original-key',undefined,true):api.submitRelightAnalysis(id,analysis,'original-key',undefined,true)).rejects.toThrow();
  expect(request).toHaveBeenCalledOnce();
 });
}
