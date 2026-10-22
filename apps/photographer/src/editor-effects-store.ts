import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
import {readEditorEffectsAttempt,sameEditorEffectsCommand,type EditorEffectsAttempt} from './editor-effects-attempt';
const prefix='photo.editor-effects.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.editor-effects-generation.v1:'+JSON.stringify(origin);
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}

export class EditorEffectsStore{
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private version:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,version].every(uuid))throw new Error('EDITOR_EFFECT_JOURNAL_SCOPE_INVALID');
  this.storageKey=prefix+JSON.stringify([origin,user,tenant,version]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('EDITOR_EFFECT_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('EDITOR_EFFECT_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('EDITOR_EFFECT_JOURNAL_EXPIRED');}
 private decode(raw:string){if(raw.length>1048576)throw new Error('EDITOR_EFFECT_JOURNAL_INVALID');return readEditorEffectsAttempt(JSON.parse(raw),this.version);}
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 write(value:EditorEffectsAttempt){const next=this.decode(JSON.stringify(value));return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();
  if(raw!==null){const previous=this.decode(raw);if(!sameEditorEffectsCommand(previous,next)||(previous.accepted!==null&&previous.accepted.run_id!==next.accepted?.run_id))throw new Error('EDITOR_EFFECT_JOURNAL_CONFLICT');}
  await this.storage.setItem(this.storageKey,JSON.stringify(next));this.current();
 });}
 // Only a previously accepted command can be acknowledged. The caller must
 // first read final status; uncertain mutations remain durable until recovery.
 clearAccepted(key:string,build:string){return this.transaction(async()=>{
  await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw===null)return;
  const previous=this.decode(raw);if(previous.key!==key||previous.accepted?.run_id!==build)throw new Error('EDITOR_EFFECT_JOURNAL_CONFLICT');
  await this.storage.removeItem(this.storageKey);this.current();
 });}
}
export function clearEditorEffectsAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){
 epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}
 }));
}
