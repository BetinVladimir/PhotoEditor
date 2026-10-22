import {type UploadPersistence,type UploadRecord} from '@photoeditor/api-client';
import {archiveUUID} from './interchange-archive-upload';
import {semanticJournalLock} from './semantic-attempt-store';
export type UploadStorageIO={getItem:(key:string)=>Promise<string|null>;setItem:(key:string,value:string)=>Promise<void>;removeItem:(key:string)=>Promise<void>;getAllKeys:()=>Promise<readonly string[]>};
const prefix='photoeditor.uploads.v2:',epochs=new Map<string,number>();
const generationKey=(origin:string)=>prefix+'generation:'+origin;
export class ScopedUploadStorage implements UploadPersistence{
 readonly key:string;private epoch:number;private generation:string|null|undefined;
 constructor(private storage:UploadStorageIO,private origin:string,user:string,private tenant:string,private project:string,private collection?:string){
  if(typeof origin!=='string'||![user,tenant,project].every(archiveUUID)||(collection!==undefined&&!archiveUUID(collection)))throw new Error('UPLOAD_SCOPE_INVALID');
  this.key=prefix+JSON.stringify([origin,user,tenant,project,collection??null]);this.epoch=epochs.get(origin)??0;
 }
 private async fence(){
  if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('UPLOAD_SCOPE_EXPIRED');
  const current=await this.storage.getItem(generationKey(this.origin));
  if(this.epoch!==(epochs.get(this.origin)??0))throw new Error('UPLOAD_SCOPE_EXPIRED');
  if(current!==null&&!archiveUUID(current))throw new Error('UPLOAD_STORAGE_INVALID');
  if(this.generation===undefined)this.generation=current;else if(this.generation!==current)throw new Error('UPLOAD_SCOPE_EXPIRED');
 }
 private own(value:unknown):UploadRecord[]{
  if(!Array.isArray(value))throw new Error('UPLOAD_STORAGE_INVALID');
  const ids=new Set<string>();
  for(const row of value){if(!row||typeof row!=='object'||row.tenant!==this.tenant||row.projectId!==this.project||row.collectionId!==this.collection||!archiveUUID(row.localId)||ids.has(row.localId)||!Array.isArray(row.parts))throw new Error('UPLOAD_SCOPE_INVALID');ids.add(row.localId)}
  return JSON.parse(JSON.stringify(value)) as UploadRecord[];
 }
 load(){return semanticJournalLock(this.origin,async()=>{await this.fence();const raw=await this.storage.getItem(this.key);await this.fence();return raw===null?[]:this.own(JSON.parse(raw));});}
 save(records:UploadRecord[]){const owned=this.own(records);return semanticJournalLock(this.origin,async()=>{await this.fence();await this.storage.setItem(this.key,JSON.stringify(owned));await this.fence();});}
}
export function clearUploadStorage(storage:UploadStorageIO,origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return semanticJournalLock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  for(const key of await storage.getAllKeys()){
   if(!key.startsWith(prefix)||key===generationKey(origin))continue;
   let scope:unknown;try{scope=JSON.parse(key.slice(prefix.length))}catch{continue}
   if(Array.isArray(scope)&&scope[0]===origin)await storage.removeItem(key);
  }
  // Legacy rows have no owner/origin provenance and cannot be safely adopted.
  await storage.removeItem('photoeditor.uploads.v1');
 });
}
