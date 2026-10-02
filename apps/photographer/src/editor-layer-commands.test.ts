import {parseCachedEditorDraft} from './editor-draft-cache';
import {expect,it} from 'vitest';
import {createEditorState,currentEditorSnapshot,undoEditor,redoEditor,beginEditorPreview,acceptsEditorPreview,applyEditorCommand} from './editor-store';
import {setEditorLayerStack,patchLayerCommand} from './editor-layer-commands';
import type {EditorLayerStack} from './editor-layers';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const binding={asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha};
const stack:EditorLayerStack={schema_version:1,source:binding,layers:[{id:id(3),enabled:true,opacity:1,blend:'normal',operation:'color.render',operation_version:'1',params:{artifact_id:id(4),sha256:sha},mask:{op:'ref',mask:{artifact_id:id(5),sha256:sha},canvas_hash:sha}}]};
const initial=()=>createEditorState({asset_version_id:id(1),source_sha256:sha,revision:'1',params:{schema_version:1,modules:{strength:1}},mask_refs:[]});
it('uses the same undo/redo history and invalidates an old preview',()=>{
 const loaded=setEditorLayerStack(initial(),binding,stack),preview=beginEditorPreview(loaded),edited=patchLayerCommand(preview.state,binding,id(3),{opacity:.5});expect(acceptsEditorPreview(edited,preview.token)).toBe(false);expect(currentEditorSnapshot(edited).layerStack?.layers[0].opacity).toBe(.5);expect(currentEditorSnapshot(undoEditor(edited)).layerStack?.layers[0].opacity).toBe(1);expect(currentEditorSnapshot(redoEditor(undoEditor(edited))).layerStack?.layers[0].opacity).toBe(.5);
 const params=applyEditorCommand(edited,{params:{schema_version:1,modules:{strength:.7}}});expect(currentEditorSnapshot(params).layerStack?.layers[0].opacity).toBe(.5);expect(stack.layers[0].opacity).toBe(1);
});
it('rejects rebinding and records explicit layer removal as an undoable command',()=>{const loaded=setEditorLayerStack(initial(),binding,stack);expect(()=>patchLayerCommand(loaded,{...binding,canvas_hash:'b'.repeat(64)},id(3),{opacity:.5})).toThrow();expect(()=>setEditorLayerStack(loaded,{...binding,asset_version_id:id(9)},stack)).toThrow();const cleared=setEditorLayerStack(loaded,binding,null);expect(currentEditorSnapshot(cleared).layerStack).toBeNull();expect(currentEditorSnapshot(undoEditor(cleared)).layerStack).toEqual(stack);});

it('retains cached layers and rejects a changed cached source binding',()=>{
 const base={asset_version_id:id(1),source_sha256:sha,revision:'1',params:{schema_version:1,modules:{}},mask_refs:[]};
 const cached={schema_version:1,base,local:{params:base.params,maskRefs:[],layerStack:stack},saved_at:Date.now()};
 expect(parseCachedEditorDraft(JSON.stringify(cached),id(1))?.local.layerStack).toEqual(stack);
 const altered={...cached,local:{...cached.local,layerStack:{...stack,source:{...binding,source:{...binding.source,sha256:'b'.repeat(64)}}}}};expect(parseCachedEditorDraft(JSON.stringify(altered),id(1))).toBeNull();
});
