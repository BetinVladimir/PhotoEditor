import {APIError} from '@photoeditor/api-client';
import type {EditorDraft} from './catalog-types';
import {acceptEditorDraftSave,currentEditorSnapshot,prepareEditorDraftSave,type EditorState} from './editor-store';
export type BatchSaveTarget={assetId:string;draft:EditorDraft;submitted:EditorState;current():EditorState|undefined;accept(state:EditorState,draft:EditorDraft):void};
export type BatchSaveResult={versionId:string;status:'saved'|'saved_local_changes'|'saved_refresh_required'|'conflict'|'failed'|'cancelled'};
/** Sequential CAS writes; stop new writes on cancellation, but acknowledge in-flight commits. */
export async function saveBatchCommand(targets:readonly BatchSaveTarget[],save:(assetId:string,draft:EditorDraft)=>Promise<EditorDraft>,signal:AbortSignal):Promise<BatchSaveResult[]> {
 if(targets.length<1||targets.length>5000||new Set(targets.map(x=>x.submitted.versionId)).size!==targets.length)throw new Error('BATCH_SELECTION_INVALID');
 // Snapshot all requests before the first await; no later target can silently change payload.
 const jobs=targets.map(target=>{const submitted=structuredClone(target.submitted),draft=structuredClone(target.draft);if(!target.assetId)throw new Error("BATCH_ASSET_INVALID");return {target,assetId:target.assetId,submitted,request:prepareEditorDraftSave(submitted,draft,currentEditorSnapshot(submitted).params)};});
 const results:BatchSaveResult[]=[];
 for(const {target,assetId,submitted,request} of jobs){
  const versionId=submitted.versionId;
  if(signal.aborted){results.push({versionId,status:'cancelled'});continue;}
  const before=target.current();
  if(!before||before.versionId!==versionId||before.sourceSha!==submitted.sourceSha||before.draftRevision!==submitted.draftRevision||before.requestRevision!==submitted.requestRevision){results.push({versionId,status:'conflict'});continue;}
  let saved:EditorDraft;
  try{saved=await save(assetId,request);}catch(error){results.push({versionId,status:error instanceof APIError&&[409,412].includes(error.status)?'conflict':'failed'});continue;}
  // The server may have committed even if cancellation/navigation occurred meanwhile.
  const current=target.current();
  if(!current){results.push({versionId,status:'saved_refresh_required'});continue;}
  try{
   const accepted=acceptEditorDraftSave(current,submitted,saved);
   target.accept(accepted.state,saved);
   results.push({versionId,status:accepted.replaceRecipe?'saved':'saved_local_changes'});
  }catch{results.push({versionId,status:'saved_refresh_required'});}
 }
 return results;
}
