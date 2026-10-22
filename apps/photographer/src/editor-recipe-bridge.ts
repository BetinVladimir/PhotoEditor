import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {OwnerAPI} from './owner-api';
import type {EditorDraft} from './catalog-types';
import {prepareV2LayerSave} from './editor-v2-save';
import type {EditorState} from './editor-store';
import type {loadEditorV2Layers} from './editor-v2-load';
import {readLayerStack} from './editor-layers';
type Baseline=Awaited<ReturnType<typeof loadEditorV2Layers>>;
export function prepareEditorRecipeBridge(loaded:Baseline,state:EditorState,draft:EditorDraft,key:string){
 const attempt=prepareV2LayerSave(loaded,state,key);
 if(draft.asset_version_id!==state.versionId||draft.source_sha256!==state.sourceSha||!/^[1-9][0-9]*$/.test(draft.revision))throw new Error('EDITOR_BRIDGE_SOURCE_CHANGED');
 return {...attempt,editor_bridge:{revision:draft.revision,params:structuredClone(draft.params),document_id:loaded.draft.data.current_document_id as string},freeze_key:crypto.randomUUID()};
}
export type EditorRecipeBridgeAttempt=ReturnType<typeof prepareEditorRecipeBridge>;
/** Retries replay the exact saved Editor revision, even after newer local edits. */
export async function submitEditorRecipeBridge(api:Pick<OwnerAPI,'bridgeEditorRecipe'|'v2Document'>,asset:string,attempt:EditorRecipeBridgeAttempt,signal:AbortSignal):Promise<Baseline>{
 signal.throwIfAborted();const a=structuredClone(attempt);
 const saved=await api.bridgeEditorRecipe(asset,{...a.request,editor_bridge:a.editor_bridge},a.key,signal);signal.throwIfAborted();
 if(saved.id!==a.baseline.draft.id||saved.data.id!==saved.id||saved.data.asset_version_id!==a.baseline.binding.asset_version_id||saved.data.stream_type!=='draft'||saved.data.revision!==saved.revision||!/^[1-9][0-9]*$/.test(saved.revision)||BigInt(saved.revision)<=BigInt(a.request.expected_revision)||typeof saved.data.current_document_id!=='string')throw new Error('EDITOR_BRIDGE_ACK_CHANGED');
 const recipe=await api.v2Document(saved.data.current_document_id,signal);signal.throwIfAborted();
 const digest=(v:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v))));
 if(digest(recipe)!==saved.document.sha256||canonicalJSON({...recipe,overrides:a.request.recipe.overrides,layer_stack:a.request.recipe.layer_stack})!==canonicalJSON(a.request.recipe))throw new Error('EDITOR_BRIDGE_ACK_CHANGED');
 // The server replaces only its preceding bridge override. All unrelated manual
 // and semantic overrides must remain in their original order.
 const before=a.request.recipe.overrides,after=recipe.overrides;
 if(!Array.isArray(after)||after.length<1||after.length<before.length||after.length>before.length+1)throw new Error('EDITOR_BRIDGE_ACK_CHANGED');
 const retained=after.slice(0,-1);let removed=0,index=0;
 for(const ref of before){if(canonicalJSON(ref)===(retained[index]===undefined?'':canonicalJSON(retained[index])))index++;else removed++;}
 if(removed>1||index!==retained.length||after.some(ref=>!ref||!/^[0-9a-f]{64}$/.test(ref.sha256)))throw new Error('EDITOR_BRIDGE_ACK_CHANGED');
 if(a.request.layers===null?recipe.layer_stack!==null:recipe.layer_stack?.sha256!==digest(a.request.layers))throw new Error('EDITOR_BRIDGE_ACK_CHANGED');
 const stack=readLayerStack(a.request.layers??{schema_version:1,source:a.baseline.binding,layers:[]},a.baseline.binding);
 return {draft:saved,recipe,binding:a.baseline.binding,stack};
}
