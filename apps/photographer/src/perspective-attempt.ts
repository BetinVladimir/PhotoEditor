import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
import {regionJobTicket,type RegionJobTicket} from './region-preview';
export type PerspectiveAttemptState='ready'|'sending'|'unknown'|'accepted';
export type PerspectiveAttemptRecord={schema_version:1;project:string;key:string;body:PerspectiveIntent;ticket:RegionJobTicket|null};
export type PerspectiveAttemptPersistence={write:(record:PerspectiveAttemptRecord)=>Promise<void>};
/** Persist before admission; ambiguous failures retain the original body/key. */
export class PerspectiveAttempt {
 private persisted=false;
 private record:PerspectiveAttemptRecord;
 private phase:PerspectiveAttemptState='ready';
 constructor(project:string,key:string,body:PerspectiveIntent,private readonly persistence:PerspectiveAttemptPersistence){
  if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezePerspectiveIntent(body),ticket:null};
 }
 get state(){return this.phase;}
 get canReplay(){return this.persisted||this.record.ticket!==null;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:PerspectiveAttemptRecord,persistence:PerspectiveAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('PERSPECTIVE_ATTEMPT_INVALID');
  const attempt=new PerspectiveAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=regionJobTicket(record.ticket);attempt.phase='accepted';}else attempt.phase='unknown';
  attempt.persisted=true;
  return attempt;
 }
 async submit(send:(project:string,body:PerspectiveIntent,key:string,signal?:AbortSignal)=>Promise<RegionJobTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('PERSPECTIVE_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('PERSPECTIVE_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';
  try{
   await this.persistence.write(this.snapshot);
   this.persisted=true;
   if(signal?.aborted)throw new Error('PERSPECTIVE_ATTEMPT_CANCELLED');
   const ticket=regionJobTicket(await send(this.record.project,freezePerspectiveIntent(this.record.body),this.record.key,signal));
   // Persist before navigation; if persistence fails, retry server acknowledgement.
   this.record.ticket=structuredClone(ticket);this.phase='accepted';
   await this.persistence.write(this.snapshot);
   if(signal?.aborted)throw new Error('PERSPECTIVE_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';throw error;}
 }
}
