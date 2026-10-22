import {canonicalJSON} from '@photoeditor/api-client';
import type {EditorRecipeBridgeAttempt} from './editor-recipe-bridge';
import type {DraftStorage} from './capture-draft-store';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';

const prefix='photo.editor-recipe-bridge-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.editor-recipe-bridge-generation.v1:'+JSON.stringify(origin);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
function fields(v:unknown,keys:string[]){return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.sort().join(',');}
export class EditorRecipeBridgeStore {
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private version:string,private lock:SemanticJournalLock=semanticJournalLock){if(!origin||![user,tenant,version].every(uuid))throw new Error('EDITOR_BRIDGE_JOURNAL_SCOPE_INVALID');this.storageKey=prefix+JSON.stringify([origin,user,tenant,version]);this.epoch=epochs.get(origin)??0;}
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('EDITOR_BRIDGE_JOURNAL_EXPIRED');}
 private async fence(){this.current();const generation=await this.storage.getItem(generationKey(this.origin));this.current();if(generation!==null&&!uuid(generation))throw new Error('EDITOR_BRIDGE_JOURNAL_INVALID');if(this.generation===undefined)this.generation=generation;else if(this.generation!==generation)throw new Error('EDITOR_BRIDGE_JOURNAL_EXPIRED');}
 private decode(raw:string):EditorRecipeBridgeAttempt{
  if(raw.length>3145728)throw new Error('EDITOR_BRIDGE_JOURNAL_INVALID');
  const value=JSON.parse(raw) as EditorRecipeBridgeAttempt;
  if(!fields(value,['baseline','request','key','editor_bridge','freeze_key'])||!uuid(value.key)||!uuid(value.freeze_key)||value.baseline.binding.asset_version_id!==this.version||value.request.recipe.source.asset_version_id!==this.version||canonicalJSON(value.baseline.binding)!==canonicalJSON(value.request.recipe.source)||value.editor_bridge.document_id!==value.baseline.draft.data.current_document_id||value.request.expected_revision!==value.baseline.draft.revision||!/^[1-9][0-9]*$/.test(value.editor_bridge.revision)||!value.editor_bridge.params||typeof value.editor_bridge.params!=='object')throw new Error('EDITOR_BRIDGE_JOURNAL_INVALID');
  return structuredClone(value);
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action));}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw);});}
 write(value:EditorRecipeBridgeAttempt){const next=this.decode(JSON.stringify(value)),raw=JSON.stringify(next);return this.transaction(async()=>{await this.fence();const previous=await this.storage.getItem(this.storageKey);this.current();if(previous!==null&&canonicalJSON(this.decode(previous))!==canonicalJSON(next))throw new Error('EDITOR_BRIDGE_JOURNAL_CONFLICT');await this.storage.setItem(this.storageKey,raw);this.current();});}
 clear(key:string){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){if(this.decode(raw).key!==key)throw new Error('EDITOR_BRIDGE_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current();}});}

}
export function clearEditorRecipeBridges(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);}}));}
