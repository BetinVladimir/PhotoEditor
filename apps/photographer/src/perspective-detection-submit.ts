import {withDetectionJournalLock} from './perspective-detection-journal-lock';
import type {V2VersionBinding} from './catalog-types';
import type {OwnerAPI} from './owner-api';
import {freezePerspectiveLinesSubmission,readPerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionIntent,type PerspectiveLinesSubmissionReceipt} from './perspective-lines-submission';
export type DetectionJournal={getItem(key:string):Promise<string|null>;setItem(key:string,value:string):Promise<void>};
export function perspectiveDetectionJournalKey(origin:string,user:string,tenant:string,project:string,version:string):string{
 if(!origin||!user||!tenant||!project||!version)throw new Error('PERSPECTIVE_DETECTION_SCOPE_INVALID');
 return 'perspective-detection-v1:'+JSON.stringify([origin,user,tenant,project,version]);
}
// Persist key before POST, so a lost response/reload retries the same run.
export async function submitPerspectiveDetection(api:Pick<OwnerAPI,'submitPerspectiveLineDetection'>,journal:DetectionJournal,journalKey:string,tenant:string,project:string,request:PerspectiveLinesSubmissionIntent,newKey:()=>string,signal:AbortSignal):Promise<PerspectiveLinesSubmissionReceipt>{
 const body=freezePerspectiveLinesSubmission(tenant,project,request);
 return withDetectionJournalLock(journal,journalKey,signal,async()=>{
 const cancelled=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');};
 cancelled();const saved=await journal.getItem(journalKey);cancelled();
 let key:string;
 if(saved!==null){
  const record=readPerspectiveDetectionJournal(saved,tenant,project);
  if(record.body.snapshot_id!==body.snapshot_id||!sameDetectionBase(record.body.base,body.base))throw new Error('PERSPECTIVE_DETECTION_SOURCE_CHANGED');
  if(record.receipt)return record.receipt;
  key=record.key;
 }else{
  key=newKey();if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
  await journal.setItem(journalKey,JSON.stringify({schema_version:1,key,body}));cancelled();
 }
 const response=await api.submitPerspectiveLineDetection(project,body,key,signal);cancelled();
 const receipt=readPerspectiveLinesSubmissionReceipt(response);
 await journal.setItem(journalKey,JSON.stringify({schema_version:2,key,body,receipt}));cancelled();
 return receipt;
 });
}

export function readPerspectiveDetectionJournal(saved:string,tenant:string,project:string):{key:string;body:PerspectiveLinesSubmissionIntent;receipt:PerspectiveLinesSubmissionReceipt|null}{
 if(saved.length>16384)throw new Error('PERSPECTIVE_DETECTION_JOURNAL_INVALID');
 const record=JSON.parse(saved) as Record<string,unknown>;
 if(!record||typeof record!=='object'||Array.isArray(record)||!((record.schema_version===1&&Object.keys(record).sort().join(',')==='body,key,schema_version')||(record.schema_version===2&&Object.keys(record).sort().join(',')==='body,key,receipt,schema_version'))||typeof record.key!=='string'||!/^[A-Za-z0-9._:-]{1,128}$/.test(record.key))throw new Error('PERSPECTIVE_DETECTION_JOURNAL_INVALID');
 return {key:record.key,body:freezePerspectiveLinesSubmission(tenant,project,record.body as PerspectiveLinesSubmissionIntent),receipt:record.schema_version===2?readPerspectiveLinesSubmissionReceipt(record.receipt):null};
}
// Local recovery restores only the request. Server authorization is repeated on
// retry; a saved snapshot is never treated as current permission.
export async function recoverPerspectiveDetectionSnapshot(journal:DetectionJournal,journalKey:string,tenant:string,project:string,expected:V2VersionBinding,signal:AbortSignal):Promise<string|null>{
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const source=structuredClone(expected);
 const saved=await journal.getItem(journalKey);
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 if(saved===null)return null;
 const {body}=readPerspectiveDetectionJournal(saved,tenant,project);
 const checked=freezePerspectiveLinesSubmission(tenant,project,{schema_version:1,snapshot_id:body.snapshot_id,base:source});
 if(!sameDetectionBase(body.base,checked.base))throw new Error('PERSPECTIVE_DETECTION_SOURCE_CHANGED');
 return body.snapshot_id;
}

function sameDetectionBase(a:V2VersionBinding,b:V2VersionBinding):boolean{
 return a.asset_version_id===b.asset_version_id&&a.canvas_hash===b.canvas_hash&&a.source.artifact_id===b.source.artifact_id&&a.source.sha256===b.source.sha256;
}

export async function recoverPerspectiveDetectionReceipt(journal:DetectionJournal,journalKey:string,tenant:string,project:string,request:PerspectiveLinesSubmissionIntent,signal:AbortSignal):Promise<PerspectiveLinesSubmissionReceipt|null>{
 const expected=freezePerspectiveLinesSubmission(tenant,project,request);
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const saved=await journal.getItem(journalKey);
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 if(saved===null)return null;
 const record=readPerspectiveDetectionJournal(saved,tenant,project);
 if(record.body.snapshot_id!==expected.snapshot_id||!sameDetectionBase(record.body.base,expected.base))throw new Error('PERSPECTIVE_DETECTION_SOURCE_CHANGED');
 return record.receipt;
}
