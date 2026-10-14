import {PerspectiveAttempt,type PerspectiveAttemptRecord} from './perspective-attempt';
import {PerspectiveLinesAttempt,type PerspectiveLinesAttemptRecord} from './perspective-lines-attempt';
import {perspectiveAutoIntent} from './perspective-lines';
import type {PerspectiveAttemptStore} from './perspective-attempt-store';
import type {PerspectiveLinesAttemptStore} from './perspective-lines-attempt-store';
/** Persist job identity before releasing acknowledged publication identity. */
export async function handoffPerspectiveLines(record:PerspectiveLinesAttemptRecord,jobs:Pick<PerspectiveAttemptStore,'read'|'write'>,lines:Pick<PerspectiveLinesAttemptStore,'clear'>,strength:number,crop:number,signal:AbortSignal){
 const original=PerspectiveLinesAttempt.restore(record,{write:async()=>{}}).snapshot;
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_LINES_HANDOFF_CANCELLED');};
 active();if(!original.receipt)throw new Error('PERSPECTIVE_LINES_UNRESOLVED');
 const body=perspectiveAutoIntent(original.receipt,strength,crop);
 const saved=await jobs.read();active();
 let attempt:PerspectiveAttempt;
 if(saved){
  const old=saved.body;
  if(saved.project!==original.project||old.params.mode!=='auto'||old.snapshot_id!==body.snapshot_id||old.params.lines?.artifact_id!==body.params.lines?.artifact_id||old.params.lines?.sha256!==body.params.lines?.sha256||JSON.stringify(old.params.base)!==JSON.stringify(body.params.base))throw new Error('PERSPECTIVE_LINES_HANDOFF_CONFLICT');
  attempt=PerspectiveAttempt.restore(saved,jobs);
 }else{
  attempt=new PerspectiveAttempt(original.project,crypto.randomUUID(),body,jobs);
  await jobs.write(attempt.snapshot);active();
 }
 await lines.clear(original.key);active();
 return attempt;
}
