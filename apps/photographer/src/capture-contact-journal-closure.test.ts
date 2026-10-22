import {expect,it} from 'vitest';
import {CaptureCreateAttemptStore,clearCaptureCreateAttempts} from './capture-create-attempt-store';
import {ContactSheetAttemptStore,clearContactSheetAttempts} from './contact-sheet-attempt-store';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const origin='https://capture-closure.test';
const lock:SemanticJournalLock=async(_origin,action)=>action();
function disk(){const rows=new Map<string,string>();return {rows,failRemove:false,async getItem(key:string){return rows.get(key)??null;},async setItem(key:string,value:string){rows.set(key,value);},async removeItem(key:string){if(this.failRemove)throw Error('disk unavailable');rows.delete(key);},async getAllKeys(){return [...rows.keys()];}};}
function harness(kind:'capture'|'contact',storage:ReturnType<typeof disk>){
 if(kind==='contact'){
  const store=new ContactSheetAttemptStore(storage,origin,id(1),id(2),id(3),lock);
  const record={schema_version:1 as const,collection_id:id(3),request:{selection_snapshot_id:id(5),locale:'en' as const,columns:4 as const,rows:5 as const}};
  return {read:()=>store.read(),write:(key:string,accepted=false)=>store.write({...record,key,ticket:accepted?{run_id:id(6)}:null}),close:(key:string,accepted=false)=>accepted?store.clearAccepted(key,id(6)):store.discardRejected(key)};
 }
 const store=new CaptureCreateAttemptStore(storage,origin,id(1),id(2),id(3),lock);
 const calibration={artifact_id:id(5),sha256:'a'.repeat(64)};
 const request={project_id:id(3),collection_id:id(4),name:'Studio',policy:{schema_version:1 as const,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration},capture_policy:{schema_version:1 as const,checks:['focus' as const],max_queue_age_seconds:30 as const,calibration}};
 return {read:()=>store.read(),write:(key:string,accepted=false)=>store.write({schema_version:1,key,request,ticket:accepted?{resource_id:id(6)}:null}),close:(key:string,accepted=false)=>accepted?store.clearAccepted(key,id(6)):store.discardRejected(key)};
}
for(const kind of ['capture','contact'] as const){
 for(const accepted of [false,true]){
  it(`${kind}: terminal ${accepted?'acceptance':'rejection'} fences stale instances and permits a new key`,async()=>{
   const storage=disk(),first=harness(kind,storage),stale=harness(kind,storage),key=id(7);
   await first.write(key);await stale.read();if(accepted)await first.write(key,true);await first.close(key,accepted);
   for(const ticket of [false,true])await expect(stale.write(key,ticket)).rejects.toThrow('ALREADY_CLOSED');
   expect(await stale.read()).toBeNull();await stale.write(id(8));
   await expect(first.close(key,accepted)).rejects.toThrow('CONFLICT');expect((await stale.read())?.key).toBe(id(8));
  });
  it(`${kind}: closure survives failed removal after ${accepted?'acceptance':'rejection'}`,async()=>{
   const storage=disk(),first=harness(kind,storage),key=id(7);await first.write(key,accepted);
   storage.failRemove=true;await expect(first.close(key,accepted)).rejects.toThrow('disk unavailable');
   storage.failRemove=false;const reopened=harness(kind,storage);expect(await reopened.read()).toBeNull();
   await expect(reopened.write(key)).rejects.toThrow('ALREADY_CLOSED');await reopened.write(id(8));expect((await reopened.read())?.key).toBe(id(8));
  });
 }
 it(`${kind}: logout clears only this origin's closed markers and fences old writers`,async()=>{
  const storage=disk(),old=harness(kind,storage);await old.write(id(7));await old.close(id(7));
  const prefix=kind==='capture'?'photo.capture-create-closed.v1:':'photo.contact-sheet-closed.v1:';
  const foreign=prefix+JSON.stringify(['https://other.test','scope',id(7)]);storage.rows.set(foreign,'1');
  await (kind==='capture'?clearCaptureCreateAttempts:clearContactSheetAttempts)(storage,origin,lock);
  expect([...storage.rows.keys()].filter(key=>key.startsWith(prefix))).toEqual([foreign]);
  await expect(old.write(id(8))).rejects.toThrow('EXPIRED');
 });
}
