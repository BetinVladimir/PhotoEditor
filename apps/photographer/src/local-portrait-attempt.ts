import {readDeadline} from './read-deadline';
import {freezeLocalPortraitSelection,readLocalPortraitTicket,type LocalPortraitSelection,type LocalPortraitTicket} from './local-portrait-intent';

export type LocalPortraitAttemptRecord={schema_version:1;project:string;key:string;body:LocalPortraitSelection;ticket:LocalPortraitTicket|null};
export type LocalPortraitAttemptPersistence={write:(record:LocalPortraitAttemptRecord)=>Promise<void>};
export type LocalPortraitAttemptState='ready'|'sending'|'unknown'|'accepted';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';

/** Persist exact intent/key before send; an ambiguous response is replayed verbatim. */
export class LocalPortraitAttempt{
 private persisted=false;
 private generation=0;
 private record:LocalPortraitAttemptRecord;
 private phase:LocalPortraitAttemptState='ready';
 constructor(project:string,key:string,body:LocalPortraitSelection,private readonly persistence:LocalPortraitAttemptPersistence){
  if(!uuid(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_ATTEMPT_INVALID');
  this.record={schema_version:1,project,key,body:freezeLocalPortraitSelection(body),ticket:null};
 }
 get state(){return this.phase;}
 get canReplay(){return this.persisted||this.record.ticket!==null;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:LocalPortraitAttemptRecord,persistence:LocalPortraitAttemptPersistence){
  if(record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,schema_version,ticket')throw new Error('LOCAL_PORTRAIT_ATTEMPT_INVALID');
  const attempt=new LocalPortraitAttempt(record.project,record.key,record.body,persistence);
  if(record.ticket!==null){attempt.record.ticket=readLocalPortraitTicket(record.ticket);attempt.phase='accepted';}
  else attempt.phase='unknown';
  attempt.persisted=true;
  return attempt;
 }
 async submit(send:(project:string,body:LocalPortraitSelection,key:string,signal?:AbortSignal)=>Promise<LocalPortraitTicket>,signal?:AbortSignal){
  if(this.phase==='sending')throw new Error('LOCAL_PORTRAIT_ATTEMPT_BUSY');
  if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
  if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';const generation=++this.generation;
  try{
   await readDeadline(signal,()=>this.persistence.write(this.snapshot),15000);
   this.persisted=true;
   if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
   const ticket=readLocalPortraitTicket(await readDeadline(signal,child=>send(this.record.project,freezeLocalPortraitSelection(this.record.body),this.record.key,child).then(value=>{const reply=readLocalPortraitTicket(value);if(child.aborted&&generation===this.generation&&this.record.ticket===null){this.record.ticket=structuredClone(reply);this.phase='accepted';void readDeadline(undefined,()=>this.persistence.write(this.snapshot),15000).catch(()=>{});}return reply;}),15000));
   this.record.ticket=structuredClone(ticket);this.phase='accepted';
   await readDeadline(signal,()=>this.persistence.write(this.snapshot),15000);
   if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
   return structuredClone(ticket);
  }catch(error){if(this.record.ticket===null)this.phase='unknown';if(signal?.aborted)throw new Error('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');throw error;}
 }
}
