import {readLayerStack} from './editor-layers';
import type {EditorDraft} from './catalog-types';
import type {EditorSnapshot} from './editor-store';
import type {DraftStorage} from './capture-draft-store';
import {parseEditorParams} from './editor-params';
export interface CachedEditorDraft {schema_version:1;base:EditorDraft;local:EditorSnapshot;saved_at:number;}
const prefix='photo.editor-draft.v1:';
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
function enqueue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next}
export function parseCachedEditorDraft(raw:string|null,version:string,now=Date.now()):CachedEditorDraft|null{
 if(!raw||raw.length>1000000)return null;
 try{
  const value=JSON.parse(raw) as CachedEditorDraft,b=value.base,l=value.local;
  if(value.schema_version!==1||!b||!l||b.asset_version_id!==version||!uuid.test(version)||typeof b.revision!=="string"||typeof b.source_sha256!=="string"||!/^([1-9][0-9]*|0)$/.test(b.revision)||BigInt(b.revision)>9007199254740991n||!/^[0-9a-f]{64}$/.test(b.source_sha256)||!Number.isSafeInteger(value.saved_at)||value.saved_at<0||value.saved_at>now||now-value.saved_at>7*86400000)return null;
  parseEditorParams(b.params);parseEditorParams(l.params);
  if(!Array.isArray(b.mask_refs)||!Array.isArray(l.maskRefs)||b.mask_refs.length>512||l.maskRefs.length>512||l.selectionObjectId!==undefined&&(typeof l.selectionObjectId!=='string'||l.selectionObjectId.length>200))return null;
  const layerStack=l.layerStack==null?l.layerStack:readLayerStack(l.layerStack,l.layerStack.source);
  if(layerStack&&(layerStack.source.asset_version_id!==b.asset_version_id||layerStack.source.source.sha256!==b.source_sha256))return null;
  return {schema_version:1,base:{asset_version_id:b.asset_version_id,source_sha256:b.source_sha256,revision:b.revision,params:b.params,mask_refs:b.mask_refs},local:{params:l.params,maskRefs:l.maskRefs,selectionObjectId:l.selectionObjectId,...(layerStack===undefined?{}:{layerStack})},saved_at:value.saved_at};
 }catch{return null}
}
export function clearEditorDraftCaches(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return enqueue(async()=>{
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length))}catch{continue}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 });
}
export class EditorDraftCache {
 readonly key:string;private epoch:number;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private version:string){
  if(!origin||![user,tenant,version].every(id=>uuid.test(id)))throw new Error('EDITOR_CACHE_SCOPE_INVALID');
  this.epoch=epochs.get(origin)??0;this.key=prefix+JSON.stringify([origin,user,tenant,version]);
 }
 read(){return enqueue(async()=>{
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  const value=parseCachedEditorDraft(await this.storage.getItem(this.key),this.version);
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  if(!value)await this.storage.removeItem(this.key);
  return value;
 })}
 write(value:CachedEditorDraft){
  const raw=JSON.stringify(value),parsed=parseCachedEditorDraft(raw,this.version);
  if(!parsed)return Promise.reject(new Error('EDITOR_CACHE_INVALID'));
  return enqueue(async()=>{if(this.epoch===(epochs.get(this.origin)??0))await this.storage.setItem(this.key,JSON.stringify(parsed))});
 }
 clear(){return enqueue(()=>this.storage.removeItem(this.key))}
}

export function editorCacheRestoreMode(cached:CachedEditorDraft,server:EditorDraft):'restore'|'conflict'|'source_changed'{
 if(cached.base.asset_version_id!==server.asset_version_id||cached.base.source_sha256!==server.source_sha256)return 'source_changed';
 if(cached.base.revision!==server.revision)return 'conflict';
 return 'restore';
}
