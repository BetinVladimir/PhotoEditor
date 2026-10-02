import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {OwnerAPI} from './owner-api';
import type {EditorState} from './editor-store';
import {readLayerStack} from './editor-layers';
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
/** Load the pinned current V2 draft without installing any state or fetching master pixels. */
export async function loadEditorV2Layers(api:Pick<OwnerAPI,'v2Draft'|'v2Binding'|'v2Document'|'v2Layers'>,asset:string,state:EditorState,signal:AbortSignal){
 const version=state.versionId,sourceSHA=state.sourceSha;signal.throwIfAborted();
 const [draft,binding]=await Promise.all([api.v2Draft(asset,signal),api.v2Binding(asset,signal)]);signal.throwIfAborted();
 const documentId=draft.data.current_document_id;
 if(draft.data.resource_type!=='v2_document_streams'||draft.data.stream_type!=='draft'||!uuid(draft.id)||draft.data.id!==draft.id||draft.data.revision!==draft.revision||!/^([1-9][0-9]*)$/.test(draft.revision)||!uuid(documentId)||!uuid(draft.document.artifact_id)||!/^[0-9a-f]{64}$/.test(draft.document.sha256)||draft.data.asset_version_id!==version||binding.asset_version_id!==version||binding.source.sha256!==sourceSHA)throw new Error('EDITOR_V2_SOURCE_CHANGED');
 const recipe=await api.v2Document(documentId,signal);signal.throwIfAborted();
 const body=new TextEncoder().encode(canonicalJSON(recipe));
 if(body.byteLength>1048576||bytesToHex(sha256(body))!==draft.document.sha256||recipe.schema_version!==3||recipe.renderer_version!=='v2.2'||canonicalJSON(recipe.source)!==canonicalJSON(binding))throw new Error('EDITOR_V2_DOCUMENT_CHANGED');
 const stack=recipe.layer_stack===null?readLayerStack({schema_version:1,source:binding,layers:[]},binding):await api.v2Layers(asset,recipe.layer_stack,binding,signal);signal.throwIfAborted();
 return {draft:structuredClone(draft),recipe:structuredClone(recipe),binding:structuredClone(binding),stack:readLayerStack(stack,binding)};
}
