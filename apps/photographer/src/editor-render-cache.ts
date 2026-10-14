import {canonicalJSON} from '@photoeditor/api-client';
import {readEditorRenderSubmission,type prepareEditorRenderSubmission} from './editor-render-submit';
import {renderBatchTicket,type RenderBatchTicket} from './render-batch';
import type {DraftStorage} from './capture-draft-store';
export type PendingEditorRender={project:string;command:ReturnType<typeof prepareEditorRenderSubmission>};
const prefix='photo.editor-render.v1:';
const epochs=new Map<string,number>();
let pending:Promise<unknown>=Promise.resolve();
function enqueue<T>(action:()=>Promise<T>){const next=pending.catch(()=>{}).then(action);pending=next;return next;}
const uuid=(value:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)&&!/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(value);
function parse(raw:string|null,now:number):RenderBatchTicket|null{
 if(!raw||raw.length>250000)return null;
 try{
  const value=JSON.parse(raw);
  if(!value||Object.keys(value).sort().join()!=='saved_at,schema_version,ticket'||value.schema_version!==1||!Number.isSafeInteger(value.saved_at)||value.saved_at<0||value.saved_at>now||now-value.saved_at>7*86400000)return null;
  return renderBatchTicket(value.ticket);
 }catch{return null;}
}
function parsePending(raw:string|null,version:string,now:number):PendingEditorRender|null{
 if(!raw||raw.length>20000)return null;
 try{
  const v=JSON.parse(raw);
  if(!v||Object.keys(v).sort().join()!=='command,project,saved_at,schema_version'||v.schema_version!==1||typeof v.project!=='string'||!uuid(v.project)||!Number.isSafeInteger(v.saved_at)||v.saved_at<0||v.saved_at>now||now-v.saved_at>7*86400000)return null;
  return {project:v.project,command:readEditorRenderSubmission(v.command,version)};
 }catch{return null;}
}
/** Only admission identifiers are persisted; never signed URLs or credentials. */
export class EditorRenderCache{
 readonly key:string;readonly pendingKey:string;private epoch:number;
 constructor(private storage:DraftStorage,private origin:string,user:string,tenant:string,private version:string){
  if(!origin||![user,tenant,version].every(uuid))throw new Error('RENDER_CACHE_SCOPE_INVALID');
  this.epoch=epochs.get(origin)??0;this.key=prefix+JSON.stringify([origin,user,tenant,version]);this.pendingKey=prefix+JSON.stringify([origin,user,tenant,version,"pending"]);
 }
 readPending(now=Date.now()){return enqueue(async()=>{
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  const value=parsePending(await this.storage.getItem(this.pendingKey),this.version,now);
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  if(!value)await this.storage.removeItem(this.pendingKey);
  return value;
 });}
 writePending(value:PendingEditorRender,now=Date.now()){
  const raw=JSON.stringify({schema_version:1,saved_at:now,...value}),parsed=parsePending(raw,this.version,now);
  if(!parsed)return Promise.reject(new Error('RENDER_PENDING_INVALID'));
  return enqueue(async()=>{
   if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('RENDER_CACHE_SCOPE_EXPIRED');
   const existing=parsePending(await this.storage.getItem(this.pendingKey),this.version,now);
   if(existing&&canonicalJSON(existing)!==canonicalJSON(parsed))throw new Error('RENDER_PENDING_EXISTS');
   await this.storage.setItem(this.pendingKey,JSON.stringify({schema_version:1,saved_at:now,...parsed}));
  });
 }
 acceptPending(value:PendingEditorRender,ticket:RenderBatchTicket,now=Date.now()){
  const owned=renderBatchTicket(ticket),raw=JSON.stringify({schema_version:1,saved_at:now,ticket:owned});
  if(!parse(raw,now))return Promise.reject(new Error('RENDER_CACHE_INVALID'));
  const expected=canonicalJSON(value);
  return enqueue(async()=>{
   if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('RENDER_CACHE_SCOPE_EXPIRED');
   const existing=parsePending(await this.storage.getItem(this.pendingKey),this.version,now);
   if(!existing||canonicalJSON(existing)!==expected)throw new Error('RENDER_PENDING_CHANGED');
   // Not an atomic storage transaction: if removal fails, both records remain
   // valid and the original command can be retried with the same server keys.
   await this.storage.setItem(this.key,raw);
   await this.storage.removeItem(this.pendingKey);
  });
 }
 clearPending(value:PendingEditorRender){return enqueue(async()=>{
  if(this.epoch!==(epochs.get(this.origin)??0))return false;
  const existing=parsePending(await this.storage.getItem(this.pendingKey),this.version,Date.now());
  if(!existing||canonicalJSON(existing)!==canonicalJSON(value))return false;
  await this.storage.removeItem(this.pendingKey);return true;
 });}
 read(now=Date.now()){return enqueue(async()=>{
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  const value=parse(await this.storage.getItem(this.key),now);
  if(this.epoch!==(epochs.get(this.origin)??0))return null;
  if(!value)await this.storage.removeItem(this.key);
  return value;
 });}
 write(ticket:RenderBatchTicket,now=Date.now()){
  const raw=JSON.stringify({schema_version:1,saved_at:now,ticket:renderBatchTicket(ticket)});
  if(!parse(raw,now))return Promise.reject(new Error('RENDER_CACHE_INVALID'));
  return enqueue(async()=>{if(this.epoch===(epochs.get(this.origin)??0))await this.storage.setItem(this.key,raw);});
 }
}
export function clearEditorRenderCaches(storage:DraftStorage&{getAllKeys():Promise<readonly string[]>},origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return enqueue(async()=>{
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length));}catch{continue;}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
 });
}
