import {freezeRelightIntent,type RelightIntent} from './relight-intent';
import {regionJobTicket,type RegionJobTicket} from './region-preview';
export type RelightAttemptState='ready'|'sending'|'unknown'|'accepted';
export type RelightAttemptRecord={schema_version:1;project:string;key:string;body:RelightIntent;ticket:RegionJobTicket|null};
export type RelightAttemptPersistence={write:(record:RelightAttemptRecord)=>Promise<void>};
/** Persist before admission; ambiguous failures retain the original body/key. */
export class RelightAttempt {
 private record:RelightAttemptRecord;
 private phase:RelightAttemptState='ready';
 constructor(project:string,key:string,body:RelightIntent,private readonly persistence:RelightAttemptPersistence){
  if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('RELIGHT_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezeRelightIntent(body),ticket:null};
 }
 get state(){return this.phase;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:RelightAttemptRecord,persistence:RelightAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('RELIGHT_ATTEMPT_INVALID');
  const attempt=new RelightAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=regionJobTicket(record.ticket);attempt.phase='accepted';}else attempt.phase='unknown';
  return attempt;
 }
 async submit(send:(project:string,body:RelightIntent,key:string,signal?:AbortSignal)=>Promise<RegionJobTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('RELIGHT_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('RELIGHT_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';
  try{
   await this.persistence.write(this.snapshot);
   if(signal?.aborted)throw new Error('RELIGHT_ATTEMPT_CANCELLED');
   const ticket=regionJobTicket(await send(this.record.project,freezeRelightIntent(this.record.body),this.record.key,signal));
   // Persist before navigation; if persistence fails, retry server acknowledgement.
   await this.persistence.write({...this.snapshot,ticket:structuredClone(ticket)});
   this.record.ticket=ticket;this.phase='accepted';
   if(signal?.aborted)throw new Error('RELIGHT_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';throw error;}
 }
}
