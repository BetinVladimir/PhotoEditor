import {canonicalJSON} from '@photoeditor/api-client';
import type {DraftStorage} from './capture-draft-store';
import {freezeArchiveUpload,readArchiveUploadReceipt,archiveCompletionParts,type ArchiveUploadRequest} from './interchange-archive-upload';
import type {ArchiveCheckpoint} from './interchange-archive-executor';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
export type InterchangeArchiveAttempt={version:1;key:string;body:ArchiveUploadRequest;checkpoint:ArchiveCheckpoint|null};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.interchange-archive-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.interchange-archive-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class InterchangeArchiveAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 private requestScope:string;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,request:ArchiveUploadRequest,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project].every(uuid))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_SCOPE_INVALID');
  this.requestScope=canonicalJSON(freezeArchiveUpload(request,'scope-validation'));
  this.key=prefix+JSON.stringify([origin,user,tenant,project,bytesToHex(sha256(new TextEncoder().encode(this.requestScope)))]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,project:string,request:ArchiveUploadRequest){
  this.current();
  if(tenant!==this.tenant||project!==this.project||canonicalJSON(freezeArchiveUpload(request,'scope-validation'))!==this.requestScope)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_SCOPE_INVALID');
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>2097152||new TextEncoder().encode(raw).byteLength>2097152)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as InterchangeArchiveAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,checkpoint,key,version'||parsed.version!==1)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_INVALID');
  const body=freezeArchiveUpload(parsed.body,parsed.key);
  if(canonicalJSON(body)!==this.requestScope)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_SCOPE_INVALID');
  let checkpoint:ArchiveCheckpoint|null=null;
  if(parsed.checkpoint!==null){
   const c=parsed.checkpoint;
   if(!c||Object.keys(c).sort().join(',')!=='parts,receipt,stage'||!['uploading','queued','aborting'].includes(c.stage)||!Array.isArray(c.parts))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_INVALID');
   const receipt=readArchiveUploadReceipt(c.receipt,this.project,body),seen=new Set<number>();
   const parts=c.parts.map(p=>{
    if(!p||Object.keys(p).sort().join(',')!=='etag,part_no'||!Number.isSafeInteger(p.part_no)||p.part_no<1||p.part_no>receipt.parts_total||seen.has(p.part_no)||typeof p.etag!=='string'||!p.etag||p.etag.length>256||/[\r\n\x00]/.test(p.etag))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_INVALID');
    seen.add(p.part_no);return {part_no:p.part_no,etag:p.etag};
   }).sort((a,b)=>a.part_no-b.part_no);
   if(c.stage==='queued')archiveCompletionParts(receipt,parts);
   checkpoint={receipt,parts,stage:c.stage};
  }
  const record:InterchangeArchiveAttempt={version:1,key:parsed.key,body,checkpoint};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:InterchangeArchiveAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)||old.checkpoint!==null&&(next.checkpoint===null||JSON.stringify(old.checkpoint.receipt)!==JSON.stringify(next.checkpoint.receipt)||old.checkpoint.stage==='queued'&&next.checkpoint.stage==='uploading'||old.checkpoint.stage==='aborting'&&next.checkpoint.stage!=='aborting'||old.checkpoint.parts.some(p=>!next.checkpoint!.parts.some(n=>n.part_no===p.part_no&&n.etag===p.etag))))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Explicit discard requires a durable queued completion acknowledgement. */
 clear(acceptedKey:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.checkpoint===null||record.checkpoint.stage!=='queued')throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');}
  await this.storage.removeItem(this.key);this.current();
 });}
 /** Caller must freshly confirm server aborted under the execution lock. */
 clearAborted(acceptedKey:string,upload:string,expected:InterchangeArchiveAttempt){
  const snapshot=canonicalJSON(this.decode(JSON.stringify(expected)));
  return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw===null)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  const record=this.decode(raw);
  if(canonicalJSON(record)!==snapshot||record.key!==acceptedKey||record.checkpoint?.stage!=='aborting'||record.checkpoint.receipt.upload_id!==upload)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
  await this.storage.removeItem(this.key);this.current();
 });}

}
export function clearInterchangeArchiveAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
