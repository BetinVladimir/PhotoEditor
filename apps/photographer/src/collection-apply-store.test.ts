import {it,expect} from 'vitest';
import {CollectionApplyStore,clearCollectionApplyAttempts,type CollectionApplyRecord} from './collection-apply-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
function fixture():CollectionApplyRecord{return {schema_version:1,collection:{id:id(4),tenant_id:id(2),project_id:id(3),revision:'7'},version:id(5),strength:.5,name:'Edited',key:id(6),accepted:null};}
it('restores exact pending request and refuses replacement or premature clear',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock),record=fixture();
 await store.write(record);record.name='mutated';expect((await store.read())?.name).toBe('Edited');
 const restored=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);expect(await restored.read()).toEqual(fixture());
 await expect(restored.write({...fixture(),key:id(9)})).rejects.toThrow('CONFLICT');await expect(restored.clear(id(6))).rejects.toThrow('CONFLICT');
 const accepted={...fixture(),accepted:{run_id:id(7),output_collection_id:id(8),status:'queued'}};await restored.write(accepted);await expect(restored.write(fixture())).rejects.toThrow('CONFLICT');await restored.clear(id(6));expect(await restored.read()).toBeNull();
});
it('isolates users and fences old instances after logout',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);await store.write(fixture());
 expect(await new CollectionApplyStore(disk,'https://studio.test',id(9),id(2),id(3),id(4),lock).read()).toBeNull();await clearCollectionApplyAttempts(disk,'https://studio.test',lock);await expect(store.read()).rejects.toThrow('EXPIRED');expect(await new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock).read()).toBeNull();
});

it('serializes concurrent resumption and reuses committed receipt without a second HTTP call',async()=>{
 const disk=storage(),first=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock),second=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);
 let calls=0;const send=async(record:CollectionApplyRecord)=>{calls++;expect(record).toEqual(fixture());return {run_id:id(7),output_collection_id:id(8),status:'queued'}};
 const [a,b]=await Promise.all([first.resume(fixture(),send),second.resume(fixture(),send)]);expect(calls).toBe(1);expect(a).toEqual(b);expect(a.accepted?.run_id).toBe(id(7));
});
it('retains pending request after lost reply and refuses a conflicting submission before HTTP',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);
 await expect(store.resume(fixture(),async()=>{throw new Error('reply lost')})).rejects.toThrow('reply lost');expect(await store.read()).toEqual(fixture());
 let calls=0;const send=async()=>{calls++;return {run_id:id(7),output_collection_id:id(8),status:'queued'}};
 await expect(store.resume({...fixture(),key:id(9)},send)).rejects.toThrow('CONFLICT');expect(calls).toBe(0);await store.resume(fixture(),send);expect(calls).toBe(1);
});

it('refuses missing receipt and substituted acknowledged receipt',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);
 await expect(store.resume(fixture(),async()=>null as never)).rejects.toThrow('INVALID');expect((await store.read())?.accepted).toBeNull();
 const accepted=await store.resume(fixture(),async()=>({run_id:id(7),output_collection_id:id(8),status:'queued'}));
 await expect(store.resume({...accepted,accepted:{...accepted.accepted!,run_id:id(9)}},async()=>{throw new Error('HTTP must not run')})).rejects.toThrow('CONFLICT');
});

it('logout during HTTP fences late receipt and cannot resurrect the journal',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);
 let release!:(value:{run_id:string;output_collection_id:string;status:string})=>void,started!:()=>void;
 const entered=new Promise<void>(resolve=>started=resolve);
 const pending=store.resume(fixture(),async()=>{started();return new Promise(resolve=>release=resolve)});
 await entered;const outcome=pending.catch(error=>error);const clearing=clearCollectionApplyAttempts(disk,'https://studio.test',lock);
 release({run_id:id(7),output_collection_id:id(8),status:'queued'});expect(String(await outcome)).toContain('EXPIRED');await clearing;
 expect(await new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock).read()).toBeNull();
});
it('retains exact pending identity when accepted receipt persistence fails',async()=>{
 const disk=storage(),set=disk.setItem;let fail=true;
 disk.setItem=async(key,value)=>{if(fail&&value.includes('"run_id"')){fail=false;throw new Error('receipt disk failure')}await set(key,value)};
 const store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);const keys:string[]=[];
 const send=async(record:CollectionApplyRecord)=>{keys.push(record.key);return {run_id:id(7),output_collection_id:id(8),status:'queued'}};
 await expect(store.resume(fixture(),send)).rejects.toThrow('receipt disk failure');expect(await store.read()).toEqual(fixture());
 const result=await store.resume(fixture(),send);expect(keys).toEqual([id(6),id(6)]);expect(result.accepted?.run_id).toBe(id(7));
});

it('releases only the exact rejected pending identity and never an accepted receipt',async()=>{
 const disk=storage(),store=new CollectionApplyStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),lock);
 await store.write(fixture());await expect(store.discardRejected(id(99))).rejects.toThrow('CONFLICT');expect(await store.read()).toEqual(fixture());
 await store.discardRejected(fixture().key);expect(await store.read()).toBeNull();
 await store.resume(fixture(),async()=>({run_id:id(7),output_collection_id:id(8),status:'queued'}));
 await expect(store.discardRejected(fixture().key)).rejects.toThrow('CONFLICT');expect((await store.read())?.accepted?.run_id).toBe(id(7));
});
