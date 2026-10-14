import {freezeLocalPortraitSelection,readLocalPortraitTicket,type LocalPortraitSelection,type LocalPortraitTicket} from './local-portrait-intent';

export type LocalPortraitAttemptRecord={schema_version:1;project:string;key:string;body:LocalPortraitSelection;ticket:LocalPortraitTicket|null};
export type LocalPortraitAttemptPersistence={write:(record:LocalPortraitAttemptRecord)=>Promise<void>};
export type LocalPortraitAttemptState='ready'|'sending'|'unknown'|'accepted';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';

/** Persist exact intent/key before send; an ambiguous response is replayed verbatim. */
export class LocalPortraitAttempt{
 private record:LocalPortraitAttemptRecord;
 private phase:LocalPortraitAttemptState='ready';
 constructor(project:string,key:string,body:LocalPortraitSelection,private readonly persistence:LocalPortraitAttemptPersistence){
  if(!uuid(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezeLocalPortraitSelection(body),ticket:null};
 }
 get state(){return this.phase;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:LocalPortraitAttemptRecord,persistence:LocalPortraitAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('LOCAL_PORTRAIT_ATTEMPT_INVALID');
  const attempt=new LocalPortraitAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=readLocalPortraitTicket(record.ticket);attempt.phase='accepted';}
  else attempt.phase='unknown';
  return attempt;
 }
 async submit(send:(project:string,body:LocalPortraitSelection,key:string,signal?:AbortSignal)=>Promise<LocalPortraitTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('LOCAL_PORTRAIT_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';
  try{
   await this.persistence.write(this.snapshot);
   if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
   const ticket=readLocalPortraitTicket(await send(this.record.project,freezeLocalPortraitSelection(this.record.body),this.record.key,signal));
   await this.persistence.write({...this.snapshot,ticket:structuredClone(ticket)});
   this.record.ticket=ticket;this.phase='accepted';
   if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';throw error;}
 }
}
