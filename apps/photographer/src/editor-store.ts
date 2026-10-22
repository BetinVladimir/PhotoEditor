import {legacyJSONRevision} from './legacy-json-revision';
import type {EditorLayerStack} from './editor-layers';
import type{EditorDraft}from"./catalog-types";

export const EDITOR_HISTORY_LIMIT=200;

export type EditorViewport={scale:number;offsetX:number;offsetY:number};
export type EditorSnapshot={params:Record<string,unknown>;maskRefs:unknown[];selectionObjectId?:string;layerStack?:EditorLayerStack|null};
export type EditorState={versionId:string;sourceSha:string;draftRevision:number;requestRevision:number;history:EditorSnapshot[];historyIndex:number;viewport:EditorViewport;pendingPreview?:{requestRevision:number;sourceSha:string;draftRevision:number}};

function snapshot(draft:Pick<EditorDraft,"params"|"mask_refs">,selectionObjectId?:string):EditorSnapshot{return{params:structuredClone(draft.params),maskRefs:structuredClone(draft.mask_refs),selectionObjectId}}

export function createEditorState(draft:EditorDraft):EditorState{return{versionId:draft.asset_version_id,sourceSha:draft.source_sha256,draftRevision:legacyJSONRevision(draft.revision),requestRevision:0,history:[snapshot(draft)],historyIndex:0,viewport:{scale:1,offsetX:0,offsetY:0}}}

export function applyEditorCommand(state:EditorState,change:Partial<EditorSnapshot>):EditorState{
 const current=state.history[state.historyIndex],next:EditorSnapshot={layerStack:structuredClone(change.layerStack===undefined?current.layerStack:change.layerStack),params:structuredClone(change.params??current.params),maskRefs:structuredClone(change.maskRefs??current.maskRefs),selectionObjectId:change.selectionObjectId===undefined?current.selectionObjectId:change.selectionObjectId};
 // Each entry is a complete snapshot. The oldest retained entry therefore
 // becomes the checkpoint when earlier commands are compacted away.
 const history=[...state.history.slice(0,state.historyIndex+1),next].slice(-EDITOR_HISTORY_LIMIT);
 return{...state,requestRevision:state.requestRevision+1,history,historyIndex:history.length-1,pendingPreview:undefined};
}

export function undoEditor(state:EditorState):EditorState{return state.historyIndex===0?state:{...state,historyIndex:state.historyIndex-1,requestRevision:state.requestRevision+1,pendingPreview:undefined}}
export function redoEditor(state:EditorState):EditorState{return state.historyIndex>=state.history.length-1?state:{...state,historyIndex:state.historyIndex+1,requestRevision:state.requestRevision+1,pendingPreview:undefined}}
export function currentEditorSnapshot(state:EditorState):EditorSnapshot{return state.history[state.historyIndex]}

export function beginEditorPreview(state:EditorState){const requestRevision=state.requestRevision+1,pendingPreview={requestRevision,sourceSha:state.sourceSha,draftRevision:state.draftRevision};return{state:{...state,requestRevision,pendingPreview},token:pendingPreview}}
export function acceptsEditorPreview(state:EditorState,token:{requestRevision:number;sourceSha:string;draftRevision:number}){const pending=state.pendingPreview;return!!pending&&pending.requestRevision===token.requestRevision&&pending.sourceSha===token.sourceSha&&pending.draftRevision===token.draftRevision&&state.sourceSha===token.sourceSha&&state.draftRevision===token.draftRevision}
export function screenToSource(point:{x:number;y:number},size:{width:number;height:number},viewport:EditorViewport){
 if(size.width<=0||size.height<=0||viewport.scale<1||!Object.values({...point,...size,...viewport}).every(Number.isFinite))throw new Error("VIEWPORT_INVALID");
 const x=(point.x-viewport.offsetX-size.width/2)/viewport.scale+size.width/2,y=(point.y-viewport.offsetY-size.height/2)/viewport.scale+size.height/2;
 return{x:Math.min(1,Math.max(0,x/size.width)),y:Math.min(1,Math.max(0,y/size.height))};
}
export function setEditorViewport(state:EditorState,viewport:EditorViewport):EditorState{if(viewport.scale<1||viewport.scale>16||!Object.values(viewport).every(Number.isFinite))throw new Error("VIEWPORT_INVALID");return{...state,viewport}}

export function prepareEditorDraftSave(state:EditorState,draft:EditorDraft,params:Record<string,unknown>):EditorDraft{
 if(state.versionId!==draft.asset_version_id||state.sourceSha!==draft.source_sha256)throw new Error("EDITOR_SOURCE_CHANGED");
 if(String(state.draftRevision)!==draft.revision)throw new Error("DRAFT_REVISION_CHANGED");
 return{...draft,params:structuredClone(params),mask_refs:structuredClone(currentEditorSnapshot(state).maskRefs)};
}

export function acceptEditorDraftSave(current:EditorState,submitted:EditorState,saved:EditorDraft){
 if(current.versionId!==submitted.versionId||current.sourceSha!==submitted.sourceSha||saved.asset_version_id!==submitted.versionId||saved.source_sha256!==submitted.sourceSha)throw new Error("EDITOR_SOURCE_CHANGED");
 const revision=Number(saved.revision);
 if(current.draftRevision!==submitted.draftRevision||!Number.isSafeInteger(revision)||revision<=submitted.draftRevision)throw new Error("DRAFT_REVISION_CHANGED");
 const replaceRecipe=current.requestRevision===submitted.requestRevision;
 const next=replaceRecipe?applyEditorCommand(current,{params:saved.params,maskRefs:saved.mask_refs}):{...current,pendingPreview:undefined};
 return{state:{...next,draftRevision:revision},replaceRecipe};
}

export function resolveEditorDraftConflict(state:EditorState,server:EditorDraft,choice:"local"|"server"):EditorState{
 if(state.versionId!==server.asset_version_id||state.sourceSha!==server.source_sha256)throw new Error("EDITOR_SOURCE_CHANGED");
 const revision=Number(server.revision);
 if(!Number.isSafeInteger(revision)||revision<state.draftRevision)throw new Error("DRAFT_REVISION_CHANGED");
 if(choice!=="local"&&choice!=="server")throw new Error("EDITOR_CONFLICT_CHOICE_INVALID");
 const next=choice==="server"?applyEditorCommand(state,{params:server.params,maskRefs:server.mask_refs}):{...state,requestRevision:state.requestRevision+1,pendingPreview:undefined};
 return{...next,draftRevision:revision};
}
