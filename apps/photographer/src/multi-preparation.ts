import {MultiAttempt} from './multi-attempt';
import {MultiPreparationStore,type MultiPreparationRecord} from './multi-attempt-store';
import {freezeMultiIntent,type MultiIntent,type MultiOperation} from './multi-intent';
import {renderInputSnapshotDigest,renderInputSnapshotResult,type RenderInputSnapshotRequest,type RenderInputSnapshotResult} from './render-input-snapshot';
import type {RegionJobTicket} from './region-preview';
export type MultiPreparationAPI={
 createRenderInputSnapshot:(project:string,body:RenderInputSnapshotRequest,key:string,signal?:AbortSignal)=>Promise<RenderInputSnapshotResult>;
 submitMulti:(operation:MultiOperation,project:string,body:MultiIntent,key:string,signal?:AbortSignal)=>Promise<RegionJobTicket>;
};
/** Frozen selection precedes both POSTs; a retry never derives keys from current UI. */
export async function beginMultiPreparation(store:MultiPreparationStore,operation:MultiOperation,project:string,params:MultiIntent['params'],keys={snapshot:crypto.randomUUID(),intent:crypto.randomUUID()}){
 const record:MultiPreparationRecord={schema_version:1,operation,project,snapshot_key:keys.snapshot,intent_key:keys.intent,params:freezeMultiIntent(operation,{schema_version:1,snapshot_id:keys.snapshot,params}).params,snapshot:null,attempt:null};
 await store.write(record);return record;
}
export class MultiPreparation {
 private sending=false;
 constructor(private store:MultiPreparationStore,private api:MultiPreparationAPI){}
 async submit(signal?:AbortSignal){
  if(this.sending)throw new Error('MULTI_ATTEMPT_BUSY');
  this.sending=true;
  try{
   let record=await this.store.read();if(!record)throw new Error('MULTI_PREPARATION_MISSING');
   if(signal?.aborted)throw new Error('MULTI_ATTEMPT_CANCELLED');
   if(record.snapshot===null){
    const request={sources:structuredClone(record.params.frames)};
    const digest=renderInputSnapshotDigest(request),count=request.sources.length;
    const response=await this.api.createRenderInputSnapshot(record.project,request,record.snapshot_key,signal);
    // Own the validated receipt before persistence yields to another callback.
    const snapshot=renderInputSnapshotResult(response,digest,count);
    record={...record,snapshot};await this.store.write(record);
   }
   if(signal?.aborted)throw new Error('MULTI_ATTEMPT_CANCELLED');
   const body=freezeMultiIntent(record.operation,{schema_version:1,snapshot_id:record.snapshot!.id,params:record.params});
   const current=record;
   const attempt=record.attempt?MultiAttempt.restore(record.attempt,{write:async value=>{await this.store.write({...current,attempt:value});}}):new MultiAttempt(record.operation,record.project,record.intent_key,body,{write:async value=>{await this.store.write({...current,attempt:value});}});
   return await attempt.submit((operation,project,intent,key,abort)=>this.api.submitMulti(operation,project,intent,key,abort),signal);
  }finally{this.sending=false;}
 }
}
