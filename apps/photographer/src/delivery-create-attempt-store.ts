import type {DraftStorage} from './capture-draft-store';
import {freezeDeliveryCreate,readDeliveryCreateTicket,type DeliveryCreateRequest,type DeliveryCreateTicket} from './delivery-create';
export type DeliveryCreateAttempt={version:1;key:string;body:DeliveryCreateRequest;ticket:DeliveryCreateTicket|null};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.delivery-create-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.delivery-create-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class DeliveryCreateAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private snapshot:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,snapshot].every(uuid))throw new Error('DELIVERY_CREATE_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,snapshot]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,project:string,snapshot:string){
  this.current();
  if(tenant!==this.tenant||project!==this.project||snapshot!==this.snapshot)throw new Error('DELIVERY_CREATE_JOURNAL_SCOPE_INVALID');
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('DELIVERY_CREATE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('DELIVERY_CREATE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('DELIVERY_CREATE_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>262144)throw new Error('DELIVERY_CREATE_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as DeliveryCreateAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,key,ticket,version'||parsed.version!==1)throw new Error('DELIVERY_CREATE_JOURNAL_INVALID');
  const body=freezeDeliveryCreate(parsed.body,parsed.key);
  if(body.project_id!==this.project||body.snapshot_id!==this.snapshot)throw new Error('DELIVERY_CREATE_JOURNAL_SCOPE_INVALID');
  const record:DeliveryCreateAttempt={version:1,key:parsed.key,body,ticket:parsed.ticket===null?null:readDeliveryCreateTicket(parsed.ticket)};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:DeliveryCreateAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('DELIVERY_CREATE_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.ticket===null)throw new Error('DELIVERY_CREATE_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
 /** Release this snapshot for another create only after the same ticket was saved. */
 clearAccepted(ticket:DeliveryCreateTicket){
  const accepted=readDeliveryCreateTicket(ticket);
  return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw===null)return;
  const saved=this.decode(raw);
  if(saved.ticket===null||JSON.stringify(saved.ticket)!==JSON.stringify(accepted))throw new Error('DELIVERY_CREATE_JOURNAL_CONFLICT');
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearDeliveryCreateAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
