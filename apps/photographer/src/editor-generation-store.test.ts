import {it,expect,vi} from 'vitest';
import {EditorGenerationStore,clearEditorGenerationAttempts} from './editor-generation-store';
import {EditorGenerationAttempt} from './editor-generation-attempt';
import type {EditorDraft,MaskRef} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function memory(){const values=new Map<string,string>();return{values,getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
function attempt(){return new EditorGenerationAttempt({asset_version_id:id(5),revision:'9',params:{},source_sha256:'a'.repeat(64),mask_refs:[]} as EditorDraft,{artifact:{artifact_id:id(6),sha256:'b'.repeat(64)},canvas:{width:10,height:10},transform_chain:{},selector:'object',confidence:1,protected:false} as MaskRef,'chosen',7);}
function store(storage:ReturnType<typeof memory>,origin='https://generation.example'){return new EditorGenerationStore(storage,origin,id(1),id(2),id(3),id(5),lock);}
const receipt={run_id:id(7),status:'queued'};
it('persists exact uncertain request and refuses conflicting writer or premature clear',async()=>{
 const storage=memory(),journal=store(storage),current=attempt();await journal.write(current.snapshot);
 expect(await journal.read()).toEqual(current.snapshot);await expect(journal.clear(current.key)).rejects.toThrow('GENERATION_JOURNAL_CONFLICT');
 const changed=current.snapshot;changed.input.seed=8;await expect(async()=>journal.write(changed)).rejects.toThrow('GENERATION_JOURNAL_CONFLICT');
 await expect(async()=>journal.write(attempt().snapshot)).rejects.toThrow('GENERATION_JOURNAL_CONFLICT');
});
it('refuses records bound to a different source version',async()=>{
 const journal=store(memory()),record=attempt().snapshot;record.input.draft.asset_version_id=id(9);await expect(async()=>journal.write(record)).rejects.toThrow('GENERATION_JOURNAL_SCOPE_INVALID');
});
it('clears acknowledged record and fences an already-open stale writer',async()=>{
 const storage=memory(),journal=store(storage),stale=store(storage),current=attempt();await journal.write(current.snapshot);await stale.read();await current.submit(vi.fn().mockResolvedValue(receipt));await journal.write(current.snapshot);await journal.clear(current.key);
 expect(await journal.read()).toBeNull();await expect(async()=>stale.write(current.snapshot)).rejects.toThrow('GENERATION_JOURNAL_EXPIRED');
});
it('partial delete failure leaves acknowledged record hidden to restart and retryable cleanup',async()=>{
 const storage=memory(),journal=store(storage),current=attempt();await current.submit(vi.fn().mockResolvedValue(receipt));await journal.write(current.snapshot);
 const remove=storage.removeItem;storage.removeItem=vi.fn().mockRejectedValueOnce(new Error('disk')).mockImplementation(remove);
 await expect(journal.clear(current.key)).rejects.toThrow('disk');expect(storage.values.has(journal.key)).toBe(true);expect(await store(storage).read()).toBeNull();await journal.clear(current.key);expect(storage.values.has(journal.key)).toBe(false);
});
it('logout invalidates existing instances and clears only matching origin',async()=>{
 const storage=memory(),journal=store(storage),other=store(storage,'https://other.example'),current=attempt();await journal.write(current.snapshot);await other.write(current.snapshot);
 await clearEditorGenerationAttempts(storage,'https://generation.example',lock);await expect(journal.read()).rejects.toThrow('GENERATION_JOURNAL_EXPIRED');expect((await other.read())?.key).toBe(current.key);expect(await store(storage).read()).toBeNull();
});
it('write failure prevents generation transport entirely',async()=>{
 const current=attempt().attach({write:vi.fn().mockRejectedValue(new Error('disk'))}),send=vi.fn();await expect(current.submit(send)).rejects.toThrow('disk');expect(send).not.toHaveBeenCalled();
});
it('receipt persistence failure retries same receipt without another transport',async()=>{
 const write=vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('receipt disk')).mockResolvedValue(undefined),current=attempt().attach({write}),send=vi.fn().mockResolvedValue(receipt);
 await expect(current.submit(send)).rejects.toThrow('receipt disk');expect(await current.submit(send)).toEqual(receipt);expect(send).toHaveBeenCalledOnce();expect(write.mock.calls[0][0].receipt).toBeNull();expect(write.mock.calls[1][0].receipt).toEqual(receipt);
});

it('post-journal capability loss blocks transport while retaining the same saved attempt',async()=>{
 const {admitEditorOperation}=await import('./editor-operation-admission');
 let finish!:()=>void;const write=vi.fn((_record:import('./editor-generation-attempt').EditorGenerationAttemptRecord)=>new Promise<void>(resolve=>{finish=resolve}));
 const current=attempt().attach({write}),transport=vi.fn();
 const pending=current.submit((...args)=>admitEditorOperation('image.generate',{current:()=>true,capabilities:()=>undefined},()=>transport(...args)));
 expect(write).toHaveBeenCalledOnce();finish();await expect(pending).rejects.toThrow('EDITOR_OPERATION_UNAVAILABLE');expect(transport).not.toHaveBeenCalled();expect(current.snapshot.key).toBe(write.mock.calls[0][0].key);
});

it('acknowledges only an unaccepted original key after confirmed abandonment',async()=>{
 const storage=memory(),journal=store(storage),current=attempt(),stale=store(storage);await journal.write(current.snapshot);await stale.read();
 await expect(journal.clearAbandoned({status:'abandoned',key:crypto.randomUUID()})).rejects.toThrow('GENERATION_JOURNAL_CONFLICT');
 await journal.clearAbandoned({status:'abandoned',key:current.key});expect(await journal.read()).toBeNull();await expect(async()=>stale.write(current.snapshot)).rejects.toThrow('GENERATION_JOURNAL_EXPIRED');
});
it('does not abandon a locally accepted generation receipt',async()=>{
 const storage=memory(),journal=store(storage),current=attempt();await current.submit(vi.fn().mockResolvedValue(receipt));await journal.write(current.snapshot);
 await expect(journal.clearAbandoned({status:'abandoned',key:current.key})).rejects.toThrow('GENERATION_JOURNAL_CONFLICT');expect(await journal.read()).toEqual(current.snapshot);
});
