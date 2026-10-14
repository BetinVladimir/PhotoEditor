import {expect,it} from 'vitest';
import {freezeRelightAnalysisIntent,type RelightAnalysisIntent} from './relight-analysis';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id='11111111-1111-4111-8111-111111111111';
function intent():RelightAnalysisIntent{return {schema_version:1,snapshot_id:id,base:{asset_version_id:id,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},operation:'depth.predict'};}
it('owns frozen public intent and refuses private pins',()=>{
 const original=intent(),frozen=freezeRelightAnalysisIntent(original);original.base.source.sha256='c'.repeat(64);expect(frozen.base.source.sha256).toBe('a'.repeat(64));
 for(const key of ['qualification','model_lock_sha','resource_estimate','tenant_id','run_id'])expect(()=>freezeRelightAnalysisIntent({...intent(),[key]:'injected'} as RelightAnalysisIntent)).toThrow();
 expect(()=>freezeRelightAnalysisIntent({...intent(),operation:'image.relight'} as unknown as RelightAnalysisIntent)).toThrow();
});
it('sends scoped frozen body and explicit idempotency key',async()=>{
 const calls:Array<{path:string;options:any}>=[];
 const client={request:async(path:string,options:any)=>{calls.push({path,options});return {run_id:id,workflow_id:id,state:'queued'};}} as unknown as ApiClient;
 const api=new OwnerAPI(client,id),request=intent();
 const result=await api.submitRelightAnalysis(id,request,'analysis:stable-key');
 expect(result.run_id).toBe(id);expect(calls[0].path).toBe(`/api/v1/tenants/${id}/projects/${id}/relight-analyses`);expect(calls[0].options.idempotencyKey).toBe('analysis:stable-key');expect(calls[0].options.body).toEqual(request);
 request.base.source.sha256='d'.repeat(64);expect(calls[0].options.body.base.source.sha256).toBe('a'.repeat(64));
 await expect(api.submitRelightAnalysis(id,intent(),'bad key')).rejects.toThrow();expect(calls).toHaveLength(1);
});
