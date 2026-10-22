import {expect,it,vi} from 'vitest';
import {readSessionAnalysisCapabilities,sessionAnalysisRequestSHA} from './session-analysis-capabilities';
import {OwnerAPI} from './owner-api';
import type {SceneAnalysisRequest} from './scene-analysis-request';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request:SceneAnalysisRequest={expected_revision:'4',selection_snapshot_id:id(5),policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(6),sha256:'a'.repeat(64)}},manual_references:[]};
const now=Date.now(),scope={tenant:id(1),session:id(2),requestSHA:sessionAnalysisRequestSHA(request)};
const result=()=>({schema_version:1,tenant_id:id(1),project_id:id(3),session_id:id(2),request_sha:scope.requestSHA,model_lock_sha:'b'.repeat(64),expires_at:new Date(now+15000).toISOString()});
it('binds exact policy, snapshot and manual choices, rejects stale or foreign advisories',()=>{
 expect(readSessionAnalysisCapabilities(result(),scope,now).project_id).toBe(id(3));
 for(const patch of [{tenant_id:id(9)},{session_id:id(9)},{project_id:'forged'},{request_sha:'c'.repeat(64)},{model_lock_sha:'bad'},{expires_at:new Date(now).toISOString()},{expires_at:new Date(now+60000).toISOString()},{extra:true}])expect(()=>readSessionAnalysisCapabilities({...result(),...patch},scope,now)).toThrow();
 for(const changed of [{...request,expected_revision:'5'},{...request,selection_snapshot_id:id(9)},{...request,manual_references:[{previous_analysis_id:id(7),group_id:id(8),asset_version_id:id(9)}]}])expect(sessionAnalysisRequestSHA(changed)).not.toBe(scope.requestSHA);
});
it('freezes the advisory POST body and refuses late cancellation without accepting a response',async()=>{
 let finish!:(value:unknown)=>void;const transport=vi.fn((..._args:unknown[])=>new Promise(resolve=>{finish=resolve})),api=new OwnerAPI({request:transport} as never,id(1)),controller=new AbortController(),body=structuredClone(request);
 const pending=api.sessionAnalysisCapabilities(id(2),body,controller.signal);body.expected_revision='5';
 expect(transport.mock.calls[0]?.[1]).toMatchObject({method:'POST',body:request});controller.abort();finish(result());await expect(pending).rejects.toMatchObject({name:'AbortError'});
 await expect(api.sessionAnalysisCapabilities(id(2),request,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(transport).toHaveBeenCalledOnce();
});
