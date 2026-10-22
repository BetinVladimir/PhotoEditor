import {APIError} from '@photoeditor/api-client';
import {withDetectionJournalLock} from './perspective-detection-journal-lock';
import type {OwnerAPI} from './owner-api';
import {readPerspectiveDetectionJournal,type DetectionJournal} from './perspective-detection-submit';
import {freezePerspectiveLinesSubmission,readPerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionIntent,type PerspectiveLinesSubmissionReceipt} from './perspective-lines-submission';
import {observePerspectiveDetection} from './perspective-detection-status';
import {withPerspectiveDetectionDeadline} from './perspective-detection-deadline';
// Explicit terminal restart only. Unknown/pending work retains its original key.
export async function restartPerspectiveDetection(api:Pick<OwnerAPI,'get'|'perspectiveDetectedLines'|'createRenderInputSnapshot'> & Partial<Pick<OwnerAPI,'submitPerspectiveLineDetection'>>,journal:DetectionJournal,journalKey:string,tenant:string,project:string,request:PerspectiveLinesSubmissionIntent,newKey:()=>string,signal:AbortSignal):Promise<PerspectiveLinesSubmissionIntent>{
 const expected=freezePerspectiveLinesSubmission(tenant,project,request);
 return withDetectionJournalLock(journal,journalKey,signal,async()=>{
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');};active();
 const saved=await journal.getItem(journalKey);active();
 if(saved===null)throw new Error('PERSPECTIVE_DETECTION_RESTART_INVALID');
 const record=readPerspectiveDetectionJournal(saved,tenant,project);
 if(record.body.snapshot_id!==expected.snapshot_id||record.body.base.asset_version_id!==expected.base.asset_version_id||record.body.base.canvas_hash!==expected.base.canvas_hash||record.body.base.source.artifact_id!==expected.base.source.artifact_id||record.body.base.source.sha256!==expected.base.source.sha256)throw new Error('PERSPECTIVE_DETECTION_RESTART_INVALID');
 if(record.receipt){
 const state=await observePerspectiveDetection(api,tenant,project,expected,record.receipt,signal);active();
 if(state!=='failed'&&state!=='cancelled'&&state!=='unsupported')throw new Error('PERSPECTIVE_DETECTION_NOT_TERMINAL');
 }else{
 // Repeat the original POST, never infer expiry from age or a generic 410.
 if(!api.submitPerspectiveLineDetection)throw new Error('PERSPECTIVE_DETECTION_RESTART_INVALID');
 let admitted:unknown;let expired=false;
 try{admitted=await withPerspectiveDetectionDeadline(signal,30_000,child=>api.submitPerspectiveLineDetection!(project,expected,record.key,child));}
 catch(error){active();if(!isPerspectiveDetectionDraftExpired(error))throw error;expired=true;}
 active();
 if(!expired){
 const receipt=readPerspectiveLinesSubmissionReceipt(admitted);
 await journal.setItem(journalKey,JSON.stringify({schema_version:2,key:record.key,body:record.body,receipt}));active();
 throw new PerspectiveDetectionAdmissionRecovered(receipt);
 }
 }
 const key=newKey();if(!/^[A-Za-z0-9._:-]{1,119}$/.test(key)||key===record.key)throw new Error('PERSPECTIVE_DETECTION_RESTART_KEY_INVALID');
 const snapshot=await withPerspectiveDetectionDeadline(signal,30_000,child=>api.createRenderInputSnapshot(project,{sources:[expected.base]},key+':snapshot',child));active();
 const body=freezePerspectiveLinesSubmission(tenant,project,{schema_version:1,snapshot_id:snapshot.id,base:expected.base});
 await journal.setItem(journalKey+':history:'+(record.receipt?.run_id??('draft:'+record.key)),saved);active();
 await journal.setItem(journalKey,JSON.stringify({schema_version:1,key,body}));active();
 return body;
 });
}

export function isPerspectiveDetectionDraftExpired(error:unknown):boolean{
 return error instanceof APIError&&error.status===410&&error.code==='PERSPECTIVE_DETECTION_DRAFT_EXPIRED';
}

// A successful retry is an admission recovery, not a failed restart. Carry an
// owned, validated receipt so the mounted editor resumes polling immediately.
export class PerspectiveDetectionAdmissionRecovered extends Error{
 readonly receipt:PerspectiveLinesSubmissionReceipt;
 constructor(receipt:unknown){
  super('PERSPECTIVE_DETECTION_NOT_TERMINAL');
  this.receipt=Object.freeze(readPerspectiveLinesSubmissionReceipt(receipt));
 }
}
