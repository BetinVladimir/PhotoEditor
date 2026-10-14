import {freezeMultiIntent,type MultiIntent,type MultiOperation} from './multi-intent';
import {renderInputSnapshotDigest,renderInputSnapshotResult,type RenderInputSnapshotResult} from './render-input-snapshot';
import type {DraftStorage} from './capture-draft-store';
import {MultiAttempt,type MultiAttemptRecord} from './multi-attempt';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.multi-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.multi-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class MultiAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private snapshot:string,private operation:MultiOperation,private lock:SemanticJournalLock=semanticJournalLock){
  if((operation!=='optical.panorama'&&operation!=='optical.focus_stack')||!origin||![user,tenant,project,snapshot].every(uuid))throw new Error('MULTI_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,snapshot,operation]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('MULTI_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('MULTI_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('MULTI_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('MULTI_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as MultiAttemptRecord;
  const record=MultiAttempt.restore(parsed,{write:async()=>{}}).snapshot;
  if(record.project!==this.project||record.body.snapshot_id!==this.snapshot||record.operation!==this.operation)throw new Error('MULTI_JOURNAL_SCOPE_INVALID');
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:MultiAttemptRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('MULTI_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.ticket===null)throw new Error('MULTI_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearMultiAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 }));
}

export type MultiPreparationRecord={schema_version:1;operation:MultiOperation;project:string;snapshot_key:string;intent_key:string;params:MultiIntent['params'];snapshot:RenderInputSnapshotResult|null;attempt:MultiAttemptRecord|null};
/** Same queue, generation and logout namespace as admission attempts, including native. */
export class MultiPreparationStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private operation:MultiOperation,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project].every(uuid)||!['optical.panorama','optical.focus_stack'].includes(operation))throw new Error('MULTI_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,'preparation',operation]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('MULTI_JOURNAL_EXPIRED');}
 private async fence(){this.current();const value=await this.storage.getItem(generationKey(this.origin));this.current();if(value!==null&&!uuid(value))throw new Error('MULTI_JOURNAL_INVALID');if(this.generation===undefined)this.generation=value;else if(this.generation!==value)throw new Error('MULTI_JOURNAL_EXPIRED');}
 private decode(raw:string):MultiPreparationRecord{
  if(raw.length>262144)throw new Error('MULTI_JOURNAL_INVALID');
  const v=JSON.parse(raw) as MultiPreparationRecord;
  if(!v||Object.keys(v).sort().join(',')!=='attempt,intent_key,operation,params,project,schema_version,snapshot,snapshot_key'||v.schema_version!==1||v.operation!==this.operation||v.project!==this.project||!uuid(v.snapshot_key)||!uuid(v.intent_key)||v.snapshot_key===v.intent_key)throw new Error('MULTI_JOURNAL_INVALID');
  // The snapshot key is used only to validate params before a server snapshot exists.
  const params=freezeMultiIntent(this.operation,{schema_version:1,snapshot_id:v.snapshot_key,params:v.params}).params;
  const snapshot=v.snapshot===null?null:renderInputSnapshotResult(v.snapshot,renderInputSnapshotDigest({sources:params.frames}),params.frames.length);
  const attempt=v.attempt===null?null:MultiAttempt.restore(v.attempt,{write:async()=>{}}).snapshot;
  if(attempt&&(!snapshot||attempt.key!==v.intent_key||attempt.project!==this.project||attempt.operation!==this.operation||JSON.stringify(attempt.body)!==JSON.stringify(freezeMultiIntent(this.operation,{schema_version:1,snapshot_id:snapshot.id,params}))))throw new Error('MULTI_JOURNAL_INVALID');
  return {schema_version:1,operation:this.operation,project:this.project,snapshot_key:v.snapshot_key,intent_key:v.intent_key,params,snapshot,attempt};
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:MultiPreparationRecord){
  const next=this.decode(JSON.stringify(record));
  return this.transaction(async()=>{
   await this.fence();const raw=await this.storage.getItem(this.key);this.current();
   if(raw!==null){const old=this.decode(raw);
    if(old.snapshot_key!==next.snapshot_key||old.intent_key!==next.intent_key||JSON.stringify(old.params)!==JSON.stringify(next.params)||old.snapshot!==null&&JSON.stringify(old.snapshot)!==JSON.stringify(next.snapshot)||old.attempt!==null&&(next.attempt===null||old.attempt.ticket!==null&&JSON.stringify(old.attempt)!==JSON.stringify(next.attempt)))throw new Error('MULTI_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,JSON.stringify(next));this.current();
  });
 }
 clear(acceptedKey:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();if(raw!==null){const old=this.decode(raw);if(old.intent_key!==acceptedKey||!old.attempt?.ticket)throw new Error('MULTI_JOURNAL_CONFLICT');}await this.storage.removeItem(this.key);this.current();});}
}
