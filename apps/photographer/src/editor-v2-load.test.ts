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
function api(){return {v2Draft:vi.fn(async()=>({id:id(4),revision:'2',document:{artifact_id:id(5),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(recipe))))},data:{resource_type:'v2_document_streams',stream_type:'draft',id:id(4),revision:'2',current_document_id:id(6),project_id:id(9),asset_version_id:id(1)}})),v2Binding:vi.fn(async()=>binding),v2Document:vi.fn(async()=>recipe),v2Layers:vi.fn()};}
it('loads the exact pinned recipe and represents a missing stack as empty without master IO',async()=>{const owner=api(),out=await loadEditorV2Layers(owner,'asset',state(),new AbortController().signal);expect(out.stack.layers).toEqual([]);expect(owner.v2Document).toHaveBeenCalledWith(id(6),expect.any(AbortSignal));expect(owner.v2Layers).not.toHaveBeenCalled();});
it('rejects a changed recipe digest before loading layer dependencies',async()=>{const owner=api();owner.v2Document.mockResolvedValue({...recipe,parameter_delta:{...recipe.parameter_delta,ev:.5}});await expect(loadEditorV2Layers(owner,'asset',state(),new AbortController().signal)).rejects.toThrow('EDITOR_V2_DOCUMENT_CHANGED');expect(owner.v2Layers).not.toHaveBeenCalled();});
it('does not load the recipe after cancellation during initial reads',async()=>{const owner=api(),controller=new AbortController();owner.v2Binding.mockImplementation(async()=>{controller.abort();return binding;});await expect(loadEditorV2Layers(owner,'asset',state(),controller.signal)).rejects.toThrow();expect(owner.v2Document).not.toHaveBeenCalled();});
