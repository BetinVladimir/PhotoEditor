import {prepareEditorRenderFromServerPolicy} from './editor-render-submit';
import {it,expect,vi} from 'vitest';
import {readRenderPolicy} from './render-policy';
import {OwnerAPI} from './owner-api';
const wire=()=>({policy_sha:'62daedfa9c9bc2aea4ba7ebaa1cff2b3753cab69425efaa798fe1627099626b4',policy:{schema_version:1,renderer_version:'v2.2',workflow_type:'preview',child_failure_policy:'fail_fast',max_frames:5000}});
it('reads the server policy with independent canonical digest and detached fields',async()=>{
 const raw=wire(),request=vi.fn((..._args:unknown[])=>Promise.resolve(raw)),signal=new AbortController().signal;
 const api=new OwnerAPI({request} as never,'tenant/id');const result=await api.renderPolicy('project/id',signal);
 expect(request.mock.calls[0]).toEqual(['/api/v1/tenants/tenant%2Fid/projects/project%2Fid/render-policy',{signal}]);
 raw.policy.max_frames=1;expect(result.policy.max_frames).toBe(5000);
});
it('denies unsupported semantics and digest/shape substitution',()=>{
 expect(()=>readRenderPolicy({...wire(),policy_sha:'a'.repeat(64)})).toThrow('RENDER_POLICY_DIGEST_INVALID');
 expect(()=>readRenderPolicy({...wire(),policy:{...wire().policy,max_frames:1}})).toThrow('RENDER_POLICY_UNSUPPORTED');
 expect(()=>readRenderPolicy({...wire(),runtime:'cuda'})).toThrow('RENDER_POLICY_INVALID');
 expect(()=>readRenderPolicy({...wire(),policy:{...wire().policy,model_lock_sha:'a'.repeat(64)}})).toThrow('RENDER_POLICY_INVALID');
});

it('pins the server policy and frozen recipe before asynchronous discovery',async()=>{
 const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
 const frozen={document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(5),sha256:sha}};
 let resolve!:(value:ReturnType<typeof readRenderPolicy>)=>void;
 const api={renderPolicy:vi.fn(()=>new Promise<ReturnType<typeof readRenderPolicy>>(r=>{resolve=r}))};
 const pending=prepareEditorRenderFromServerPolicy(api,'project',frozen,'inventory','render',new AbortController().signal);
 frozen.recipe.sha256='b'.repeat(64);resolve(readRenderPolicy(wire()));
 const attempt=await pending;expect(attempt.policySHA).toBe(wire().policy_sha);expect(attempt.selection[0].recipe.sha256).toBe(sha);
});

it('rejects a policy response arriving after scope cancellation',async()=>{
 const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
 const frozen={document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(5),sha256:sha}};
 let resolve!:(value:ReturnType<typeof readRenderPolicy>)=>void;
 const controller=new AbortController();
 const pending=prepareEditorRenderFromServerPolicy({renderPolicy:()=>new Promise(r=>{resolve=r;})},'project',frozen,'inventory','render',controller.signal);
 const rejected=expect(pending).rejects.toThrow();
 controller.abort();resolve(readRenderPolicy(wire()));await rejected;
});
