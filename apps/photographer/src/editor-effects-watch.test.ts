import {it,expect,vi} from 'vitest';
import {observeEditorEffects} from './editor-effects-watch';
import {prepareEditorEffectsAttempt} from './editor-effects-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const accepted=()=>({...prepareEditorEffectsAttempt(id(6),{document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}},id(7)),accepted:{run_id:id(10),status:'queued' as const}});
const status=()=>({build_run_id:id(10),state:'sealing',build_status:'completed',seal_run_id:id(11),final_run_id:null,review_required:false});
it('never reads intermediate Apply or seal outputs as final results',async()=>{
 const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockResolvedValue(status()),run:vi.fn(),runOutputs:vi.fn()};
 expect((await observeEditorEffects(api,accepted(),new AbortController().signal,()=>true)).frames).toEqual([]);
 expect(api.queueEditorEffects).not.toHaveBeenCalled();expect(api.run).not.toHaveBeenCalled();expect(api.runOutputs).not.toHaveBeenCalled();
});
it('accepted access loss cannot enqueue again or read result artifacts',async()=>{
 for(const code of [401,403]){const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockRejectedValue({status:code}),run:vi.fn(),runOutputs:vi.fn()};
 await expect(observeEditorEffects(api,accepted(),new AbortController().signal,()=>true)).rejects.toEqual({status:code});
 expect(api.queueEditorEffects).not.toHaveBeenCalled();expect(api.runOutputs).not.toHaveBeenCalled();}
});
it('late status after scope revocation cannot continue into a final run',async()=>{
 let active=true;const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockImplementation(async()=>{active=false;return {...status(),state:'composing',final_run_id:id(12)}}),run:vi.fn(),runOutputs:vi.fn()};
 await expect(observeEditorEffects(api,accepted(),new AbortController().signal,()=>active)).rejects.toThrow('SCOPE_EXPIRED');expect(api.run).not.toHaveBeenCalled();
});
it('source mismatch recovery remains read-only until failed withdrawal and never reads retired artifact outputs',async()=>{
 const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockRejectedValueOnce({status:409}).mockResolvedValueOnce({...status(),state:'failed',final_run_id:id(12)}),run:vi.fn(),runOutputs:vi.fn()};
 await expect(observeEditorEffects(api,accepted(),new AbortController().signal,()=>true)).rejects.toEqual({status:409});
 const recovered=await observeEditorEffects(api,accepted(),new AbortController().signal,()=>true);
 expect(recovered.status.state).toBe('failed');expect(recovered.frames).toEqual([]);expect(api.run).not.toHaveBeenCalled();expect(api.runOutputs).not.toHaveBeenCalled();expect(api.queueEditorEffects).not.toHaveBeenCalled();
});
