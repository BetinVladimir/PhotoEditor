import {readCollectionApplyAccepted} from './collection-apply-contract';
import type {DraftStorage} from './capture-draft-store';
import type {ApplyAccepted} from './catalog-types';
export type CollectionApplyRecord={schema_version:1;collection:{id:string;tenant_id:string;project_id:string;revision:string};version:string;strength:number;name:string;key:string;accepted:ApplyAccepted|null};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.collection-apply.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.collection-apply-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class CollectionApplyStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private collection:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,collection].every(uuid))throw new Error('COLLECTION_APPLY_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,collection]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('COLLECTION_APPLY_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('COLLECTION_APPLY_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('COLLECTION_APPLY_JOURNAL_EXPIRED');}
 private decode(raw:string):CollectionApplyRecord{
  if(raw.length>16384)throw new Error('COLLECTION_APPLY_JOURNAL_INVALID');
  const record=JSON.parse(raw) as CollectionApplyRecord,c=record?.collection;
  if(!record||Object.keys(record).sort().join(',')!=='accepted,collection,key,name,schema_version,strength,version'||record.schema_version!==1||!c||Object.keys(c).sort().join(',')!=='id,project_id,revision,tenant_id'||c.id!==this.collection||c.tenant_id!==this.tenant||c.project_id!==this.project||!uuid(record.version)||!uuid(record.key)||typeof c.revision!=='string'||!/^[1-9][0-9]*$/.test(c.revision)||BigInt(c.revision)>9007199254740991n||!Number.isFinite(record.strength)||record.strength<0||record.strength>1||typeof record.name!=='string'||!record.name.trim()||record.name!==record.name.trim()||record.name.length>240||Array.from(record.name).length>120)throw new Error('COLLECTION_APPLY_JOURNAL_INVALID');
  if(record.accepted!==null)readCollectionApplyAccepted(record.accepted);
  return structuredClone(record);
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:CollectionApplyRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(JSON.stringify({...old,accepted:null})!==JSON.stringify({...next,accepted:null})||old.accepted!==null&&JSON.stringify(old.accepted)!==JSON.stringify(next.accepted))throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 // Serialize the whole admission across views/tabs, not only storage writes.
 // A lost HTTP reply retains the exact pending key; a stored receipt skips HTTP.
 resume(record:CollectionApplyRecord,send:(pending:CollectionApplyRecord)=>Promise<ApplyAccepted>){
  const requested=this.decode(JSON.stringify(record));
  return this.transaction(async()=>{
   await this.fence();
   const raw=await this.storage.getItem(this.key);this.current();
   let current=raw===null?requested:this.decode(raw);
   if(JSON.stringify({...current,accepted:null})!==JSON.stringify({...requested,accepted:null}))throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');
   if(requested.accepted!==null&&JSON.stringify(requested.accepted)!==JSON.stringify(current.accepted))throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');
   if(current.accepted!==null)return structuredClone(current);
   if(requested.accepted!==null)throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');
   if(raw===null){await this.storage.setItem(this.key,JSON.stringify(current));this.current();}
   await this.fence();
   const receipt=await send(structuredClone(current));
   if(!receipt)throw new Error('COLLECTION_APPLY_JOURNAL_INVALID');
   await this.fence();
   current=this.decode(JSON.stringify({...current,accepted:receipt}));
   await this.storage.setItem(this.key,JSON.stringify(current));this.current();
   return structuredClone(current);
  });
 }
 /** Release only the exact pending request after a definitive server rejection. */
 discardRejected(key:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==key||record.accepted!==null)throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');await this.storage.removeItem(this.key);this.current();}
 });}
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(key:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==key||record.accepted===null)throw new Error('COLLECTION_APPLY_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearCollectionApplyAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
