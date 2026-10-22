import {readImportPersonMaskStatuses,type ImportPersonMaskStatus} from './interchange-person-mask-status';
import type {DraftStorage} from './capture-draft-store';
import {freezeImportPersonMaskDecision,readImportPersonMaskDecision,type ImportPersonMaskDecision} from './interchange-person-mask-decision';
type ImportMaskReceipt=ReturnType<typeof readImportPersonMaskDecision>;
export type ImportMaskAttempt={version:1;key:string;body:ImportPersonMaskDecision};
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const scopePrefix='photo.import-mask-scope-generation.v1:';
const prefix='photo.import-mask-attempt.v1:',epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.import-mask-generation.v1:'+JSON.stringify(origin);
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class ImportMaskAttemptStore {
 readonly key:string;private scopeKey:string;private scopeGeneration:string|null|undefined;private acknowledgedKey:string|null=null;private operationEpoch=0;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,private tenant:string,private project:string,private workflow:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,project,workflow].every(uuid))throw new Error('IMPORT_MASK_JOURNAL_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,workflow]);this.scopeKey=scopePrefix+JSON.stringify([origin,user,tenant,project,workflow]);this.epoch=epochs.get(origin)??0;
 }
 assertScope(tenant:string,project:string,workflow:string){
  this.current();
  if(tenant!==this.tenant||project!==this.project||workflow!==this.workflow)throw new Error('IMPORT_MASK_JOURNAL_SCOPE_INVALID');
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('IMPORT_MASK_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('IMPORT_MASK_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('IMPORT_MASK_JOURNAL_EXPIRED');}
 private async scopeFence(){
  const raw=await this.storage.getItem(this.scopeKey);this.current();
  let marker:{generation:string;acknowledged_key:string}|null=null;
  if(raw!==null){
   if(raw.length>256)throw new Error('IMPORT_MASK_JOURNAL_INVALID');
   marker=JSON.parse(raw);
   if(!marker||Object.keys(marker).sort().join(',')!=='acknowledged_key,generation'||!uuid(marker.generation)||!uuid(marker.acknowledged_key))throw new Error('IMPORT_MASK_JOURNAL_INVALID');
  }
  const generation=marker?.generation??null;
  if(this.scopeGeneration===undefined)this.scopeGeneration=generation;
  else if(this.scopeGeneration!==generation)throw new Error('IMPORT_MASK_JOURNAL_EXPIRED');
  this.acknowledgedKey=marker?.acknowledged_key??null;
 }
 private decode(raw:string){
  if(raw.length>(20<<20)||new TextEncoder().encode(raw).byteLength>(20<<20))throw new Error('IMPORT_MASK_JOURNAL_INVALID');
  const parsed=JSON.parse(raw) as ImportMaskAttempt;
  if(!parsed||Object.keys(parsed).sort().join(',')!=='body,key,version'||parsed.version!==1)throw new Error('IMPORT_MASK_JOURNAL_INVALID');
  if(!uuid(parsed.key))throw new Error('IMPORT_MASK_JOURNAL_INVALID');
  const body=freezeImportPersonMaskDecision(parsed.body);
  const record:ImportMaskAttempt={version:1,key:parsed.key,body};
  return record;
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();await this.scopeFence();const raw=await this.storage.getItem(this.key);this.current();if(raw===null)return null;const record=this.decode(raw);return record.key===this.acknowledgedKey?null:record;});}
 write(record:ImportMaskAttempt){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next),operationEpoch=this.operationEpoch;
  return this.transaction(async()=>{
   await this.fence();await this.scopeFence();if(operationEpoch!==this.operationEpoch||next.key===this.acknowledgedKey)throw new Error('IMPORT_MASK_JOURNAL_EXPIRED');const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){
    const old=this.decode(previous);
    if(old.key!==this.acknowledgedKey&&(old.key!==next.key||JSON.stringify(old.body)!==JSON.stringify(next.body)))throw new Error('IMPORT_MASK_JOURNAL_CONFLICT');
   }
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 /** Only an acknowledged attempt can be discarded to author another operation. */
 clear(acceptedKey:string,receipt:ImportMaskReceipt){
  const acknowledged=structuredClone(receipt);
  return this.transaction(async()=>{
   await this.fence();await this.scopeFence();const raw=await this.storage.getItem(this.key);this.current();
   if(raw!==null){
    const record=this.decode(raw);
    if(record.key!==acceptedKey)throw new Error('IMPORT_MASK_JOURNAL_CONFLICT');
    try{
     readImportPersonMaskDecision(acknowledged,this.workflow,record.body);
    }catch{throw new Error('IMPORT_MASK_JOURNAL_CONFLICT');}
   }
   if(raw!==null&&acceptedKey!==this.acknowledgedKey){
    const generation=crypto.randomUUID();
    await this.storage.setItem(this.scopeKey,JSON.stringify({generation,acknowledged_key:acceptedKey}));this.current();
    this.scopeGeneration=generation;this.acknowledgedKey=acceptedKey;this.operationEpoch++;
   }
   await this.storage.removeItem(this.key);this.current();
  });
 }

 /** Retire only a decision whose CAS epoch has been consumed (a replay cannot mutate it again).
  * Caller must obtain statuses from the authenticated workflow status endpoint. */
 clearSuperseded(acceptedKey:string,statuses:ImportPersonMaskStatus[]){
  const owned=readImportPersonMaskStatuses({workflow_id:this.workflow,items:statuses},this.workflow);
  return this.transaction(async()=>{
   await this.fence();await this.scopeFence();const raw=await this.storage.getItem(this.key);this.current();
   if(raw===null)throw new Error('IMPORT_MASK_JOURNAL_CONFLICT');
   const record=this.decode(raw),body=record.body;
   const status=owned.find(item=>item.entry_id===body.entry_id&&item.override_artifact_id===body.override_artifact_id&&item.mask_artifact_id===body.mask_artifact_id&&item.target_version_id===body.target_version_id);
   const desired=body.action==='grant'?'granted':'revoked';
   if(record.key!==acceptedKey||!status||!(status.epoch>=body.expected_epoch!+1||status.epoch===body.expected_epoch&&status.status===desired))throw new Error('IMPORT_MASK_JOURNAL_CONFLICT');
   const generation=crypto.randomUUID();
   await this.storage.setItem(this.scopeKey,JSON.stringify({generation,acknowledged_key:acceptedKey}));this.current();
   this.scopeGeneration=generation;this.acknowledgedKey=acceptedKey;this.operationEpoch++;
   await this.storage.removeItem(this.key);this.current();
  });
 }

}
export function clearImportMaskAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){
   const matchedPrefix=key.startsWith(prefix)?prefix:key.startsWith(scopePrefix)?scopePrefix:null;if(!matchedPrefix)continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(matchedPrefix.length));}catch{continue;}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 }));
}
