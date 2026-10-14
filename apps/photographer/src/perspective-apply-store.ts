import type {DraftStorage} from './capture-draft-store';
import {PerspectiveApplyAttempt,type PerspectiveApplyRecord} from './perspective-apply-attempt';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.perspective-apply.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.perspective-apply-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class PerspectiveApplyStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private version:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,version].every(uuid))throw new Error('PERSPECTIVE_APPLY_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,version]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('PERSPECTIVE_APPLY_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('PERSPECTIVE_APPLY_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('PERSPECTIVE_APPLY_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('PERSPECTIVE_APPLY_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as PerspectiveApplyRecord;
  const record=PerspectiveApplyAttempt.restore(parsed,{write:async()=>{}}).snapshot;
  if(record.review.proposal.tenant_id!==this.tenant||record.review.proposal.project_id!==this.project||record.review.proposal.base.asset_version_id!==this.version)throw new Error('PERSPECTIVE_APPLY_JOURNAL_SCOPE_INVALID');
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:PerspectiveApplyRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(JSON.stringify(old.review)!==JSON.stringify(next.review)||old.warnings!==next.warnings||JSON.stringify(old.selection)!==JSON.stringify(next.selection)||old.applied!==null&&JSON.stringify(old.applied)!==JSON.stringify(next.applied)||old.selected&&!next.selected)throw new Error('PERSPECTIVE_APPLY_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(qualityResult:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.review.quality_result_id!==qualityResult||record.applied===null||record.selection!==null&&!record.selected)throw new Error('PERSPECTIVE_APPLY_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearPerspectiveApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
