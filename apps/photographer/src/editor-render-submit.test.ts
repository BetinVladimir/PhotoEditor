import {it,expect,vi} from 'vitest';
import {prepareEditorRenderSubmission,submitEditorRender,readEditorRenderSubmission} from './editor-render-submit';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const frozen=()=>({document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(5),sha256:sha}});
it('retries both stages with the same inventory/recipe and keys after lost queue response',async()=>{
 const original=frozen(),attempt=prepareEditorRenderSubmission(original,sha,'inventory-key','render-key');original.recipe.sha256='b'.repeat(64);
 const api={createRenderInputSnapshot:vi.fn(async()=>({id:id(6),digest:sha,item_count:1})),queueRenderBatch:vi.fn().mockRejectedValueOnce(new Error('transport')).mockResolvedValueOnce({run_id:id(7),state:'queued',child_run_ids:[id(8)]})};
 const signal=new AbortController().signal;
 await expect(submitEditorRender(api,'project',attempt,signal)).rejects.toThrow('transport');
 await submitEditorRender(api,'project',attempt,signal);
 expect(api.createRenderInputSnapshot.mock.calls[0]).toEqual(api.createRenderInputSnapshot.mock.calls[1]);
 expect(api.queueRenderBatch.mock.calls[0]).toEqual(api.queueRenderBatch.mock.calls[1]);
 expect(api.queueRenderBatch.mock.calls[1][1]).toEqual({input_snapshot_id:id(6),policy_sha:sha,selection:[{source:frozen().source,recipe:frozen().recipe}]});
});
it('does not queue after cancellation between inventory and render',async()=>{
 const controller=new AbortController(),api={createRenderInputSnapshot:vi.fn(async()=>{controller.abort();return {id:id(6),digest:sha,item_count:1}}),queueRenderBatch:vi.fn()};
 await expect(submitEditorRender(api,'project',prepareEditorRenderSubmission(frozen(),sha,'inventory','render'),controller.signal)).rejects.toThrow();
 expect(api.queueRenderBatch).not.toHaveBeenCalled();
});

it('rejects an unfrozen source document before preparing either transport stage',()=>{
 const value=frozen();value.recipe={...value.source.source};
 expect(()=>prepareEditorRenderSubmission(value,sha,'inventory','render')).toThrow('RENDER_BATCH_INVALID');
});

it('discards a late queue ticket and retries the uncertain server outcome with the same keys',async()=>{
 const attempt=prepareEditorRenderSubmission(frozen(),sha,'inventory-key','render-key');
 const ticket={run_id:id(7),state:'queued' as const,child_run_ids:[id(8)]};
 const controller=new AbortController();
 let resolve!:(value:typeof ticket)=>void;
 const api={createRenderInputSnapshot:vi.fn(async()=>({id:id(6),digest:sha,item_count:1})),queueRenderBatch:vi.fn().mockImplementationOnce(()=>new Promise(r=>{resolve=r;})).mockResolvedValue(ticket)};
 const pending=submitEditorRender(api,'project',attempt,controller.signal);
 const rejected=expect(pending).rejects.toThrow();
 await Promise.resolve();expect(api.queueRenderBatch).toHaveBeenCalledTimes(1);
 controller.abort();resolve(ticket);await rejected;
 const retry=await submitEditorRender(api,'project',attempt,new AbortController().signal);
 expect(retry).toEqual(ticket);
 expect(api.createRenderInputSnapshot.mock.calls[1].slice(0,3)).toEqual(api.createRenderInputSnapshot.mock.calls[0].slice(0,3));
 expect(api.queueRenderBatch.mock.calls[1].slice(0,3)).toEqual(api.queueRenderBatch.mock.calls[0].slice(0,3));
});

it('refuses mismatched persisted inventory/source binding before either server mutation',async()=>{
 const attempt=prepareEditorRenderSubmission(frozen(),sha,'inventory','render');
 const restored=readEditorRenderSubmission(JSON.parse(JSON.stringify(attempt)),id(3));
 expect(restored).toEqual(attempt);
 expect(()=>readEditorRenderSubmission(attempt,id(9))).toThrow('RENDER_COMMAND_SOURCE_CHANGED');
 const changed=structuredClone(attempt);changed.input.sources[0].canvas_hash='b'.repeat(64);
 const api={createRenderInputSnapshot:vi.fn(),queueRenderBatch:vi.fn()};
 await expect(submitEditorRender(api,'project',changed,new AbortController().signal)).rejects.toThrow('RENDER_COMMAND_INPUT_CHANGED');
 expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(api.queueRenderBatch).not.toHaveBeenCalled();
 expect(()=>readEditorRenderSubmission({...attempt,url:'https://private'})).toThrow('RENDER_COMMAND_INVALID');
 attempt.selection[0].recipe.sha256='b'.repeat(64);expect(restored.selection[0].recipe.sha256).toBe(sha);
});
