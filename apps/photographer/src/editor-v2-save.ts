import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {OwnerAPI} from './owner-api';
import type {EditorState} from './editor-store';
import {currentEditorSnapshot} from './editor-store';
import {readLayerStack} from './editor-layers';
import type {loadEditorV2Layers} from './editor-v2-load';
import type {V2DraftResource} from './catalog-types';
type Baseline=Awaited<ReturnType<typeof loadEditorV2Layers>>;
const digest=(value:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(value))));
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
/** Capture once; retries must reuse this request and key, even if local history advances. */
export function prepareV2LayerSave(loaded:Baseline,state:EditorState,key:string){
 if(state.versionId!==loaded.binding.asset_version_id||state.sourceSha!==loaded.binding.source.sha256||!key)throw new Error('EDITOR_V2_SOURCE_CHANGED');
 const local=currentEditorSnapshot(state).layerStack;
 const layers=local===null?null:readLayerStack(local===undefined?loaded.stack:local,loaded.binding);
 const recipe=structuredClone(loaded.recipe);if(layers===null)recipe.layer_stack=null;
 const request={expected_revision:loaded.draft.revision,recipe,layers};
 if(new TextEncoder().encode(canonicalJSON(request)).byteLength>1048576)throw new Error('EDITOR_V2_REQUEST_TOO_LARGE');
 return {baseline:structuredClone(loaded),request,key};
}
export class V2LayerSavedRefreshRequired extends Error{
 constructor(readonly saved:V2DraftResource,options?:ErrorOptions){super('EDITOR_V2_SAVED_REFRESH_REQUIRED',options);}
}
/** A successful mutation followed by a failed verification is not a new write attempt. */
export async function saveV2Layers(api:Pick<OwnerAPI,'saveV2Draft'|'v2Document'>,asset:string,attempt:ReturnType<typeof prepareV2LayerSave>,signal:AbortSignal):Promise<Baseline>{
 signal.throwIfAborted();const {request,key,baseline}=attempt;
 const saved=await api.saveV2Draft(asset,request.expected_revision,structuredClone(request.recipe),structuredClone(request.layers),key,signal);
 try{
  signal.throwIfAborted();const d=saved.data;
  if(saved.id!==baseline.draft.id||d.id!==saved.id||d.resource_type!=='v2_document_streams'||d.stream_type!=='draft'||d.asset_version_id!==baseline.binding.asset_version_id||d.revision!==saved.revision||!/^[1-9][0-9]*$/.test(saved.revision)||BigInt(saved.revision)<=BigInt(request.expected_revision)||!uuid(d.current_document_id)||!uuid(saved.document.artifact_id)||!/^[0-9a-f]{64}$/.test(saved.document.sha256))throw new Error('EDITOR_V2_ACK_CHANGED');
  const recipe=await api.v2Document(d.current_document_id,signal);signal.throwIfAborted();
  if(new TextEncoder().encode(canonicalJSON(recipe)).byteLength>1048576||digest(recipe)!==saved.document.sha256||canonicalJSON({...recipe,layer_stack:request.recipe.layer_stack})!==canonicalJSON(request.recipe))throw new Error('EDITOR_V2_ACK_CHANGED');
  if(request.layers===null?recipe.layer_stack!==null:!recipe.layer_stack||!uuid(recipe.layer_stack.artifact_id)||recipe.layer_stack.sha256!==digest(request.layers))throw new Error('EDITOR_V2_ACK_CHANGED');
  const stack=request.layers===null?readLayerStack({schema_version:1,source:baseline.binding,layers:[]},baseline.binding):readLayerStack(request.layers,baseline.binding);
  return {draft:structuredClone(saved),recipe:structuredClone(recipe),binding:structuredClone(baseline.binding),stack};
 }catch(error){throw new V2LayerSavedRefreshRequired(saved,{cause:error});}
}
