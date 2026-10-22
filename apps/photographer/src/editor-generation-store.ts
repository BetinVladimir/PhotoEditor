import type {DraftStorage} from './capture-draft-store';
import {readEditorGenerationAttempt,type EditorGenerationAttemptRecord} from './editor-generation-attempt';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
const prefix='photo.editor-generation-attempt.v1:',scopePrefix='photo.editor-generation-scope.v1:',epochs=new Map<string,number>();
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const generationKey=(origin:string)=>'photo.editor-generation-epoch.v1:'+JSON.stringify(origin);
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const result=pending.catch(()=>{}).then(action);pending=result;return result;}
export class EditorGenerationStore{
 readonly key:string;private scopeKey:string;private epoch:number;private generation:string|null|undefined;private scopeGeneration:string|null|undefined;private acknowledged:string|null=null;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,asset:string,private version:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,asset,version].every(uuid))throw new Error('GENERATION_JOURNAL_SCOPE_INVALID');
  const scope=JSON.stringify([origin,user,tenant,asset,version]);this.key=prefix+scope;this.scopeKey=scopePrefix+scope;this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('GENERATION_JOURNAL_EXPIRED');}
 private async fence(){
  this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();
  if(generation!==null&&!uuid(generation))throw new Error('GENERATION_JOURNAL_INVALID');
  if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('GENERATION_JOURNAL_EXPIRED');
  const raw=await this.storage.getItem(this.scopeKey);this.current();let marker:{generation:string;key:string}|null=null;
  if(raw!==null){if(raw.length>256)throw new Error('GENERATION_JOURNAL_INVALID');marker=JSON.parse(raw);if(!marker||Object.keys(marker).sort().join(',')!=='generation,key'||!uuid(marker.generation)||!uuid(marker.key))throw new Error('GENERATION_JOURNAL_INVALID');}
  const scopeGeneration=marker?.generation??null;
  if(this.scopeGeneration===undefined)this.scopeGeneration=scopeGeneration;else if(this.scopeGeneration!==scopeGeneration)throw new Error('GENERATION_JOURNAL_EXPIRED');
  this.acknowledged=marker?.key??null;
 }
 private decode(raw:string){const record=readEditorGenerationAttempt(raw);if(record.input.draft.asset_version_id!==this.version)throw new Error('GENERATION_JOURNAL_SCOPE_INVALID');return record;}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.key);this.current();if(raw===null)return null;const record=this.decode(raw);return record.key===this.acknowledged?null:record;});}
 write(record:EditorGenerationAttemptRecord){
  const next=this.decode(JSON.stringify(record)),raw=JSON.stringify(next);
  return this.transaction(async()=>{
   await this.fence();if(next.key===this.acknowledged)throw new Error('GENERATION_JOURNAL_EXPIRED');
   const previous=await this.storage.getItem(this.key);this.current();
   if(previous!==null){const old=this.decode(previous);if(old.key!==this.acknowledged&&(old.key!==next.key||JSON.stringify(old.input)!==JSON.stringify(next.input)||old.receipt!==null&&JSON.stringify(old.receipt)!==JSON.stringify(next.receipt)))throw new Error('GENERATION_JOURNAL_CONFLICT');}
   await this.storage.setItem(this.key,raw);this.current();
  });
 }
 clear(key:string){return this.clearKnown(key,true);}
 clearAbandoned(receipt:{status:'abandoned';key:string}){
  if(receipt.status!=='abandoned'||!uuid(receipt.key))return Promise.reject(new Error('GENERATION_ABANDONMENT_INVALID'));
  return this.clearKnown(receipt.key,false);
 }
 private clearKnown(key:string,accepted:boolean){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.key);this.current();
  if(raw!==null){const record=this.decode(raw);if(record.key!==key||(record.receipt!==null)!==accepted)throw new Error('GENERATION_JOURNAL_CONFLICT');
   if(key!==this.acknowledged){const generation=crypto.randomUUID();await this.storage.setItem(this.scopeKey,JSON.stringify({generation,key}));this.current();this.scopeGeneration=generation;this.acknowledged=key;}
  }
  await this.storage.removeItem(this.key);this.current();
 });}
}
export function clearEditorGenerationAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){
  const matched=key.startsWith(prefix)?prefix:key.startsWith(scopePrefix)?scopePrefix:null;if(!matched)continue;
  let scope:unknown;try{scope=JSON.parse(key.slice(matched.length))}catch{continue}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
 }}));
}
