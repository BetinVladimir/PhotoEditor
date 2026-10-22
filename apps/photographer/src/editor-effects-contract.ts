import {renderFreezeResult,type RenderFreezeResult} from './render-freeze';
export type EditorEffectsRequest={source:RenderFreezeResult['source'];recipe:RenderFreezeResult['recipe'];frozen_document_id:string;draft_document_id:string;draft_revision:number};
export function editorEffectsRequest(value:RenderFreezeResult):EditorEffectsRequest{
 const checked=renderFreezeResult(value,{document_id:value.draft_document_id,expected_revision:value.draft_revision,assume_profile:null});
 const revision=Number(checked.draft_revision);if(!Number.isSafeInteger(revision)||revision<1)throw new Error('EDITOR_EFFECT_REVISION_INVALID');
 return {source:structuredClone(checked.source),recipe:{...checked.recipe},frozen_document_id:checked.document_id,draft_document_id:checked.draft_document_id,draft_revision:revision};
}
export function readEditorEffectsAccepted(value:unknown):{run_id:string;status:'queued'}{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('EDITOR_EFFECT_RESPONSE_INVALID');
 const v=value as Record<string,unknown>;
 if(Object.keys(v).sort().join(',')!=='output_collection_id,run_id,status'||v.output_collection_id!=='00000000-0000-0000-0000-000000000000'||typeof v.run_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v.run_id)||v.run_id==='00000000-0000-0000-0000-000000000000'||v.status!=='queued')throw new Error('EDITOR_EFFECT_RESPONSE_INVALID');
 return {run_id:v.run_id,status:'queued'};
}

export type EditorEffectsStatus={build_run_id:string;state:'building'|'effects_ready'|'sealing'|'sealed'|'composing'|'complete'|'failed';build_status:string;seal_run_id:string|null;final_run_id:string|null;review_required:boolean};
export function readEditorEffectsStatus(value:unknown,build:string):EditorEffectsStatus{
 const validID=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('EDITOR_EFFECT_STATUS_INVALID');
 const v=value as Record<string,unknown>;
 if(Object.keys(v).sort().join(',')!=='build_run_id,build_status,final_run_id,review_required,seal_run_id,state'||!validID(v.build_run_id)||v.build_run_id.toLowerCase()!==build.toLowerCase()||typeof v.state!=='string'||typeof v.build_status!=='string'||!['building','effects_ready','sealing','sealed','composing','complete','failed'].includes(v.state)||!['queued','running','waiting_review','completed','failed','cancelled','cancelling','completed_with_errors'].includes(v.build_status)||typeof v.review_required!=='boolean'||(v.seal_run_id!==null&&!validID(v.seal_run_id))||(v.final_run_id!==null&&!validID(v.final_run_id))||(v.state==='complete'&&v.final_run_id===null))throw new Error('EDITOR_EFFECT_STATUS_INVALID');
 if((v.seal_run_id!==null&&String(v.seal_run_id).toLowerCase()===build.toLowerCase())||(v.final_run_id!==null&&(String(v.final_run_id).toLowerCase()===build.toLowerCase()||String(v.final_run_id).toLowerCase()===String(v.seal_run_id).toLowerCase()))||(['sealing','sealed','composing','complete'].includes(String(v.state))&&v.seal_run_id===null)||(['composing','complete'].includes(String(v.state))&&v.final_run_id===null))throw new Error('EDITOR_EFFECT_STATUS_INVALID');
 return {...v} as EditorEffectsStatus;
}
