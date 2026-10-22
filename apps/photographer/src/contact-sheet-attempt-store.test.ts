import {expect,it} from 'vitest';
import {ContactSheetAttemptStore,type ContactSheetAttempt,clearContactSheetAttempts} from './contact-sheet-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function setup(){const data=new Map<string,string>();const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};const make=()=>new ContactSheetAttemptStore(storage,'https://photos.example',id(1),id(2),id(3));return {storage,make,store:make()};}
const record:ContactSheetAttempt={schema_version:1,collection_id:id(3),key:id(4),request:{selection_snapshot_id:id(5),locale:'en',columns:4,rows:5},ticket:null};
it('preserves exact snapshot/request/key and monotonic accepted ticket across remount',async()=>{
 const {store,make}=setup();await store.write(record);expect(await make().read()).toEqual(record);
 await expect(store.write({...record,key:id(6)})).rejects.toThrow('CONFLICT');
 await store.write({...record,ticket:{run_id:id(7)}});await expect(store.write(record)).rejects.toThrow('CONFLICT');
 await expect(store.discardRejected(record.key)).rejects.toThrow('CONFLICT');
 await make().clearAccepted(record.key,id(7));expect(await store.read()).toBeNull();
});
it('fences writes after logout',async()=>{
 const {store,storage}=setup();await store.write(record);await clearContactSheetAttempts(storage,'https://photos.example');
 await expect(store.write(record)).rejects.toThrow('EXPIRED');
});
it('fences stale-context restoration after server-confirmed abandonment',async()=>{
 const {store,make}=setup();await store.write(record);const other=make();await other.read();
 await store.clearAbandoned(record.key);expect(await store.read()).toBeNull();
 await expect(other.write(record)).rejects.toThrow('ALREADY_CLOSED');
 await make().write({...record,key:id(9)});expect((await make().read())?.key).toBe(id(9));
});
