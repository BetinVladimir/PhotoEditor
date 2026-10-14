import type {DraftStorage} from './capture-draft-store';
import {freezeSceneApplyDecision,type SceneApplyDecision} from './scene-apply';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

export type SceneApplyAttempt={schema_version:1;session_id:string;analysis_id:string;key:string;body:SceneApplyDecision;ticket:{run_id:string}|null};
const prefix='photo.scene-apply-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.scene-apply-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}

export class SceneApplyAttemptStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private session:string,private analysis:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,session,analysis].every(uuid))throw new Error('SCENE_APPLY_JOURNAL_SCOPE_INVALID');
  this.storageKey=prefix+JSON.stringify([origin,user,tenant,session,analysis]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('SCENE_APPLY_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('SCENE_APPLY_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('SCENE_APPLY_JOURNAL_EXPIRED');}
 private decode(raw:string):SceneApplyAttempt{
  if(raw.length>262144)throw new Error('SCENE_APPLY_JOURNAL_INVALID');
  const value=JSON.parse(raw) as SceneApplyAttempt;
  if(!fields(value,['schema_version','session_id','analysis_id','key','body','ticket'])||value.schema_version!==1||value.session_id!==this.session||value.analysis_id!==this.analysis||!uuid(value.key)||value.ticket!==null&&(!fields(value.ticket,['run_id'])||!uuid(value.ticket.run_id)))throw new Error('SCENE_APPLY_JOURNAL_INVALID');
  return {schema_version:1,session_id:value.session_id,analysis_id:value.analysis_id,key:value.key,body:freezeSceneApplyDecision(value.body),ticket:value.ticket===null?null:{run_id:value.ticket.run_id}};
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 write(value:SceneApplyAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{
  await this.fence();const previous=await this.storage.getItem(this.storageKey);this.current();
  if(previous!==null){const old=this.decode(previous);if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('SCENE_APPLY_JOURNAL_CONFLICT');}
  await this.storage.setItem(this.storageKey,raw);this.current();
 });}
 clear(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const record=this.decode(raw);if(record.key!==key||record.ticket===null)throw new Error('SCENE_APPLY_JOURNAL_CONFLICT');}await this.storage.removeItem(this.storageKey);this.current();});}
 /** A definitive server conflict rejects the submitted decision; an uncertain network error never calls this. */
 discardRejected(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const record=this.decode(raw);if(record.key!==key||record.ticket!==null)throw new Error('SCENE_APPLY_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}
}

export function clearSceneApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));
}
