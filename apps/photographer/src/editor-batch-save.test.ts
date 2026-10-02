import {it,expect,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {saveBatchCommand,type BatchSaveTarget} from './editor-batch-save';
import {createEditorState,applyEditorCommand,currentEditorSnapshot} from './editor-store';
import type {EditorDraft} from './catalog-types';
function target(id:string){const draft={asset_version_id:id,source_sha256:'a'.repeat(64),revision:'1',params:{strength:.5},mask_refs:[]} as EditorDraft;let current=createEditorState(draft);const accept=vi.fn((state)=>{current=state;});const value:BatchSaveTarget={assetId:id+'-asset',draft,submitted:current,current:()=>current,accept};return {value,edit:()=>{current=applyEditorCommand(current,{params:{strength:.7}});},get:()=>current,accept};}
it('preserves local edits while accepting each independent server revision',async()=>{
 const a=target('a'),b=target('b');const save=vi.fn(async(_asset,draft)=>{if(_asset==='a-asset')a.edit();return {...draft,revision:'2'};});
 const result=await saveBatchCommand([a.value,b.value],save,new AbortController().signal);expect(result.map(x=>x.status)).toEqual(['saved_local_changes','saved']);expect(currentEditorSnapshot(a.get()).params).toEqual({strength:.7});expect(a.get().draftRevision).toBe(2);expect(save.mock.calls[0][1].revision).toBe('1');
});
it('separates CAS conflict from successful frames',async()=>{
 const a=target('a'),b=target('b');const save=vi.fn().mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockImplementation(async(_asset,draft)=>({...draft,revision:'2'}));expect((await saveBatchCommand([a.value,b.value],save,new AbortController().signal)).map(x=>x.status)).toEqual(['conflict','saved']);expect(a.accept).not.toHaveBeenCalled();
});
it('acknowledges a commit after cancellation but starts no further writes',async()=>{
 const a=target('a'),b=target('b'),controller=new AbortController();const save=vi.fn(async(_asset,draft)=>{controller.abort();return {...draft,revision:'2'};});expect((await saveBatchCommand([a.value,b.value],save,controller.signal)).map(x=>x.status)).toEqual(['saved','cancelled']);expect(save).toHaveBeenCalledOnce();
});
it('does not save a selection changed after preparation',async()=>{const a=target('a');a.edit();const save=vi.fn();expect((await saveBatchCommand([a.value],save,new AbortController().signal))[0].status).toBe('conflict');expect(save).not.toHaveBeenCalled();});
