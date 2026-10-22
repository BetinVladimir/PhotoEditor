import {expect,it,vi} from 'vitest';
import {CollectionVersionStore,clearCollectionVersionAttempts} from './collection-version-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const selection={collection:id(1),item:id(2),asset:id(3),revision:'7',expectedVersion:id(4),version:id(5)};
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
function fixture(){const values=new Map<string,string>();return {getItem:vi.fn(async(k:string)=>values.get(k)??null),setItem:vi.fn(async(k:string,v:string)=>{values.set(k,v)}),removeItem:vi.fn(async(k:string)=>{values.delete(k)}),getAllKeys:async()=>[...values.keys()]};}
it('retains one immutable selection across store instances and refuses another intent',async()=>{
 const storage=fixture(),make=()=>new CollectionVersionStore(storage,'https://retain.test',id(10),id(11),id(12),'a'.repeat(64),lock);
 await make().write(selection);expect(await make().read()).toEqual(selection);
 await expect(make().write({...selection,revision:'8'})).rejects.toThrow('CONFLICT');
 expect(await make().read()).toEqual(selection);
});
it('keeps conflicting choices and clears only after an authoritative applied read',async()=>{
 const storage=fixture(),store=new CollectionVersionStore(storage,'https://receipt.test',id(10),id(11),id(12),'a'.repeat(64),lock);
 await store.write(selection);
 await expect(store.resume({reconcileCollectionVersion:async()=> 'conflict',replaceCollectionVersion:vi.fn()})).rejects.toThrow('CONFLICT');
 expect(await store.read()).toEqual(selection);
 await store.resume({reconcileCollectionVersion:async()=> 'applied',replaceCollectionVersion:vi.fn()});expect(await store.read()).toBeNull();
});
it('logout invalidates old stores and removes only matching-origin journals',async()=>{
 const storage=fixture(),origin='https://logout.test',store=new CollectionVersionStore(storage,origin,id(10),id(11),id(12),'a'.repeat(64),lock);
 const other=new CollectionVersionStore(storage,'https://other.test',id(10),id(11),id(12),'a'.repeat(64),lock);
 await store.write(selection);await other.write(selection);await clearCollectionVersionAttempts(storage,origin,lock);
 await expect(store.read()).rejects.toThrow('EXPIRED');expect(await other.read()).toEqual(selection);
});

it('discards only a confirmed conflict and refuses losing pending or applied intent',async()=>{
 const storage=fixture(),store=new CollectionVersionStore(storage,'https://discard.test',id(10),id(11),id(12),'a'.repeat(64),lock);
 await store.write(selection);
 for(const state of ['unchanged','applied'] as const){
  await expect(store.discardConflicted({reconcileCollectionVersion:async()=>state})).rejects.toThrow('NOT_CONFLICTED');
  expect(await store.read()).toEqual(selection);
 }
 await store.discardConflicted({reconcileCollectionVersion:async()=> 'conflict'});expect(await store.read()).toBeNull();
});
