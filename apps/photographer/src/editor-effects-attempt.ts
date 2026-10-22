import {canonicalJSON} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {editorEffectsRequest,readEditorEffectsStatus,type EditorEffectsStatus} from './editor-effects-contract';
import {renderFreezeResult,type RenderFreezeResult} from './render-freeze';

export type EditorEffectsAttempt={key:string;project:string;frozen:RenderFreezeResult;accepted:{run_id:string;status:'queued'}|null};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)&&value!=='00000000-0000-0000-0000-000000000000';

export function readEditorEffectsAttempt(value:unknown,version:string):EditorEffectsAttempt{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('EDITOR_EFFECT_ATTEMPT_INVALID');
 const v=value as EditorEffectsAttempt;
 if(Object.keys(v).sort().join(',')!=='accepted,frozen,key,project'||!uuid(v.key)||!uuid(v.project)||!v.frozen||v.frozen.source?.asset_version_id!==version)throw new Error('EDITOR_EFFECT_ATTEMPT_INVALID');
 const frozen=renderFreezeResult(v.frozen,{document_id:v.frozen.draft_document_id,expected_revision:v.frozen.draft_revision,assume_profile:null});
 editorEffectsRequest(frozen);
 if(v.accepted!==null&&(!v.accepted||Object.keys(v.accepted).sort().join(',')!=='run_id,status'||!uuid(v.accepted.run_id)||v.accepted.status!=='queued'))throw new Error('EDITOR_EFFECT_ATTEMPT_INVALID');
 return {key:v.key,project:v.project,frozen,accepted:v.accepted?{...v.accepted}:null};
}

export function prepareEditorEffectsAttempt(project:string,frozen:RenderFreezeResult,key:string):EditorEffectsAttempt{
 return readEditorEffectsAttempt({key,project,frozen:structuredClone(frozen),accepted:null},frozen.source.asset_version_id);
}

/** Persist before calling; a lost POST response leaves the exact command intact.
 * Accepted recovery is read-only and must never requeue the intermediate build. */
export async function submitEditorEffectsAttempt(api:Pick<OwnerAPI,'queueEditorEffects'|'editorEffectsStatus'>,value:EditorEffectsAttempt,signal:AbortSignal,current:()=>boolean){
 const attempt=readEditorEffectsAttempt(value,value.frozen.source.asset_version_id);
 const fence=()=>{signal.throwIfAborted();if(!current())throw new Error('EDITOR_EFFECT_SCOPE_EXPIRED');};
 fence();
 if(attempt.accepted){
  const status=readEditorEffectsStatus(await api.editorEffectsStatus(attempt.project,attempt.accepted.run_id,signal),attempt.accepted.run_id);fence();
  return {attempt,status};
 }
 const accepted=await api.queueEditorEffects(attempt.project,attempt.frozen,attempt.key,signal);fence();
 return {attempt:readEditorEffectsAttempt({...attempt,accepted},attempt.frozen.source.asset_version_id),status:null as EditorEffectsStatus|null};
}

export function sameEditorEffectsCommand(left:EditorEffectsAttempt,right:EditorEffectsAttempt){
 return canonicalJSON({...left,accepted:null})===canonicalJSON({...right,accepted:null});
}
