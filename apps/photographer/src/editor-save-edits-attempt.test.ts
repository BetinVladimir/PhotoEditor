import {it,expect,vi} from 'vitest';
import {EditorSaveEditsAttemptStore,freezeSaveEditsBody,clearEditorSaveEditsAttempts,readSaveEditsResolution,type SaveEditsBody} from './editor-save-edits-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body:SaveEditsBody={source_revision:7,scope:'selected',selected_item_ids:[id(6)],overrides_id:id(7),output_name:'Edited'};
const ticket={run_id:id(8),output_collection_id:id(9),status:'queued'};
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
function fixture(){const values=new Map<string,string>();const disk={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};const create=()=>new EditorSaveEditsAttemptStore(disk,'https://studio.test',id(1),id(2),id(3),id(4),id(6),lock);return {values,disk,create};}
it('lost response reload replays exact durable body/key rather than a new draft',async()=>{
 const f=fixture(),first=f.create(),sent:{body:SaveEditsBody;key:string}[]=[];
 await expect(first.submit(body,async(b,key)=>{sent.push({body:b,key});throw Error('lost')},new AbortController().signal)).rejects.toThrow('lost');
 const restored=f.create();await restored.submit(null,async(b,key)=>{sent.push({body:b,key});return ticket},new AbortController().signal);
 expect(sent).toHaveLength(2);expect(sent[1]).toEqual(sent[0]);expect(sent[0].body).toEqual(body);
});
it('failed initial persistence prevents HTTP and retries the same owned key',async()=>{
 const f=fixture(),store=f.create(),write=f.disk.setItem;let fail=true;f.disk.setItem=async(k,v)=>{if(fail){fail=false;throw Error('disk')}await write(k,v)};
 const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket);await expect(store.submit(body,send,new AbortController().signal)).rejects.toThrow('disk');expect(send).not.toHaveBeenCalled();
 const remembered=await store.read();await store.submit(null,send,new AbortController().signal);expect(send.mock.calls[0][1]).toBe(remembered?.key);
});
it('known validated receipt wins over pending disk after receipt persistence failure',async()=>{
 const f=fixture(),store=f.create(),write=f.disk.setItem;let fail=true;f.disk.setItem=async(k,v)=>{if(fail&&v.includes('"run_id"')){fail=false;throw Error('receipt disk')}await write(k,v)};
 const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket);await expect(store.submit(body,send,new AbortController().signal)).rejects.toThrow('receipt disk');
 expect(await store.submit(null,send,new AbortController().signal)).toEqual(ticket);expect(send).toHaveBeenCalledTimes(1);
});
it('Stop during persistence prevents late POST and retains exact command',async()=>{
 const f=fixture(),store=f.create(),write=f.disk.setItem,controller=new AbortController();let release!:()=>void,entered!:()=>void;const started=new Promise<void>(r=>entered=r);
 f.disk.setItem=async(k,v)=>{entered();await new Promise<void>(r=>release=r);await write(k,v)};
 const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket),pending=store.submit(body,send,controller.signal).catch(e=>e);await started;controller.abort();release();await pending;
 expect(send).not.toHaveBeenCalled();expect(JSON.parse(f.values.get(store.key)!).body).toEqual(body);
});
it('late acknowledged clear can be retried without POST or erasing another intent',async()=>{
 const f=fixture(),store=f.create();await store.submit(body,async()=>ticket,new AbortController().signal);
 const key=store.identityKey()!,remove=f.disk.removeItem,controller=new AbortController();f.disk.removeItem=async k=>{await remove(k);controller.abort()};await expect(store.acknowledge(key,controller.signal)).rejects.toThrow();
 f.disk.removeItem=remove;await store.acknowledge(key,new AbortController().signal);expect(await store.read()).toBeNull();
});
it('refuses corrupt/different persisted identity and fences logout without cross actor replay',async()=>{
 const f=fixture(),store=f.create();f.values.set(store.key,'{');const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket);await expect(store.submit(body,send,new AbortController().signal)).rejects.toThrow();expect(send).not.toHaveBeenCalled();expect(f.values.get(store.key)).toBe('{');
 f.values.delete(store.key);await store.submit(body,async()=>ticket,new AbortController().signal);await clearEditorSaveEditsAttempts(f.disk,'https://studio.test',lock);await expect(store.read()).rejects.toThrow('EXPIRED');
});
it('freezes caller data and refuses unsafe numeric CAS before persistence',()=>{
 const mutable=structuredClone(body),frozen=freezeSaveEditsBody(mutable,id(6));mutable.output_name='other';expect(frozen.output_name).toBe('Edited');expect(()=>freezeSaveEditsBody({...body,source_revision:9007199254740992},id(6))).toThrow('INVALID');
});
it('explicit resolve keeps unknown request on denial and clears only exact server tombstone',async()=>{
 const f=fixture(),store=f.create();await expect(store.submit(body,async()=>{throw Error('lost')},new AbortController().signal)).rejects.toThrow();const saved=await store.read();
 await expect(store.resolve(store.identityKey()!,async()=>{throw Error('409')},new AbortController().signal)).rejects.toThrow('409');expect((await store.read())?.key).toBe(saved?.key);
 let sent='';const result=await store.resolve(store.identityKey()!,async(b,key)=>{expect(b).toEqual(body);sent=key;return {status:'abandoned',key,collection_id:id(4)}},new AbortController().signal);expect(sent).toBe(saved?.key);expect(result).toBeNull();expect(await store.read()).toBeNull();
});
it('resolution accepted receipt wins and prevents another normal POST',async()=>{
 const f=fixture(),store=f.create();await expect(store.submit(body,async()=>{throw Error('lost')},new AbortController().signal)).rejects.toThrow();expect(await store.resolve(store.identityKey()!,async()=>ticket,new AbortController().signal)).toEqual(ticket);
 const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket);expect(await store.submit(null,send,new AbortController().signal)).toEqual(ticket);expect(send).not.toHaveBeenCalled();
});

