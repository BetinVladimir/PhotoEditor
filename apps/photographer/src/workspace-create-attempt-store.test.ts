import {expect,it} from 'vitest';
import {WorkspaceCreateAttemptStore,clearWorkspaceCreateAttempts,type WorkspaceCreateAttempt} from './workspace-create-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const origin='https://workspace-store.test';
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function setup(){const values=new Map<string,string>();const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};const make=(user=id(1),scope=origin)=>new WorkspaceCreateAttemptStore(storage,scope,user,lock);return{values,storage,make,store:make()};}
const record:WorkspaceCreateAttempt={schema_version:1,key:id(2),request:{name:'Wedding'},ticket:null};
it('reloads exact request and monotonically accepted tenant, clearing only matching receipt',async()=>{
 const {store,make}=setup();await store.write(record);expect(await make().read()).toEqual(record);
 await expect(store.write({...record,key:id(3)})).rejects.toThrow('CONFLICT');
 await expect(store.write({...record,request:{name:'Other'}})).rejects.toThrow('CONFLICT');
 const accepted={...record,ticket:{tenant_id:id(4)}};await store.write(accepted);expect(await make().read()).toEqual(accepted);
 await expect(store.write(record)).rejects.toThrow('CONFLICT');await expect(store.discardRejected(record.key)).rejects.toThrow('CONFLICT');
 await expect(store.clearAccepted(record.key,id(5))).rejects.toThrow('CONFLICT');
 await make().clearAccepted(record.key,id(4));expect(await store.read()).toBeNull();
});
it('rejects collection transformation fields and follows server120 codepoint name limit',async()=>{
 const {store}=setup();
 for(const value of [{...record,project_id:id(3)},{...record,request:{name:'Wedding',description:''}},{...record,ticket:{collection_id:id(4)}},{...record,request:{name:'x'.repeat(121)}}]){
  await expect(async()=>store.write(value as WorkspaceCreateAttempt)).rejects.toThrow('INVALID');
 }
 await store.write({...record,request:{name:'📷'.repeat(120)}});expect((await store.read())?.request.name).toBe('📷'.repeat(120));
});
it('isolates users and origins and fences old stores after logout generation advances',async()=>{
 const {store,make,storage}=setup();await store.write(record);
 expect(await make(id(7)).read()).toBeNull();expect(await make(id(1),'https://another.test').read()).toBeNull();
 await clearWorkspaceCreateAttempts(storage,origin,lock);
 await expect(store.write(record)).rejects.toThrow('EXPIRED');expect(await make().read()).toBeNull();
});
it('logout queues behind a pending write and removes its late envelope',async()=>{
 const {store,storage,make}=setup();let release!:()=>void,entered!:()=>void;
 const gate=new Promise<void>(done=>{release=done}),started=new Promise<void>(done=>{entered=done});const original=storage.setItem;
 storage.setItem=async(key,value)=>{if(key===store.storageKey){entered();await gate;}await original(key,value);};
 const write=store.write(record);const rejected=expect(write).rejects.toThrow('EXPIRED');await started;
 const clear=clearWorkspaceCreateAttempts(storage,origin,lock);release();await rejected;await clear;
 expect(await make().read()).toBeNull();
});
it('a persisted cross-session generation change refuses stale reads and writes',async()=>{
 const {store,storage}=setup();await store.write(record);
 await storage.setItem('photo.workspace-create-generation.v1:'+JSON.stringify(origin),id(8));
 await expect(store.read()).rejects.toThrow('EXPIRED');await expect(store.write(record)).rejects.toThrow('EXPIRED');
});
