import type {DraftStorage} from './capture-draft-store';
import {freezeCollectionVersion,type CollectionVersionSelection} from './collection-version';
import {resumeCollectionVersion,type CollectionVersionTransport} from './collection-version-recovery';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.collection-version.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
const uuid=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.collection-version-generation.v1:'+JSON.stringify(origin);
export class CollectionVersionStore{
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,review:string,private decisionHash:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,review].every(uuid)||!/^[0-9a-f]{64}$/.test(decisionHash))throw new Error('COLLECTION_VERSION_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,review]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('COLLECTION_VERSION_EXPIRED');}
 private async fence(){this.current();const next=await this.storage.getItem(generationKey(this.origin));this.current();if(next!==null&&!uuid(next))throw new Error('COLLECTION_VERSION_INVALID');if(this.generation===undefined)this.generation=next;else if(next!==this.generation)throw new Error('COLLECTION_VERSION_EXPIRED');}
 private decode(raw:string){
  if(raw.length>8192)throw new Error('COLLECTION_VERSION_INVALID');
  const v=JSON.parse(raw);
  if(!v||Object.keys(v).sort().join(',')!=='decision_hash,selection,version'||v.version!==1||v.decision_hash!==this.decisionHash||!v.selection||Object.keys(v.selection).sort().join(',')!=='asset,collection,expectedVersion,item,revision,version')throw new Error('COLLECTION_VERSION_INVALID');
  return freezeCollectionVersion(v.selection);
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw)});}
 write(selection:CollectionVersionSelection){
  const next=freezeCollectionVersion(selection),raw=JSON.stringify({version:1,decision_hash:this.decisionHash,selection:next});
  return this.transaction(async()=>{await this.fence();const old=await this.storage.getItem(this.key);this.current();if(old!==null&&JSON.stringify(this.decode(old))!==JSON.stringify(next))throw new Error('COLLECTION_VERSION_CONFLICT');await this.storage.setItem(this.key,raw);await this.fence();});
 }
 // Explicitly abandoning a confirmed conflict never changes the collection.
 // An unchanged or already-applied intent must instead be recovered.
 discardConflicted(api:Pick<CollectionVersionTransport,'reconcileCollectionVersion'>,signal?:AbortSignal){return this.transaction(async()=>{
  await this.fence();signal?.throwIfAborted();const raw=await this.storage.getItem(this.key);this.current();if(raw===null)return;
  const selection=this.decode(raw);
  if(await api.reconcileCollectionVersion(structuredClone(selection),signal)!=='conflict')throw new Error('COLLECTION_VERSION_NOT_CONFLICTED');
  await this.fence();signal?.throwIfAborted();await this.storage.removeItem(this.key);this.current();
 });}
 resume(api:CollectionVersionTransport,signal?:AbortSignal){return this.transaction(async()=>{
  await this.fence();signal?.throwIfAborted();const raw=await this.storage.getItem(this.key);this.current();if(raw===null)throw new Error('COLLECTION_VERSION_MISSING');
  const selection=this.decode(raw);
  await resumeCollectionVersion(api,selection,signal);await this.fence();signal?.throwIfAborted();
  // A matching authoritative collection read, not a local mutation response,
  // is required before deleting the recovery intent.
  if(await api.reconcileCollectionVersion(structuredClone(selection),signal)!=='applied')throw new Error('COLLECTION_VERSION_CONFLICT');
  await this.fence();signal?.throwIfAborted();await this.storage.removeItem(this.key);this.current();return selection;
 });}
}
export function clearCollectionVersionAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){
  if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length))}catch{continue}
  if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
 }}));
}
