import {it,expect} from 'vitest';
import {applyBatchCommand} from './editor-batch';
import {createEditorState,currentEditorSnapshot} from './editor-store';
import type {EditorDraft} from './catalog-types';
function state(id:string,extra:number){return createEditorState({asset_version_id:id,source_sha256:'a'.repeat(64),revision:'1',params:{modules:{strength:1,extra}},mask_refs:[]} as EditorDraft);}
it('creates per-source history edits without losing other overrides',()=>{
 const a=state('a',1),b=state('b',2),states=new Map([['a',a],['b',b]]);const selection=[a,b].map(s=>({versionId:s.versionId,sourceSha:s.sourceSha,draftRevision:s.draftRevision}));
 const result=applyBatchCommand(selection,states,{path:['modules','strength'],value:.5});expect(result.affectedCount).toBe(2);expect(result.conflicts).toEqual([]);
 expect(currentEditorSnapshot(result.states.get('a')!).params).toEqual({modules:{strength:.5,extra:1}});expect(currentEditorSnapshot(result.states.get('b')!).params).toEqual({modules:{strength:.5,extra:2}});expect(a.requestRevision).toBe(0);expect(result.states.get('a')!.requestRevision).toBe(1);
});
it('reports each source or revision conflict and missing parameters separately',()=>{
 const a=state('a',1),b=state('b',2);const selection=[{versionId:'a',sourceSha:'b'.repeat(64),draftRevision:1},{versionId:'b',sourceSha:b.sourceSha,draftRevision:0},{versionId:'missing',sourceSha:a.sourceSha,draftRevision:1}];
 expect(applyBatchCommand(selection,new Map([['a',a],['b',b]]),{path:['modules','strength'],value:1}).conflicts.map(x=>x.reason)).toEqual(['source_changed','revision_changed','missing']);
 expect(applyBatchCommand([{versionId:'a',sourceSha:a.sourceSha,draftRevision:1}],new Map([['a',a]]),{path:['unknown'],value:1}).conflicts[0].reason).toBe('parameter_missing');
});
it('rejects duplicate frozen versions, unsafe values and prototype paths',()=>{
 const a=state('a',1),selection={versionId:'a',sourceSha:a.sourceSha,draftRevision:1},states=new Map([['a',a]]);
 expect(()=>applyBatchCommand([selection,selection],states,{path:['modules','strength'],value:1})).toThrow();expect(()=>applyBatchCommand([selection],states,{path:['__proto__','x'],value:1})).toThrow();expect(()=>applyBatchCommand([selection],states,{path:['modules','strength'],value:NaN})).toThrow();
});
