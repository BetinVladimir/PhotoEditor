import {canonicalJSON} from '@photoeditor/api-client';
import type {DraftStorage} from './capture-draft-store';
import {freezeInterchangeCreate,readInterchangeCreateTicket,type InterchangeCreateRequest,type InterchangeCreateTicket} from './interchange-create';
export type InterchangeCreateAttempt={version:1;key:string;body:InterchangeCreateRequest;ticket:InterchangeCreateTicket|null};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.interchange-create-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.interchange-create-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class InterchangeCreateAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 private requestScope:string;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,request:InterchangeCreateRequest,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant].every(uuid))throw new Error('INTERCHANGE_CREATE_JOURNAL_SCOPE_INVALID');
  this.requestScope=canonicalJSON(freezeInterchangeCreate(request,'scope-validation'));
  this.key=prefix+JSON.stringify([origin,user,tenant,this.requestScope]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,request:InterchangeCreateRequest){
  this.current();
  if(tenant!==this.tenant||canonicalJSON(freezeInterchangeCreate(request,'scope-validation'))!==this.requestScope)throw new Error('INTERCHANGE_CREATE_JOURNAL_SCOPE_INVALID');
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('INTERCHANGE_CREATE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('INTERCHANGE_CREATE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('INTERCHANGE_CREATE_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('INTERCHANGE_CREATE_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as InterchangeCreateAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,key,ticket,version'||parsed.version!==1)throw new Error('INTERCHANGE_CREATE_JOURNAL_INVALID');
  const body=freezeInterchangeCreate(parsed.body,parsed.key);
  if(canonicalJSON(body)!==this.requestScope)throw new Error('INTERCHANGE_CREATE_JOURNAL_SCOPE_INVALID');
  const record:InterchangeCreateAttempt={version:1,key:parsed.key,body,ticket:parsed.ticket===null?null:readInterchangeCreateTicket(parsed.ticket)};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:InterchangeCreateAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.ticket===null)throw new Error('INTERCHANGE_CREATE_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearInterchangeCreateAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
