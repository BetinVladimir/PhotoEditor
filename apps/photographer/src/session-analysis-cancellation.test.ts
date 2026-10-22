import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const decision={expected_revision:'1',plan_sha:'a'.repeat(64),approved_groups:[id(2)],recipe:{artifact_id:id(3),sha256:'b'.repeat(64)}};
for(const phase of ['analyze','apply','list','board'] as const)it(`fences pre/late cancellation of actual ${phase} adapter`,async()=>{
 let resolve!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise<unknown>(done=>{resolve=done})),api=new OwnerAPI({request} as never,id(1));
 const invoke=(signal:AbortSignal)=>phase==='analyze'?api.analyzeSession(id(2),{} as never,'same-key',signal):phase==='apply'?api.applySceneAnalysis(id(2),decision,'same-key',signal):phase==='list'?api.sessionAnalyses(id(2),undefined,signal):api.sceneBoard(id(2),id(3),signal);
 const controller=new AbortController(),pending=invoke(controller.signal);controller.abort();resolve({run_id:id(4),workflow_id:id(5),state:'queued'});await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(invoke(controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
