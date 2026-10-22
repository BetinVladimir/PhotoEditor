import {expect,it,vi} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {createEditorState} from './editor-store';
import {prepareEditorRecipeBridge,submitEditorRecipeBridge} from './editor-recipe-bridge';
import {EditorRecipeBridgeStore,clearEditorRecipeBridges} from './editor-recipe-bridge-store';
import type {V2EditingRecipe} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const binding={asset_version_id:id(1),source:{artifact_id:id(2),sha256:sha},canvas_hash:sha};
const recipe:V2EditingRecipe={schema_version:3,source:binding,base_style:{artifact_id:id(3),sha256:sha},session_corrections:null,learning_version_id:null,overrides:[{artifact_id:id(20),sha256:sha}],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
const draft={asset_version_id:id(1),source_sha256:sha,revision:'7',params:{schema_version:1,modules:{strength:0.5}},mask_refs:[]};
function attempt(){const state=createEditorState(draft);return prepareEditorRecipeBridge({binding,recipe,stack:{schema_version:1,source:binding,layers:[]},draft:{id:id(4),revision:'2',document:{artifact_id:id(5),sha256:sha},data:{id:id(4),revision:'2',current_document_id:id(6),project_id:id(9),asset_version_id:id(1),stream_type:'draft'}}},state,draft,id(7));}
it('retains immutable editor revision and exact request across remount and rejects replacement',async()=>{
 const data=new Map<string,string>(),storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};
 const make=()=>new EditorRecipeBridgeStore(storage,'https://photos.example',id(8),id(9),id(1));const store=make(),a=attempt();await store.write(a);
 expect(await make().read()).toEqual(a);await expect(store.write({...a,key:id(10)})).rejects.toThrow('CONFLICT');
 await clearEditorRecipeBridges(storage,'https://photos.example');await expect(store.write(a)).rejects.toThrow('EXPIRED');expect(await make().read()).toBeNull();
});
it('verifies full-recipe acknowledgement and preserves semantic manual override',async()=>{
 const a=attempt(),committed={...recipe,layer_stack:{artifact_id:id(21),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(attempt().request.layers))))},overrides:[...recipe.overrides,{artifact_id:id(11),sha256:sha}]};
 const saved={...a.baseline.draft,revision:'3',document:{artifact_id:id(12),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(committed))))},data:{...a.baseline.draft.data,revision:'3',current_document_id:id(13)}};
 const mutation=vi.fn(async()=>saved),read=vi.fn(async()=>committed);
 const loaded=await submitEditorRecipeBridge({bridgeEditorRecipe:mutation,v2Document:read},'asset',a,new AbortController().signal);
 expect(loaded.recipe.overrides).toEqual(committed.overrides);expect(mutation).toHaveBeenCalledWith('asset',{...a.request,editor_bridge:a.editor_bridge},a.key,expect.any(AbortSignal));
});
it('does not emit a replacement mutation on cancellation',async()=>{const mutation=vi.fn(),controller=new AbortController();controller.abort();await expect(submitEditorRecipeBridge({bridgeEditorRecipe:mutation,v2Document:vi.fn()},'asset',attempt(),controller.signal)).rejects.toThrow();expect(mutation).not.toHaveBeenCalled();});
it('rejects a hash-correct recipe that changes semantic manual overrides or color delta',async()=>{
 const a=attempt();
 for(const committed of [
  {...recipe,parameter_delta:{...recipe.parameter_delta,ev:1},overrides:[...recipe.overrides,{artifact_id:id(11),sha256:sha}]},
  {...recipe,overrides:[{artifact_id:id(19),sha256:sha},{artifact_id:id(11),sha256:sha}]},
 ]){
  const saved={...a.baseline.draft,revision:'3',document:{artifact_id:id(12),sha256:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(committed))))},data:{...a.baseline.draft.data,revision:'3',current_document_id:id(13)}};
  await expect(submitEditorRecipeBridge({bridgeEditorRecipe:vi.fn(async()=>saved),v2Document:vi.fn(async()=>committed)},'asset',a,new AbortController().signal)).rejects.toThrow('ACK_CHANGED');
 }
});
it('retains the immutable attempt after an uncertain mutation and fences late responses',async()=>{
 const a=attempt(),controller=new AbortController();let release!:(v:typeof a.baseline.draft)=>void;
 const mutation=vi.fn(()=>new Promise<typeof a.baseline.draft>(resolve=>{release=resolve})),read=vi.fn();
 const request=submitEditorRecipeBridge({bridgeEditorRecipe:mutation,v2Document:read},'asset',a,controller.signal);
 controller.abort();release(a.baseline.draft);await expect(request).rejects.toThrow();expect(read).not.toHaveBeenCalled();
 expect(a.editor_bridge.revision).toBe('7');expect(a.key).toBe(id(7));
});
