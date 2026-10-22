import {freezeLinkPolicy,type LinkPolicy} from './collection-access-links-api';
import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

export type CollectionCreateAttempt={schema_version:1;project_id:string;key:string;request:CollectionCreateRequest;ticket:{collection_id:string}|null};
const prefix='photo.collection-create-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.collection-create-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}
export type CollectionCreateRequest={project_id:string;name:string;description:'';initial_share?:LinkPolicy};
function freezeRequest(value:CollectionCreateRequest):CollectionCreateRequest{
 if(!fields(value,Object.hasOwn(value,'initial_share')?['project_id','name','description','initial_share']:['project_id','name','description'])||!uuid(value.project_id)||typeof value.name!=='string'||!value.name.trim()||value.name!==value.name.trim()||Array.from(value.name).length>120||value.description!=='')throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');
 return {project_id:value.project_id,name:value.name,description:'',...(Object.hasOwn(value,'initial_share')?{initial_share:freezeLinkPolicy(value.initial_share)}:{})};
}
export class CollectionCreateAttemptStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private project:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![user,tenant,project].every(uuid))throw new Error('COLLECTION_CREATE_JOURNAL_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,user,tenant,project]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('COLLECTION_CREATE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('COLLECTION_CREATE_JOURNAL_EXPIRED');}
 private decode(raw:string):CollectionCreateAttempt{if(raw.length>20000)throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');const value=JSON.parse(raw) as CollectionCreateAttempt;if(!fields(value,['schema_version','project_id','key','request','ticket'])||value.schema_version!==1||value.project_id!==this.project||!uuid(value.key)||value.ticket!==null&&(!fields(value.ticket,['collection_id'])||!uuid(value.ticket.collection_id)))throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');if(value.request.project_id!==this.project)throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');return {schema_version:1,project_id:value.project_id,key:value.key,request:freezeRequest(value.request),ticket:value.ticket===null?null:{collection_id:value.ticket.collection_id}};}
 private closedKey(key:string){return prefix+JSON.stringify([this.origin,this.storageKey,'closed',key]);}
 private async isClosed(key:string){const value=await this.storage.getItem(this.closedKey(key));this.current();return value!==null;}
 private async markClosed(key:string){if(!uuid(key))throw new Error('COLLECTION_CREATE_JOURNAL_INVALID');await this.storage.setItem(this.closedKey(key),'closed');this.current();}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw===null)return null;const value=this.decode(raw);return await this.isClosed(value.key)?null:value;});}
 write(value:CollectionCreateAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{await this.fence();if(await this.isClosed(next.key))throw new Error('COLLECTION_CREATE_JOURNAL_CLOSED');const previous=await this.storage.getItem(this.storageKey);this.current();if(previous!==null){const old=this.decode(previous);if(!await this.isClosed(old.key)&&(old.key!==next.key||JSON.stringify(old.request)!==JSON.stringify(next.request)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket)))throw new Error('COLLECTION_CREATE_JOURNAL_CONFLICT');}await this.storage.setItem(this.storageKey,raw);this.current();});}
 clearAccepted(key:string,collectionID:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();
  if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket?.collection_id!==collectionID)throw new Error('COLLECTION_CREATE_JOURNAL_CONFLICT');await this.markClosed(key);await this.storage.removeItem(this.storageKey);this.current();}else{await this.markClosed(key);}
 });}
 clearAbandoned(key:string){return this.discardRejected(key);}
 discardRejected(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket!==null)throw new Error('COLLECTION_CREATE_JOURNAL_CONFLICT');await this.markClosed(key);await this.storage.removeItem(this.storageKey);this.current();}else{await this.markClosed(key);}});}
}
export function clearCollectionCreateAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
