import {expect,it,vi} from 'vitest';
import {freezeSceneApplyDecision,sceneApplyDecision} from './scene-apply';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const decision=()=>({plan_sha:sha,approved_groups:[id(1)],recipe:{artifact_id:id(2),sha256:sha},expected_revision:'1'});
it('owns the exact decision before caller data changes',()=>{
 const input=decision(),frozen=freezeSceneApplyDecision(input);input.approved_groups.push(id(3));input.recipe.sha256='b'.repeat(64);expect(frozen).toEqual(decision());
});
it('refuses duplicate groups, overflow revisions and missing verified plan SHA',()=>{
 for(const input of [{...decision(),approved_groups:[id(1),id(1)]},{...decision(),approved_groups:[]},{...decision(),expected_revision:'9223372036854775808'},{...decision(),expected_revision:'01'},{...decision(),plan_sha:''}])expect(()=>freezeSceneApplyDecision(input)).toThrow('SCENE_APPLY_INVALID');
 const board={snapshotId:id(5),snapshotDigest:sha,policySha:sha,groups:[]};expect(()=>sceneApplyDecision(board,[id(1)],decision().recipe,'1')).toThrow();
});
it('submits to the analysis scope with the caller retry key and abort signal',async()=>{
 const request=vi.fn().mockResolvedValue({run_id:id(6),workflow_id:id(7),state:'queued'}),api=new OwnerAPI({request} as unknown as ApiClient,id(8));
 const input=decision(),signal=new AbortController().signal,pending=api.applySceneAnalysis(id(9),input,'retry-key',signal);input.approved_groups.push(id(3));input.recipe.sha256='b'.repeat(64);
 await expect(pending).resolves.toEqual({run_id:id(6),workflow_id:id(7),state:'queued'});
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id(8)}/session-analyses/${id(9)}/apply`,{method:'POST',body:decision(),idempotencyKey:'retry-key',signal});
});
it('rejects invalid decisions and keys before network I/O',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(8));
 await expect(api.applySceneAnalysis(id(9),decision(),'')).rejects.toThrow();
 await expect(api.applySceneAnalysis(id(9),{...decision(),approved_groups:[]},'key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
