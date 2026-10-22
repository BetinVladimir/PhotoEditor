import {freezeLinkPolicy,type LinkPolicy} from './collection-access-links-api';
import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

export type AccessLinkAttempt={schema_version:1;key:string;request:LinkPolicy;ticket:{resource_id:string}|null};
const prefix='photo.access-link-create-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.access-link-create-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}
function freezeRequest(value:LinkPolicy):LinkPolicy{return freezeLinkPolicy(value);}
export class AccessLinkAttemptStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,collection:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![user,tenant,collection].every(uuid))throw new Error('ACCESS_LINK_CREATE_JOURNAL_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,user,tenant,collection]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('ACCESS_LINK_CREATE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('ACCESS_LINK_CREATE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('ACCESS_LINK_CREATE_JOURNAL_EXPIRED');}
 private decode(raw:string):AccessLinkAttempt{if(raw.length>20000)throw new Error('ACCESS_LINK_CREATE_JOURNAL_INVALID');const value=JSON.parse(raw) as AccessLinkAttempt;if(!fields(value,['schema_version','key','request','ticket'])||value.schema_version!==1||!uuid(value.key)||value.ticket!==null&&(!fields(value.ticket,['resource_id'])||!uuid(value.ticket.resource_id)))throw new Error('ACCESS_LINK_CREATE_JOURNAL_INVALID');return {schema_version:1,key:value.key,request:freezeRequest(value.request),ticket:value.ticket===null?null:{resource_id:value.ticket.resource_id}};}
 private closedKey(key:string){return prefix+JSON.stringify([this.origin,this.storageKey,'closed',key]);}
 private async isClosed(key:string){const value=await this.storage.getItem(this.closedKey(key));this.current();return value!==null;}
 private async markClosed(key:string){if(!uuid(key))throw new Error('ACCESS_LINK_CREATE_JOURNAL_INVALID');await this.storage.setItem(this.closedKey(key),'closed');this.current();}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw===null)return null;const value=this.decode(raw);return await this.isClosed(value.key)?null:value;});}
 write(value:AccessLinkAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{await this.fence();if(await this.isClosed(next.key))throw new Error('ACCESS_LINK_CREATE_JOURNAL_CLOSED');const previous=await this.storage.getItem(this.storageKey);this.current();if(previous!==null){const old=this.decode(previous);if(!await this.isClosed(old.key)&&(old.key!==next.key||JSON.stringify(old.request)!==JSON.stringify(next.request)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket)))throw new Error('ACCESS_LINK_CREATE_JOURNAL_CONFLICT');}await this.storage.setItem(this.storageKey,raw);this.current();});}
 clearAccepted(key:string,tenantID:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();
  if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket?.resource_id!==tenantID)throw new Error('ACCESS_LINK_CREATE_JOURNAL_CONFLICT');await this.markClosed(key);await this.storage.removeItem(this.storageKey);this.current();}else{await this.markClosed(key);}
 });}
 clearAbandoned(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket!==null)throw new Error('ACCESS_LINK_CREATE_JOURNAL_CONFLICT');await this.markClosed(key);await this.storage.removeItem(this.storageKey);this.current();}else{await this.markClosed(key);}});}
 discardRejected(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket!==null)throw new Error('ACCESS_LINK_CREATE_JOURNAL_CONFLICT');await this.markClosed(key);await this.storage.removeItem(this.storageKey);this.current();}else{await this.markClosed(key);}});}
}
export function clearAccessLinkAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
