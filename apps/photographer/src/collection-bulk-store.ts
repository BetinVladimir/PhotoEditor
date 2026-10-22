import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
import type {DraftStorage} from './capture-draft-store';
import {bulkUUID,freezeBulkCommand,readBulkTicket,type BulkCommand,type BulkTicket} from './collection-bulk-api';
export type BulkAttempt={schema_version:1;key:string;snapshot_key:string;command:BulkCommand;snapshot:{id:string;digest:string;item_count:number;expires_at:string}|null;ticket:BulkTicket|null};
const generationKey=(origin:string)=>'photo.collection-bulk-generation.v1:'+JSON.stringify(origin);
const closedPrefix='photo.collection-bulk-closed.v1:';
const prefix='photo.collection-bulk.v1:',epochs=new Map<string,number>();let tail:Promise<unknown>=Promise.resolve();
function queue<T>(fn:()=>Promise<T>){const next=tail.catch(()=>{}).then(fn);tail=next;return next}
export function decodeBulkAttempt(raw:string):BulkAttempt{
 if(raw.length>1048576)throw new Error('BULK_JOURNAL_INVALID');const v=JSON.parse(raw) as BulkAttempt;
 if(!v||Object.keys(v).sort().join(',')!=='command,key,schema_version,snapshot,snapshot_key,ticket'||v.schema_version!==1||!bulkUUID(v.key)||!bulkUUID(v.snapshot_key))throw new Error('BULK_JOURNAL_INVALID');
 const command=freezeBulkCommand(v.command);
 if(v.snapshot!==null&&(!v.snapshot||Object.keys(v.snapshot).sort().join(',')!=='digest,expires_at,id,item_count'||!bulkUUID(v.snapshot.id)||! /^[0-9a-f]{64}$/.test(v.snapshot.digest)||v.snapshot.item_count!==command.item_ids.length||typeof v.snapshot.expires_at!=='string'||!Number.isFinite(Date.parse(v.snapshot.expires_at))))throw new Error('BULK_JOURNAL_INVALID');
 if(v.ticket!==null){if(!v.snapshot)throw new Error('BULK_JOURNAL_INVALID');readBulkTicket(v.ticket)}
 return JSON.parse(JSON.stringify({...v,command,snapshot:v.snapshot===null?null:{id:v.snapshot.id,digest:v.snapshot.digest,item_count:v.snapshot.item_count,expires_at:v.snapshot.expires_at},ticket:v.ticket===null?null:readBulkTicket(v.ticket)}));
}
export class CollectionBulkStore{
 readonly key:string;private epoch:number;private generation:string|null|undefined;constructor(readonly storage:DraftStorage,readonly origin:string,readonly actor:string,tenant:string,collection:string,private current:()=>boolean=()=>true,private lock:SemanticJournalLock=semanticJournalLock){if(![actor,tenant,collection].every(bulkUUID))throw new Error('BULK_SCOPE_INVALID');this.key=prefix+JSON.stringify([origin,actor,tenant,collection]);this.epoch=epochs.get(origin)??0}
 private closedKey(key:string){return closedPrefix+JSON.stringify([this.origin,this.actor,this.key,key])}
 private active(){if(!this.current()||this.epoch!==(epochs.get(this.origin)??0))throw new Error('BULK_JOURNAL_EXPIRED')}
 private async fence(){this.active();const generation=await this.storage.getItem(generationKey(this.origin));this.active();if(generation!==null&&!bulkUUID(generation))throw new Error('BULK_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('BULK_JOURNAL_EXPIRED')}
 private transaction<T>(fn:()=>Promise<T>){return queue(()=>this.lock(this.origin,fn))}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.active();return raw===null?null:decodeBulkAttempt(raw)})}
 save(value:BulkAttempt){const frozen=decodeBulkAttempt(JSON.stringify(value));return this.transaction(async()=>{await this.fence();if(await this.storage.getItem(this.closedKey(frozen.key))!==null)throw new Error('BULK_ATTEMPT_CLOSED');this.active();const prior=await this.storage.getItem(this.key);this.active();if(prior!==null){const old=decodeBulkAttempt(prior);if(old.key!==frozen.key||old.snapshot_key!==frozen.snapshot_key||JSON.stringify(old.command)!==JSON.stringify(frozen.command)||old.snapshot!==null&&JSON.stringify(old.snapshot)!==JSON.stringify(frozen.snapshot)||old.ticket!==null&&JSON.stringify(old.ticket)!==JSON.stringify(frozen.ticket))throw new Error('BULK_JOURNAL_CONFLICT')};await this.storage.setItem(this.key,JSON.stringify(frozen));this.active();return frozen})}

 clearAbandoned(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.active();if(raw!==null&&decodeBulkAttempt(raw).key!==key)throw new Error('BULK_JOURNAL_CONFLICT');await this.storage.setItem(this.closedKey(key),'1');this.active();await this.storage.removeItem(this.key);this.active()})}
 clearTerminal(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.active();if(raw===null)return;const old=decodeBulkAttempt(raw);if(old.key!==key||!old.ticket)throw new Error('BULK_JOURNAL_CONFLICT');await this.storage.setItem(this.closedKey(key),'1');this.active();await this.storage.removeItem(this.key);this.active()})}
}
export function clearCollectionBulkAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());const keys=await storage.getAllKeys();for(const key of keys){const matched=key.startsWith(prefix)?prefix:key.startsWith(closedPrefix)?closedPrefix:null;if(!matched)continue;try{if(JSON.parse(key.slice(matched.length))[0]===origin)await storage.removeItem(key)}catch{throw new Error('BULK_JOURNAL_CLEAR_FAILED')}}}))}
