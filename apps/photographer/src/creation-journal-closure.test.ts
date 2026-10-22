import {expect,it} from 'vitest';
import {AccessLinkAttemptStore,clearAccessLinkAttempts} from './access-link-attempt-store';
import {CollectionCreateAttemptStore,clearCollectionCreateAttempts} from './collection-create-attempt-store';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock:SemanticJournalLock=async(_origin,action)=>action();
function storage(){const rows=new Map<string,string>();return {rows,failRemove:false,async getAllKeys(){return [...rows.keys()];},async getItem(key:string){return rows.get(key)??null;},async setItem(key:string,value:string){rows.set(key,value);},async removeItem(key:string){if(this.failRemove)throw Error('disk unavailable');rows.delete(key);}};}
function harness(kind:'access'|'collection',disk:ReturnType<typeof storage>){
 const policy={label:'Client',permissions:{view:true,download:false,upload:false},limits:{max_upload_files:10,max_upload_bytes_total:1000,max_file_bytes:100,allowed_formats:['jpeg']},moderation:'owner_review' as const,allowed_presets:[]};
 if(kind==='access'){
  const store=new AccessLinkAttemptStore(disk,'https://photo.test',id(1),id(2),id(3),lock);
  return {read:()=>store.read(),pending:(key:string)=>store.write({schema_version:1,key,request:policy,ticket:null}),accepted:(key:string)=>store.write({schema_version:1,key,request:policy,ticket:{resource_id:id(6)}}),closePending:(key:string)=>store.clearAbandoned(key),closeAccepted:(key:string)=>store.clearAccepted(key,id(6))};
 }
 const store=new CollectionCreateAttemptStore(disk,'https://photo.test',id(1),id(2),id(3),lock);
 const request={project_id:id(3),name:'Collection',description:'' as const};
 return {read:()=>store.read(),pending:(key:string)=>store.write({schema_version:1,project_id:id(3),key,request,ticket:null}),accepted:(key:string)=>store.write({schema_version:1,project_id:id(3),key,request,ticket:{collection_id:id(6)}}),closePending:(key:string)=>store.clearAbandoned(key),closeAccepted:(key:string)=>store.clearAccepted(key,id(6))};
}
for(const kind of ['access','collection'] as const){
 it(`${kind}: another live store cannot resurrect a confirmed closed attempt`,async()=>{
  for(const accepted of [false,true]){
   const disk=storage(),first=harness(kind,disk),stale=harness(kind,disk),key=id(4);
   await first.pending(key);await stale.read();
   if(accepted){await first.accepted(key);await first.closeAccepted(key);}else await first.closePending(key);
   await expect(stale.pending(key)).rejects.toThrow('JOURNAL_CLOSED');
   await expect(stale.accepted(key)).rejects.toThrow('JOURNAL_CLOSED');
   expect(await stale.read()).toBeNull();
   await stale.pending(id(5));expect((await stale.read())?.key).toBe(id(5));
  }
 });
 it(`${kind}: durable closure survives a failed journal removal and a new instance`,async()=>{
  const disk=storage(),first=harness(kind,disk),key=id(4);await first.pending(key);
  disk.failRemove=true;await expect(first.closePending(key)).rejects.toThrow('disk unavailable');
  disk.failRemove=false;const reopened=harness(kind,disk);
  expect(await reopened.read()).toBeNull();await expect(reopened.pending(key)).rejects.toThrow('JOURNAL_CLOSED');
  await reopened.pending(id(5));expect((await reopened.read())?.key).toBe(id(5));
 });
 it(`${kind}: a retained acknowledgement cannot clear the replacement request`,async()=>{
  const disk=storage(),old=harness(kind,disk),fresh=harness(kind,disk);
  await old.pending(id(4));await old.closePending(id(4));await fresh.pending(id(5));
  await expect(old.closePending(id(4))).rejects.toThrow('JOURNAL_CONFLICT');
  expect((await fresh.read())?.key).toBe(id(5));
 });
 it(`${kind}: logout removes closed markers and fences old instances without clearing another origin`,async()=>{
  const disk=storage(),old=harness(kind,disk),key=id(4);await old.pending(key);await old.closePending(key);
  const prefix=kind==='access'?'photo.access-link-create-attempt.v1:':'photo.collection-create-attempt.v1:';
  const foreignKey=prefix+JSON.stringify(['https://other.test',id(1),id(2),id(3)]);
  disk.rows.set(foreignKey,'foreign-origin-data');
  expect([...disk.rows.keys()].some(key=>key.startsWith(prefix)&&key.includes('closed'))).toBe(true);
  await (kind==='access'?clearAccessLinkAttempts:clearCollectionCreateAttempts)(disk,'https://photo.test',lock);
  expect([...disk.rows.keys()].filter(key=>key.startsWith(prefix))).toEqual([foreignKey]);
  expect(disk.rows.get(foreignKey)).toBe('foreign-origin-data');
  await expect(old.pending(id(5))).rejects.toThrow('JOURNAL_EXPIRED');
 });
}
