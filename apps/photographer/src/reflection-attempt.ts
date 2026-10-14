import {freezeReflectionIntent,readReflectionTicket,type ReflectionIntent,type ReflectionTicket} from './reflection-intent';

export type ReflectionAttemptRecord={schema_version:1;project:string;key:string;body:ReflectionIntent;ticket:ReflectionTicket|null};
export type ReflectionAttemptPersistence={write:(record:ReflectionAttemptRecord)=>Promise<void>};
export type ReflectionAttemptState='ready'|'sending'|'unknown'|'accepted';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';

/** Persist exact intent/key before send; an ambiguous response is replayed verbatim. */
export class ReflectionAttempt{
 private record:ReflectionAttemptRecord;
 private phase:ReflectionAttemptState='ready';
 constructor(project:string,key:string,body:ReflectionIntent,private readonly persistence:ReflectionAttemptPersistence){
  if(!uuid(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('REFLECTION_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezeReflectionIntent(body),ticket:null};
 }
 get state(){return this.phase;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:ReflectionAttemptRecord,persistence:ReflectionAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('REFLECTION_ATTEMPT_INVALID');
  const attempt=new ReflectionAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=readReflectionTicket(record.ticket);attempt.phase='accepted';}
  else attempt.phase='unknown';
  return attempt;
 }
 async submit(send:(project:string,body:ReflectionIntent,key:string,signal?:AbortSignal)=>Promise<ReflectionTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('REFLECTION_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('REFLECTION_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';
  try{
   await this.persistence.write(this.snapshot);
   if(signal?.aborted)throw new Error('REFLECTION_ATTEMPT_CANCELLED');
   const ticket=readReflectionTicket(await send(this.record.project,freezeReflectionIntent(this.record.body),this.record.key,signal));
   await this.persistence.write({...this.snapshot,ticket:structuredClone(ticket)});
   this.record.ticket=ticket;this.phase='accepted';
   if(signal?.aborted)throw new Error('REFLECTION_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';throw error;}
 }
}
