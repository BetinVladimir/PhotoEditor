import {expect,it,vi} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {loadEditorV2Layers} from './editor-v2-load';
import {createEditorState} from './editor-store';
import type {V2EditingRecipe} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const binding={asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha};
const recipe:V2EditingRecipe={schema_version:3,source:binding,base_style:{artifact_id:id(3),sha256:sha},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
const state=()=>createEditorState({asset_version_id:id(1),source_sha256:sha,revision:'1',params:{schema_version:1,modules:{}},mask_refs:[]});
function api(){return {v2Draft:vi.fn(async()=>({id:id(4),revision:'2',document:{artifact_id:id(5),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(recipe))))},data:{resource_type:'v2_document_streams',stream_type:'draft',id:id(4),revision:'2',current_document_id:id(6),asset_version_id:id(1)}})),v2Binding:vi.fn(async()=>binding),v2Document:vi.fn(async()=>recipe),v2Layers:vi.fn()};}

import {prepareV2LayerSave,saveV2Layers,V2LayerSavedRefreshRequired} from './editor-v2-save';
it('freezes the request and verifies the committed recipe without changing legacy revision',async()=>{
 const owner=api(),editor=state(),baseline=await loadEditorV2Layers(owner,'asset',editor,new AbortController().signal);
 const attempt=prepareV2LayerSave(baseline,editor,'stable-key');
 const committed={...recipe,layer_stack:{artifact_id:id(7),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(attempt.request.layers))))}};
 const saved={...baseline.draft,revision:'9007199254740993',document:{artifact_id:id(8),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(committed))))},data:{...baseline.draft.data,revision:'9007199254740993',current_document_id:id(9)}};
 const mutation=vi.fn(async()=>saved),read=vi.fn(async()=>committed);
 baseline.recipe.parameter_delta.ev=2;
 const verified=await saveV2Layers({saveV2Draft:mutation,v2Document:read},'asset',attempt,new AbortController().signal);
 expect(verified.recipe.parameter_delta.ev).toBe(0);expect(editor.draftRevision).toBe(1);
 expect(mutation).toHaveBeenCalledWith('asset','2',attempt.request.recipe,attempt.request.layers,'stable-key',expect.any(AbortSignal));
});
it('marks an acknowledged but unreadable commit as refresh required',async()=>{
 const baseline=await loadEditorV2Layers(api(),'asset',state(),new AbortController().signal),attempt=prepareV2LayerSave(baseline,state(),'key');
 const saved={...baseline.draft,revision:'3',data:{...baseline.draft.data,revision:'3'}};
 await expect(saveV2Layers({saveV2Draft:vi.fn(async()=>saved),v2Document:vi.fn(async()=>{throw new Error('offline')})},'asset',attempt,new AbortController().signal)).rejects.toBeInstanceOf(V2LayerSavedRefreshRequired);
});
it('rejects a source change before writing',async()=>{
 const baseline=await loadEditorV2Layers(api(),'asset',state(),new AbortController().signal);
 expect(()=>prepareV2LayerSave(baseline,{...state(),sourceSha:'b'.repeat(64)},'key')).toThrow('EDITOR_V2_SOURCE_CHANGED');
});

import {prepareEditorRenderFreeze,freezeEditorRender} from './editor-render-freeze';
it('prepares the saved V2 revision and verifies server source before accepting freeze',async()=>{
 const editor=state(),baseline=await loadEditorV2Layers(api(),'asset',editor,new AbortController().signal);
 const attempt=prepareEditorRenderFreeze(baseline,editor,'freeze-key');
 expect(attempt.request).toEqual({document_id:id(6),expected_revision:'2',assume_profile:null});
 const response={document_id:id(8),draft_document_id:id(6),draft_revision:'2',source:structuredClone(binding),recipe:{artifact_id:id(9),sha256:sha}};
 const transport={freezeRenderDraft:vi.fn(async()=>response)};
 expect(await freezeEditorRender(transport,'asset',attempt,new AbortController().signal)).toEqual(response);
 response.source.canvas_hash='b'.repeat(64);
 await expect(freezeEditorRender(transport,'asset',attempt,new AbortController().signal)).rejects.toThrow('EDITOR_V2_SOURCE_CHANGED');
});