it('closed resolution rejects forged parent/key and extra accepted fields',()=>{
 const key=id(10);expect(()=>readSaveEditsResolution({status:'abandoned',key,collection_id:id(99)},id(4),key)).toThrow();expect(()=>readSaveEditsResolution({status:'abandoned',key:id(99),collection_id:id(4)},id(4),key)).toThrow();expect(()=>readSaveEditsResolution({...ticket,collection_id:id(4)},id(4),key)).toThrow();expect(readSaveEditsResolution(ticket,id(4),key)).toEqual(ticket);
});
it('durable scope closure fences another mounted instance from resurrecting an abandoned request',async()=>{
 const f=fixture(),a=f.create(),b=f.create();await expect(a.submit(body,async()=>{throw Error('lost')},new AbortController().signal)).rejects.toThrow();await b.read();
 await a.resolve(a.identityKey()!,async(_body,key)=>({status:'abandoned',key,collection_id:id(4)}),new AbortController().signal);
 const send=vi.fn(async(_body:SaveEditsBody,_key:string,_signal:AbortSignal)=>ticket);await expect(b.submit(null,send,new AbortController().signal)).rejects.toThrow('EXPIRED');expect(send).not.toHaveBeenCalled();
 const fresh=f.create();expect(await fresh.read()).toBeNull();await fresh.submit(body,send,new AbortController().signal);expect(send).toHaveBeenCalledTimes(1);
});
it('durable accepted acknowledgement fences stale instances and cross-window logout generation',async()=>{
 const f=fixture(),a=f.create(),b=f.create();await a.submit(body,async()=>ticket,new AbortController().signal);await b.read();await a.acknowledge(a.identityKey()!,new AbortController().signal);
 await expect(b.submit(null,async()=>ticket,new AbortController().signal)).rejects.toThrow('EXPIRED');
 const fresh=f.create();await fresh.read();f.values.set('photo.editor-save-edits-generation.v1:'+JSON.stringify('https://studio.test'),id(99));
 await expect(fresh.submit(body,async()=>ticket,new AbortController().signal)).rejects.toThrow('EXPIRED');
});

it('old acknowledgement key cannot clear a newer accepted command',async()=>{const f=fixture(),store=f.create();await store.submit(body,async()=>ticket,new AbortController().signal);const old=store.identityKey()!;await store.acknowledge(old,new AbortController().signal);await store.submit({...body,output_name:'New'},async()=>({...ticket,run_id:id(99)}),new AbortController().signal);const current=store.identityKey();await expect(store.acknowledge(old,new AbortController().signal)).rejects.toThrow('CONFLICT');expect(store.identityKey()).toBe(current);expect(store.accepted()?.run_id).toBe(id(99));});

it('queued old resolve key cannot send or close a newer command',async()=>{const f=fixture(),store=f.create();await store.submit(body,async()=>ticket,new AbortController().signal);const old=store.identityKey()!;await store.acknowledge(old,new AbortController().signal);await expect(store.submit({...body,output_name:'New'},async()=>{throw Error('unknown')},new AbortController().signal)).rejects.toThrow();const current=store.identityKey();const send=vi.fn(async(_body:SaveEditsBody,key:string)=>({status:'abandoned' as const,key,collection_id:id(4)}));await expect(store.resolve(old,send,new AbortController().signal)).rejects.toThrow('CONFLICT');expect(send).not.toHaveBeenCalled();expect(store.identityKey()).toBe(current);});
