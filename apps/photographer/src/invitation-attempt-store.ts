import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
export type InvitationRequest={email:string;role:'admin'|'editor'|'viewer';locale:'en'|'ru'|'bg'};
export type InvitationReceipt={id:string;email:string;role:InvitationRequest['role'];expires_at:string};
export type InvitationAttempt={version:1;key:string;request:InvitationRequest;receipt:InvitationReceipt|null};
const prefix='photo.invitation-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function normalizedEmail(raw:string){const parts=raw.trim().normalize('NFC').split('@');if(parts.length!==2)throw new Error('INVITATION_EMAIL_INVALID');return parts[0].toLowerCase()+'@'+new URL('http://'+parts[1].toLowerCase()).hostname;}
const generationKey=(origin:string)=>'photo.invitation-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');}
export function readInvitationReceipt(value:unknown,request:InvitationRequest):InvitationReceipt{
 if(!fields(value,['id','email','role','expires_at'])||!uuid(value.id)||typeof value.email!=='string'||value.email!==normalizedEmail(request.email)||value.role!==request.role||typeof value.expires_at!=='string'||!/^\d{4}-\d\d-\d\dT/.test(value.expires_at)||!Number.isFinite(Date.parse(value.expires_at)))throw new Error('INVITATION_RECEIPT_INVALID');
 return {id:value.id,email:value.email,role:value.role as InvitationReceipt['role'],expires_at:value.expires_at};
}
function decode(raw:string):InvitationAttempt{
 if(raw.length>4096)throw new Error('INVITATION_JOURNAL_INVALID');const v:unknown=JSON.parse(raw);
 if(!fields(v,['version','key','request','receipt'])||v.version!==1||!uuid(v.key)||!fields(v.request,['email','role','locale']))throw new Error('INVITATION_JOURNAL_INVALID');
 const r=v.request;if(typeof r.email!=='string'||!r.email.trim()||r.email.length>320||r.email!==r.email.trim()||!['admin','editor','viewer'].includes(String(r.role))||!['en','ru','bg'].includes(String(r.locale)))throw new Error('INVITATION_JOURNAL_INVALID');
 const request={email:r.email,role:r.role,locale:r.locale} as InvitationRequest;
 return {version:1,key:v.key,request,receipt:v.receipt===null?null:readInvitationReceipt(v.receipt,request)};
}
export class InvitationAttemptStore{
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,actor:string,tenant:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||!uuid(actor)||!uuid(tenant))throw new Error('INVITATION_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,actor,tenant]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('INVITATION_JOURNAL_EXPIRED');}
 private async fence(){this.current();const value=await this.storage.getItem(generationKey(this.origin));this.current();if(value!==null&&!uuid(value))throw new Error('INVITATION_JOURNAL_INVALID');if(this.generation===undefined)this.generation=value;else if(this.generation!==value)throw new Error('INVITATION_JOURNAL_EXPIRED');}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:decode(raw);});}
 write(value:InvitationAttempt){const next=decode(JSON.stringify(value));return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){const old=decode(raw);if(old.key!==next.key||JSON.stringify(old.request)!==JSON.stringify(next.request)||old.receipt!==null&&JSON.stringify(old.receipt)!==JSON.stringify(next.receipt))throw new Error('INVITATION_JOURNAL_CONFLICT');}await this.storage.setItem(this.storageKey,JSON.stringify(next));this.current();});}
 discardRejected(value:InvitationAttempt){const next=decode(JSON.stringify(value));if(next.receipt!==null)throw new Error('INVITATION_JOURNAL_CONFLICT');return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){if(JSON.stringify(decode(raw))!==JSON.stringify(next))throw new Error('INVITATION_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}
 clearAccepted(value:InvitationAttempt){const next=decode(JSON.stringify(value));if(!next.receipt)throw new Error('INVITATION_RECEIPT_INVALID');return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){if(JSON.stringify(decode(raw))!==JSON.stringify(next))throw new Error('INVITATION_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}
}
export function clearInvitationAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
