import {expect,it} from 'vitest';
import {CollectionCreateAttemptStore,type CollectionCreateAttempt,clearCollectionCreateAttempts} from './collection-create-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function setup(){const data=new Map<string,string>();const storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};const make=()=>new CollectionCreateAttemptStore(storage,'https://photos.example',id(1),id(2),id(3));return {storage,make,store:make()};}
const record:CollectionCreateAttempt={schema_version:1,project_id:id(3),key:id(4),request:{project_id:id(3),name:'Wedding',description:''},ticket:null};
it('preserves exact snapshot/request/key and monotonic accepted ticket across remount',async()=>{
 const {store,make}=setup();await store.write(record);expect(await make().read()).toEqual(record);
 await expect(async()=>store.write({...record,key:id(6)})).rejects.toThrow('CONFLICT');
 await store.write({...record,ticket:{collection_id:id(7)}});await expect(async()=>store.write(record)).rejects.toThrow('CONFLICT');
 await expect(store.discardRejected(record.key)).rejects.toThrow('CONFLICT');
 await make().clearAccepted(record.key,id(7));expect(await store.read()).toBeNull();
});
it('fences writes after logout',async()=>{
 const {store,storage}=setup();await store.write(record);await clearCollectionCreateAttempts(storage,'https://photos.example');
 await expect(async()=>store.write(record)).rejects.toThrow('EXPIRED');
});

it('rejects a request bound to a different project',async()=>{
 const {store}=setup();await expect(async()=>store.write({...record,request:{...record.request,project_id:id(99)}})).rejects.toThrow('INVALID');
});
it('rejects changed names under the same key',async()=>{
 const {store}=setup();await store.write(record);await expect(async()=>store.write({...record,request:{...record.request,name:'Other'}})).rejects.toThrow('CONFLICT');
});
