import {canonicalJSON} from '@photoeditor/api-client';
import type {DraftStorage} from './capture-draft-store';
import {MultiApplyAttempt,type MultiApplyRecord} from './multi-apply-attempt';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.multi-apply.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const executions=new Map<string,Promise<unknown>>();
function executionQueue<T>(key:string,action:()=>Promise<T>):Promise<T>{
 const prior=executions.get(key)??Promise.resolve();
 const result=prior.catch(()=>{}).then(action);executions.set(key,result);
 void result.finally(()=>{if(executions.get(key)===result)executions.delete(key);}).catch(()=>{});
 return result;
}
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.multi-apply-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class MultiApplyStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private version:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,version].every(uuid))throw new Error('MULTI_APPLY_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,version]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('MULTI_APPLY_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('MULTI_APPLY_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('MULTI_APPLY_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('MULTI_APPLY_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as MultiApplyRecord;
  const record=MultiApplyAttempt.restore(parsed,{write:async()=>{}}).snapshot;
  if(record.review.proposal.tenant_id!==this.tenant||record.review.proposal.project_id!==this.project||record.review.proposal.producer_task_id!==this.version)throw new Error('MULTI_APPLY_JOURNAL_SCOPE_INVALID');
  return record;
 }
 /** Separate lock namespace avoids holding the short journal/logout lock over network IO. */
 execute<T>(action:(record:MultiApplyRecord|null)=>Promise<T>):Promise<T>{
  return executionQueue(this.key,()=>this.lock(this.origin+':multi-apply-execution:'+this.key,async()=>action(await this.read())));
 }
 assertCurrent(){return this.transaction(()=>this.fence());}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:MultiApplyRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(canonicalJSON(old.intent)!==canonicalJSON(next.intent)||canonicalJSON(old.review)!==canonicalJSON(next.review)||old.warnings!==next.warnings||canonicalJSON(old.selection)!==canonicalJSON(next.selection)||old.applied!==null&&canonicalJSON(old.applied)!==canonicalJSON(next.applied)||old.selected&&!next.selected)throw new Error('MULTI_APPLY_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(qualityResult:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.review.quality_result_id!==qualityResult||record.applied===null||record.selection!==null&&!record.selected)throw new Error('MULTI_APPLY_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearMultiApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
/** Read-only discovery, independent of preparation/tickets. No malformed matching record is silently discarded. */
export function listMultiApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},scope:{origin:string;user:string;tenant:string;project:string;collection:string;cursor?:string},lock:SemanticJournalLock=semanticJournalLock){
 const {origin,user,tenant,project,collection,cursor}=scope;
 if(!origin||![user,tenant,project,collection].every(uuid)||cursor!==undefined&&!uuid(cursor))throw new Error('MULTI_APPLY_JOURNAL_SCOPE_INVALID');
 const epoch=epochs.get(origin)??0;
 return queue(()=>lock(origin,async()=>{
  const current=()=>{if(epoch!==(epochs.get(origin)??0))throw new Error('MULTI_APPLY_JOURNAL_EXPIRED');};
  current();const generation=await storage.getItem(generationKey(origin));current();
  if(generation!==null&&!uuid(generation))throw new Error('MULTI_APPLY_JOURNAL_INVALID');
  const candidates:{key:string;producer:string}[]=[];
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix))continue;
   let parsed:unknown;try{parsed=JSON.parse(key.slice(prefix.length));}catch{continue;}
   if(!Array.isArray(parsed)||parsed.length!==5||parsed[0]!==origin||parsed[1]!==user||parsed[2]!==tenant||parsed[3]!==project)continue;
   if(!uuid(parsed[4])||key!==prefix+JSON.stringify(parsed))throw new Error('MULTI_APPLY_JOURNAL_INVALID');
   if(cursor===undefined||parsed[4]>cursor)candidates.push({key,producer:parsed[4]});
  }
  candidates.sort((a,b)=>a.producer<b.producer?-1:a.producer>b.producer?1:0);
  const records:MultiApplyRecord[]=[];let nextCursor:string|null=null,scanned=0;
  for(let i=0;i<candidates.length;i++){
   current();const candidate=candidates[i],raw=await storage.getItem(candidate.key);current();
   if(raw!==null){
    if(raw.length>262144)throw new Error('MULTI_APPLY_JOURNAL_INVALID');
    const record=MultiApplyAttempt.restore(JSON.parse(raw),{write:async()=>{}}).snapshot;
    if(record.review.proposal.tenant_id!==tenant||record.review.proposal.project_id!==project||record.review.proposal.producer_task_id!==candidate.producer)throw new Error('MULTI_APPLY_JOURNAL_SCOPE_INVALID');
    if(record.selection?.collection===collection)records.push(record);
   }
   scanned++;
   if(records.length===20||scanned===200){if(i+1<candidates.length)nextCursor=candidate.producer;break;}
  }
  const endGeneration=await storage.getItem(generationKey(origin));current();
  if(endGeneration!==generation)throw new Error('MULTI_APPLY_JOURNAL_EXPIRED');
  return {records:structuredClone(records),nextCursor};
 }));
}
