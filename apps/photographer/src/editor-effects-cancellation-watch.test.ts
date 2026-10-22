import {expect,it,vi} from 'vitest';
import {observeEditorEffects} from './editor-effects-watch';
import {prepareEditorEffectsAttempt} from './editor-effects-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function attempt(){return {...prepareEditorEffectsAttempt(id(6),{document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}},id(7)),accepted:{run_id:id(10),status:'queued' as const}}}
it.each(['cancelling','cancelled','completed_with_errors'])('recovers %s without resubmitting or reading intermediate outputs',async(build_status)=>{
 const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockResolvedValue({build_run_id:id(10),state:build_status==='cancelling'?'building':'failed',build_status,seal_run_id:null,final_run_id:null,review_required:false}),run:vi.fn(),runOutputs:vi.fn()};
 const result=await observeEditorEffects(api,attempt(),new AbortController().signal,()=>true);
 expect(result.status.build_status).toBe(build_status);expect(result.frames).toEqual([]);
 expect(api.queueEditorEffects).not.toHaveBeenCalled();expect(api.run).not.toHaveBeenCalled();expect(api.runOutputs).not.toHaveBeenCalled();
});
it('does not continue a late status response after cancellation',async()=>{
 const controller=new AbortController();
 const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockImplementation(async()=>{controller.abort();return {build_run_id:id(10),state:'composing',build_status:'completed',seal_run_id:id(11),final_run_id:id(12),review_required:false}}),run:vi.fn(),runOutputs:vi.fn()};
 await expect(observeEditorEffects(api,attempt(),controller.signal,()=>true)).rejects.toThrow();
 expect(api.queueEditorEffects).not.toHaveBeenCalled();expect(api.run).not.toHaveBeenCalled();expect(api.runOutputs).not.toHaveBeenCalled();
});
