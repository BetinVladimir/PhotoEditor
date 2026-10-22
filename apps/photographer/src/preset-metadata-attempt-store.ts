import {readExportPresetResponse,readPresetMetadataReceipt} from "./export-preset-response";
import type {DraftStorage} from './capture-draft-store';
import type {ExportPreset} from './catalog-types';
import {semanticJournalLock,type SemanticJournalLock} from './semantic-attempt-store';
export type PresetMetadataAttempt={version:1;key:string;preset:string;revision:string;mode:ExportPreset['config']['metadata_policy'];baseline?:ExportPreset};
const prefix='photo.preset-metadata-attempt.v1:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>'photo.preset-metadata-generation.v1:'+JSON.stringify(origin);
let pending:Promise<unknown>=Promise.resolve();
function queue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next}
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
export class PresetMetadataAttemptStore{
 readonly storageKey:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private preset:string,private lock:SemanticJournalLock=semanticJournalLock){
  if(!origin||![user,tenant,preset].every(uuid))throw new Error('PRESET_METADATA_JOURNAL_SCOPE_INVALID');
  this.storageKey=prefix+JSON.stringify([origin,user,tenant,preset]);this.epoch=epochs.get(origin)??0;
 }
 private current(){if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('PRESET_METADATA_JOURNAL_EXPIRED')}
 private async fence(){this.current();const value=await this.storage.getItem(generationKey(this.origin));this.current();if(value!==null&&!uuid(value))throw new Error('PRESET_METADATA_JOURNAL_INVALID');if(this.generation===undefined)this.generation=value;else if(this.generation!==value)throw new Error('PRESET_METADATA_JOURNAL_EXPIRED')}
 private decode(raw:string):PresetMetadataAttempt{
  if(raw.length>16384)throw new Error('PRESET_METADATA_JOURNAL_INVALID');let value:PresetMetadataAttempt;try{value=JSON.parse(raw) as PresetMetadataAttempt}catch{throw new Error('PRESET_METADATA_JOURNAL_INVALID')}
  if(!value||typeof value!=='object'||Array.isArray(value)||!['key,mode,preset,revision,version','baseline,key,mode,preset,revision,version'].includes(Object.keys(value).sort().join(','))||value.version!==1||!uuid(value.key)||value.preset!==this.preset||typeof value.revision!=='string'||!/^[1-9][0-9]{0,15}$/.test(value.revision)||BigInt(value.revision)>=9007199254740991n||!['strip','copyright','approved_exif'].includes(value.mode))throw new Error('PRESET_METADATA_JOURNAL_INVALID');
  const baseline=value.baseline===undefined?undefined:readExportPresetResponse(value.baseline);if(baseline&&(baseline.id!==value.preset||baseline.revision!==value.revision))throw new Error('PRESET_METADATA_JOURNAL_INVALID');
  return {version:1,key:value.key,preset:value.preset,revision:value.revision,mode:value.mode,...(baseline?{baseline}: {})};
 }
 private transaction<T>(action:()=>Promise<T>){return queue(()=>this.lock(this.origin,action))}
 read(){return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();return raw===null?null:this.decode(raw)})}
 write(value:PresetMetadataAttempt){const next=this.decode(JSON.stringify(value));return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null&&JSON.stringify(this.decode(raw))!==JSON.stringify(next))throw new Error('PRESET_METADATA_JOURNAL_CONFLICT');await this.storage.setItem(this.storageKey,JSON.stringify(next));this.current()})}
 /** Called only after the exact immutable PATCH returned a definitive revision conflict. */
 discardRejected(value:PresetMetadataAttempt){const frozen=this.decode(JSON.stringify(value));return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){if(JSON.stringify(this.decode(raw))!==JSON.stringify(frozen))throw new Error('PRESET_METADATA_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current()}})}
 clearAccepted(value:PresetMetadataAttempt,preset:ExportPreset){const frozen=this.decode(JSON.stringify(value));preset=readPresetMetadataReceipt(preset,{id:frozen.preset,revision:frozen.revision,mode:frozen.mode},frozen.baseline);if(preset.id!==frozen.preset||preset.revision!==(BigInt(frozen.revision)+1n).toString()||preset.config.metadata_policy!==frozen.mode)throw new Error('PRESET_METADATA_RECEIPT_INVALID');return this.transaction(async()=>{await this.fence();const raw=await this.storage.getItem(this.storageKey);this.current();if(raw!==null){if(JSON.stringify(this.decode(raw))!==JSON.stringify(frozen))throw new Error('PRESET_METADATA_JOURNAL_CONFLICT');await this.storage.removeItem(this.storageKey);this.current()}})}
}
export function clearPresetMetadataAttempts(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string,lock:SemanticJournalLock=semanticJournalLock){epochs.set(origin,(epochs.get(origin)??0)+1);return queue(()=>lock(origin,async()=>{await storage.setItem(generationKey(origin),crypto.randomUUID());for(const key of await storage.getAllKeys()){if(!key.startsWith(prefix))continue;let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length))}catch{continue}if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key)}}))}
