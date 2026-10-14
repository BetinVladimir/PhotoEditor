import {canonicalJSON} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {loadEditorV2Layers} from './editor-v2-load';
import {currentEditorSnapshot,type EditorState} from './editor-store';
import {renderFreezeRequest} from './render-freeze';
type Baseline=Awaited<ReturnType<typeof loadEditorV2Layers>>;
export function prepareEditorRenderFreeze(loaded:Baseline,state:EditorState,key:string){
 if(state.versionId!==loaded.binding.asset_version_id||state.sourceSha!==loaded.binding.source.sha256)throw new Error('EDITOR_V2_SOURCE_CHANGED');
 const local=currentEditorSnapshot(state).layerStack;
 if(local!==undefined&&canonicalJSON(local===null?{schema_version:1,source:loaded.binding,layers:[]}:local)!==canonicalJSON(loaded.stack))throw new Error('EDITOR_V2_SAVE_REQUIRED');
 const request=renderFreezeRequest({document_id:loaded.draft.data.current_document_id as string,expected_revision:loaded.draft.revision,assume_profile:null});
 return {key,request,binding:structuredClone(loaded.binding)};
}
export async function freezeEditorRender(api:Pick<OwnerAPI,'freezeRenderDraft'>,asset:string,attempt:ReturnType<typeof prepareEditorRenderFreeze>,signal:AbortSignal){
 const owned=structuredClone(attempt);signal.throwIfAborted();
 const result=await api.freezeRenderDraft(asset,owned.request,owned.key,signal);signal.throwIfAborted();
 if(canonicalJSON(result.source)!==canonicalJSON(owned.binding))throw new Error('EDITOR_V2_SOURCE_CHANGED');
 return result;
}
