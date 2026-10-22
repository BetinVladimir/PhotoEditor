import {readDeadline} from './read-deadline';
import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock} from './semantic-attempt-store';
import {freezeGlareSourceIntent,readGlareSourceTicket,type GlareSourceIntent,type GlareSourceTicket} from './glare-source-intent';
export type GlareSourceAttemptRecord={schema_version:1;project:string;key:string;body:GlareSourceIntent;ticket:GlareSourceTicket|null};
const prefix='photo.glare-source-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.glare-source-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
function decode(value:unknown):GlareSourceAttemptRecord{
 const r=value as GlareSourceAttemptRecord;
 if(!r||Object.keys(r).sort().join(',')!=='body,key,project,schema_version,ticket'||r.schema_version!==1||!uuid(r.project)||!r.key||r.key.length>128||!/^[!-~]+$/.test(r.key))throw new Error('GLARE_SOURCE_JOURNAL_INVALID');
 return {schema_version:1,project:r.project,key:r.key,body:freezeGlareSourceIntent(r.body),ticket:r.ticket===null?null:readGlareSourceTicket(r.ticket)};
}
export class GlareSourceAttemptStore{
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private project:string,private version:string){
  if(!origin||![user,tenant,project,version].every(uuid))throw new Error('GLARE_SOURCE_JOURNAL_SCOPE');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,version]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('GLARE_SOURCE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const g=await this.storage.getItem(generationKey(this.origin));this.current();if(g!==null&&!uuid(g))throw new Error('GLARE_SOURCE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=g;else if(this.generation!==g)throw new Error('GLARE_SOURCE_JOURNAL_EXPIRED');}
 private parse(raw:string){if(raw.length>262144)throw new Error('GLARE_SOURCE_JOURNAL_INVALID');const r=decode(JSON.parse(raw));if(r.project!==this.project||r.body.params_proposal.base.asset_version_id!==this.version)throw new Error('GLARE_SOURCE_JOURNAL_SCOPE');return r;}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>semanticJournalLock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.parse(raw);});}
 write(record:GlareSourceAttemptRecord){const next=this.parse(JSON.stringify(record));return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const old=this.parse(raw);if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('GLARE_SOURCE_JOURNAL_CONFLICT');}
  await this.storage.setItem(this.key,JSON.stringify(next));this.current();
 });}
}
export class GlareSourceAttempt{
 private persisted=false;
 private generation=0;
 private record:GlareSourceAttemptRecord;private phase:'ready'|'sending'|'unknown'|'accepted'='ready';
 constructor(project:string,key:string,body:GlareSourceIntent,private persistence:{write:(r:GlareSourceAttemptRecord)=>Promise<void>}){this.record=decode({schema_version:1,project,key,body,ticket:null});}
 get state(){return this.phase;}
 get canReplay(){return this.persisted||this.record.ticket!==null;}
 get snapshot(){return structuredClone(this.record);}
 static restore(r:GlareSourceAttemptRecord,persistence:{write:(r:GlareSourceAttemptRecord)=>Promise<void>}){const record=decode(r),attempt=new GlareSourceAttempt(record.project,record.key,record.body,persistence);attempt.record=record;attempt.phase=record.ticket?'accepted':'unknown';attempt.persisted=true;return attempt;}
 async submit(send:(project:string,body:GlareSourceIntent,key:string,signal?:AbortSignal)=>Promise<GlareSourceTicket>,signal?:AbortSignal){
  if(this.phase==='sending'||signal?.aborted)throw new Error('GLARE_SOURCE_ATTEMPT_CANCELLED');if(this.record.ticket)return structuredClone(this.record.ticket);
  this.phase='sending';const generation=++this.generation;try{
   await readDeadline(signal,()=>this.persistence.write(this.snapshot),15000);this.persisted=true;if(signal?.aborted)throw new Error('GLARE_SOURCE_ATTEMPT_CANCELLED');
   const ticket=readGlareSourceTicket(await readDeadline(signal,child=>send(this.record.project,freezeGlareSourceIntent(this.record.body),this.record.key,child).then(value=>{const reply=readGlareSourceTicket(value);if(child.aborted&&generation===this.generation&&this.record.ticket===null){this.record.ticket=structuredClone(reply);this.phase='accepted';void readDeadline(undefined,()=>this.persistence.write(this.snapshot),15000).catch(()=>{});}return reply;}),15000));
   this.record.ticket=structuredClone(ticket);this.phase='accepted';await readDeadline(signal,()=>this.persistence.write(this.snapshot),15000);if(signal?.aborted)throw new Error('GLARE_SOURCE_ATTEMPT_CANCELLED');return structuredClone(ticket);
  }catch(e){if(!this.record.ticket)this.phase='unknown';if(signal?.aborted)throw new Error('GLARE_SOURCE_ATTEMPT_CANCELLED');throw e;}
 }
}
export function clearGlareSourceAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>semanticJournalLock(origin,async()=>{
 await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}
}));}
