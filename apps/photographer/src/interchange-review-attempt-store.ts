import type {DraftStorage} from './capture-draft-store';
import {canonicalJSON} from '@photoeditor/api-client';
import {readInterchangeInspection,interchangeDecisionRequest,interchangeCloseRequest,readInterchangeDecisionResult,readInterchangeCloseResult,type InterchangeInspection} from './interchange-job';
export type InterchangeReviewAttempt={version:1;kind:'decide'|'close';key:string;inspection:InterchangeInspection;decisions:{entry_id:string;decision:'approve'|'reject'}[];receipt:unknown|null};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.interchange-review-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.interchange-review-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class InterchangeReviewAttemptStore {
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private workflow:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,workflow].every(uuid))throw new Error('INTERCHANGE_REVIEW_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,workflow]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,project:string,workflow:string){
  this.current();
  if(tenant!==this.tenant||project!==this.project||workflow!==this.workflow)throw new Error('INTERCHANGE_REVIEW_JOURNAL_SCOPE_INVALID');
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('INTERCHANGE_REVIEW_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('INTERCHANGE_REVIEW_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('INTERCHANGE_REVIEW_JOURNAL_EXPIRED');}
 private decode(raw:string){
  if(raw.length>8*1024*1024||new TextEncoder().encode(raw).byteLength>8*1024*1024)throw new Error('INTERCHANGE_REVIEW_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as InterchangeReviewAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='decisions,inspection,key,kind,receipt,version'||parsed.version!==1||!['decide','close'].includes(parsed.kind)||!Array.isArray(parsed.decisions))throw new Error('INTERCHANGE_REVIEW_JOURNAL_INVALID');
  const inspection=readInterchangeInspection(parsed.inspection,this.tenant,this.project,this.workflow);
  let decisions:InterchangeReviewAttempt['decisions']=[],receipt:unknown=null;
  if(parsed.kind==='decide'){
   decisions=interchangeDecisionRequest(inspection,parsed.decisions,parsed.key).decisions;
   if(parsed.receipt!==null)receipt=readInterchangeDecisionResult(parsed.receipt,inspection,decisions.length);
  }else{
   if(parsed.decisions.length)throw new Error('INTERCHANGE_REVIEW_JOURNAL_INVALID');
   interchangeCloseRequest(inspection,parsed.key);
   if(parsed.receipt!==null)receipt={...readInterchangeCloseResult(parsed.receipt,inspection),outputs:[],error_code:null};
  }
  return {version:1,kind:parsed.kind,key:parsed.key,inspection,decisions,receipt} as InterchangeReviewAttempt;
 }

 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();return raw===null?null:this.decode(raw);});}
 write(record:InterchangeReviewAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==next.key||old.kind!==next.kind||canonicalJSON(old.inspection)!==canonicalJSON(next.inspection)||canonicalJSON(old.decisions)!==canonicalJSON(next.decisions)||old.receipt!==null&&canonicalJSON(old.receipt)!==canonicalJSON(next.receipt))throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string,expected?:InterchangeReviewAttempt){
  const expectedBody=expected===undefined?undefined:canonicalJSON(this.decode(JSON.stringify(expected)));
  return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==acceptedKey||record.receipt===null||expectedBody!==undefined&&canonicalJSON(record)!==expectedBody)throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');}
  if(raw===null&&expectedBody!==undefined)throw new Error('INTERCHANGE_REVIEW_JOURNAL_CONFLICT');
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearInterchangeReviewAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
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
