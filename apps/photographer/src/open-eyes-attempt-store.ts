import type {DraftStorage} from './capture-draft-store';
import {OpenEyesAttempt,type OpenEyesAttemptRecord} from './open-eyes-attempt';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

const prefix='photo.open-eyes-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.open-eyes-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}

export class OpenEyesAttemptStore{
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,
  private project:string,private baseVersion:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,baseVersion].every(uuid))throw new Error('OPEN_EYES_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,baseVersion]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('OPEN_EYES_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('OPEN_EYES_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('OPEN_EYES_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('OPEN_EYES_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as OpenEyesAttemptRecord;
  const record=OpenEyesAttempt.restore(parsed,{write:async()=>{}}).snapshot;
  if(record.project!==this.project||record.body.params_proposal.base.asset_version_id!==this.baseVersion)throw new Error('OPEN_EYES_JOURNAL_SCOPE_INVALID');
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:OpenEyesAttemptRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('OPEN_EYES_JOURNAL_CONFLICT');}
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 clear(acceptedKey:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.ticket===null)throw new Error('OPEN_EYES_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}

export function clearOpenEyesAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
