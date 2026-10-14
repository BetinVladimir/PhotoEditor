import {freezeOpenEyesIntent,readOpenEyesTicket,type OpenEyesIntent,type OpenEyesTicket} from './open-eyes-intent';

export type OpenEyesAttemptRecord={schema_version:1;project:string;key:string;body:OpenEyesIntent;ticket:OpenEyesTicket|null};
export type OpenEyesAttemptPersistence={write:(record:OpenEyesAttemptRecord)=>Promise<void>};
export type OpenEyesAttemptState='ready'|'sending'|'unknown'|'accepted';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';

/** Persist exact intent/key before send; an ambiguous response is replayed verbatim. */
export class OpenEyesAttempt{
 private record:OpenEyesAttemptRecord;
 private phase:OpenEyesAttemptState='ready';
 constructor(project:string,key:string,body:OpenEyesIntent,private readonly persistence:OpenEyesAttemptPersistence){
  if(!uuid(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('OPEN_EYES_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezeOpenEyesIntent(body),ticket:null};
 }
 get state(){return this.phase;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:OpenEyesAttemptRecord,persistence:OpenEyesAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('OPEN_EYES_ATTEMPT_INVALID');
  const attempt=new OpenEyesAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=readOpenEyesTicket(record.ticket);attempt.phase='accepted';}
  else attempt.phase='unknown';
  return attempt;
 }
 async submit(send:(project:string,body:OpenEyesIntent,key:string,signal?:AbortSignal)=>Promise<OpenEyesTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('OPEN_EYES_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('OPEN_EYES_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';
  try{
   await this.persistence.write(this.snapshot);
   if(signal?.aborted)throw new Error('OPEN_EYES_ATTEMPT_CANCELLED');
   const ticket=readOpenEyesTicket(await send(this.record.project,freezeOpenEyesIntent(this.record.body),this.record.key,signal));
   await this.persistence.write({...this.snapshot,ticket:structuredClone(ticket)});
   this.record.ticket=ticket;this.phase='accepted';
   if(signal?.aborted)throw new Error('OPEN_EYES_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';throw error;}
 }
}
