import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

export type WorkspaceCreateAttempt={schema_version:1;key:string;request:WorkspaceCreateRequest;ticket:{tenant_id:string}|null};
const prefix='photo.workspace-create-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.workspace-create-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}
export type WorkspaceCreateRequest={name:string};
function freezeRequest(value:WorkspaceCreateRequest):WorkspaceCreateRequest{
 if(!fields(value,['name'])||typeof value.name!=='string'||!value.name.trim()||value.name!==value.name.trim()||Array.from(value.name).length>120)throw new Error('WORKSPACE_CREATE_JOURNAL_INVALID');
 return {name:value.name};
}
export class WorkspaceCreateAttemptStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||!uuid(user))throw new Error('WORKSPACE_CREATE_JOURNAL_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,user]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('WORKSPACE_CREATE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('WORKSPACE_CREATE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('WORKSPACE_CREATE_JOURNAL_EXPIRED');}
 private decode(raw:string):WorkspaceCreateAttempt{if(raw.length>2048)throw new Error('WORKSPACE_CREATE_JOURNAL_INVALID');const value=JSON.parse(raw) as WorkspaceCreateAttempt;if(!fields(value,['schema_version','key','request','ticket'])||value.schema_version!==1||!uuid(value.key)||value.ticket!==null&&(!fields(value.ticket,['tenant_id'])||!uuid(value.ticket.tenant_id)))throw new Error('WORKSPACE_CREATE_JOURNAL_INVALID');return {schema_version:1,key:value.key,request:freezeRequest(value.request),ticket:value.ticket===null?null:{tenant_id:value.ticket.tenant_id}};}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 write(value:WorkspaceCreateAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{await this.fence();const previous=await this.storage.getItem(this.storageKey);this.current();if(previous!==null){const old=this.decode(previous);if(old.key!==next.key||JSON.stringify(old.request)!==JSON.stringify(next.request)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(next.ticket))throw new Error('WORKSPACE_CREATE_JOURNAL_CONFLICT');}await this.storage.setItem(this.storageKey,raw);this.current();});}
 clearAccepted(key:string,tenantID:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();
  if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket?.tenant_id!==tenantID)throw new Error('WORKSPACE_CREATE_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}
 });}
 discardRejected(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=this.decode(raw);if(old.key!==key||old.ticket!==null)throw new Error('WORKSPACE_CREATE_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}
}
export function clearWorkspaceCreateAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
