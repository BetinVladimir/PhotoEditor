import type {V2VersionBinding} from './catalog-types';
import {readLayerStack,patchEditorLayer,moveEditorLayer,type EditorLayer,type EditorLayerStack} from './editor-layers';
import {applyEditorCommand,currentEditorSnapshot,type EditorState} from './editor-store';
function bound(state:EditorState,binding:V2VersionBinding){if(state.versionId!==binding.asset_version_id||state.sourceSha!==binding.source.sha256)throw new Error('EDITOR_SOURCE_CHANGED');}
export function setEditorLayerStack(state:EditorState,binding:V2VersionBinding,stack:EditorLayerStack|null):EditorState{
 bound(state,binding);return applyEditorCommand(state,{layerStack:stack===null?null:readLayerStack(stack,binding)});
}
export function patchLayerCommand(state:EditorState,binding:V2VersionBinding,id:string,patch:Partial<Pick<EditorLayer,'enabled'|'opacity'|'blend'>>):EditorState{
 bound(state,binding);const stack=currentEditorSnapshot(state).layerStack;if(!stack)throw new Error('EDITOR_LAYERS_MISSING');return setEditorLayerStack(state,binding,patchEditorLayer(readLayerStack(stack,binding),id,patch));
}
export function moveLayerCommand(state:EditorState,binding:V2VersionBinding,id:string,index:number):EditorState{
 bound(state,binding);const stack=currentEditorSnapshot(state).layerStack;if(!stack)throw new Error('EDITOR_LAYERS_MISSING');return setEditorLayerStack(state,binding,moveEditorLayer(readLayerStack(stack,binding),id,index));
}
