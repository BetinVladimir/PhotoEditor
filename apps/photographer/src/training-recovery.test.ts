import {expect,it,vi} from 'vitest';
import {TrainingAttemptStore,clearTrainingAttempts,type TrainingCommand} from './training-attempt-store';
import {readTrainingSet,resumeTraining} from './training-recovery';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){const data=new Map<string,string>(),storage={getItem:async(key:string)=>data.get(key)??null,setItem:async(key:string,value:string)=>{data.set(key,value);},removeItem:async(key:string)=>{data.delete(key);},getAllKeys:async()=>[...data.keys()]},lock=async<T>(_origin:string,action:()=>Promise<T>)=>action(),origin='https://training-'+crypto.randomUUID()+'.test';return{data,storage,lock,origin,store:()=>new TrainingAttemptStore(storage,origin,id(1),id(2),id(3),lock)};}
const trainingSet=(pairs:unknown[]=[])=>({id:id(4),style_id:id(3),revision:'2',state:'draft',created_at:'now',pairs:{items:pairs,revision:'2'}});
const signal=()=>new AbortController().signal;
it('persists a returned acceptance after UI cancellation and recovers using GET only',async()=>{
 const f=fixture(),store=f.store(),state=await store.begin({kind:'create',body:{}}),controller=new AbortController();
 let resolve!:(value:unknown)=>void;const mutate=vi.fn(()=>new Promise(done=>{resolve=done})),read=vi.fn().mockResolvedValue(trainingSet()),api={mutate,trainingSet:read} as unknown as OwnerAPI;
 const pending=resumeTraining(api,id(3),store,state,controller.signal,()=>true);await vi.waitFor(()=>expect(mutate).toHaveBeenCalledTimes(1));controller.abort();resolve({id:id(4)});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});expect((await store.read()).pending?.ticket).toBe(id(4));expect(read).not.toHaveBeenCalled();
 await resumeTraining(api,id(3),f.store(),await store.read(),signal(),()=>true);expect(mutate).toHaveBeenCalledTimes(1);expect(read).toHaveBeenCalledTimes(1);
});
it('retains a known ticket in the scoped store when its durable receipt write fails',async()=>{
 const f=fixture(),store=f.store(),state=await store.begin({kind:'create',body:{}}),save=f.storage.setItem;
 f.storage.setItem=async()=>{throw Error('disk full');};
 const mutate=vi.fn().mockResolvedValue({id:id(4)}),api={mutate,trainingSet:vi.fn().mockResolvedValue(trainingSet())} as unknown as OwnerAPI;
 await expect(resumeTraining(api,id(3),store,state,signal(),()=>true)).rejects.toThrow('disk full');f.storage.setItem=save;
 await resumeTraining(api,id(3),store,state,signal(),()=>true);expect(mutate).toHaveBeenCalledTimes(1);
});
it.each(['command','set'])('refuses same-key substituted %s before reusing a cached accepted ticket',async mode=>{
 const f=fixture(),store=f.store();let state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);
 state=await store.begin({kind:'pair',set_id:id(4),body:{before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'original',confirmed:true,included:true}});
 const save=f.storage.setItem;f.storage.setItem=async()=>{throw Error('disk full');};await expect(store.receipt(state.pending!.key,id(7))).rejects.toThrow('disk full');f.storage.setItem=save;
 const changed=structuredClone(state);if(changed.pending!.command.kind!=='pair')throw Error('fixture');
 if(mode==='command')changed.pending!.command.body.session_id='substituted';else{changed.set_id=id(20);changed.pending!.command.set_id=id(20);}
 f.data.set(store.storageKey,JSON.stringify(changed));const mutate=vi.fn(),read=vi.fn();
 await expect(resumeTraining({mutate,trainingSet:read} as unknown as OwnerAPI,id(3),store,changed,signal(),()=>true)).rejects.toThrow('TRAINING_JOURNAL_CONFLICT');expect(mutate).not.toHaveBeenCalled();expect(read).not.toHaveBeenCalled();
});
it('recovers a lost creation response on remount with identical body/key before opening the same set',async()=>{
 const f=fixture(),store=f.store(),state=await store.begin({kind:'create',body:{}});const mutate=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({id:id(4)}),api={mutate,trainingSet:vi.fn().mockResolvedValue(trainingSet())} as unknown as OwnerAPI;
 await expect(resumeTraining(api,id(3),store,state,signal(),()=>true)).rejects.toThrow('lost');const restored=f.store(),saved=await restored.read();
 const result=await resumeTraining(api,id(3),restored,saved,signal(),()=>true);expect(result.state.set_id).toBe(id(4));expect(result.state.pending).toBeNull();expect(mutate.mock.calls[1][1]).toEqual(mutate.mock.calls[0][1]);expect(mutate.mock.calls[1][2]).toBe(mutate.mock.calls[0][2]);
});
it('persists an accepted creation ticket before GET and recovers with no second POST',async()=>{
 const f=fixture(),store=f.store(),state=await store.begin({kind:'create',body:{}});const mutate=vi.fn().mockResolvedValue({id:id(4)}),read=vi.fn().mockRejectedValueOnce(new Error('read lost')).mockResolvedValueOnce(trainingSet()),api={mutate,trainingSet:read} as unknown as OwnerAPI;
 await expect(resumeTraining(api,id(3),store,state,signal(),()=>true)).rejects.toThrow('read lost');const restored=f.store();expect((await restored.read()).pending?.ticket).toBe(id(4));
 await resumeTraining(api,id(3),restored,await restored.read(),signal(),()=>true);expect(mutate).toHaveBeenCalledOnce();
});
it('does not confirm a pair receipt until the exact before/after/session pair appears in the current set',async()=>{
 const f=fixture(),store=f.store();let state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);
 const command:TrainingCommand={kind:'pair',set_id:id(4),body:{before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'scene',confirmed:true,included:true}};state=await store.begin(command);
 const mutate=vi.fn().mockResolvedValue({id:id(7)}),read=vi.fn().mockResolvedValueOnce(trainingSet()).mockResolvedValueOnce(trainingSet([{id:id(7),training_set_id:id(4),...command.body,revision:'1',validation_status:'pending',created_at:'2026-10-17T00:00:00Z'}])),api={mutate,trainingSet:read} as unknown as OwnerAPI;
 await expect(resumeTraining(api,id(3),store,state,signal(),()=>true)).rejects.toThrow('TRAINING_PAIR_RECEIPT_MISSING');state=await store.read();expect(state.pending?.ticket).toBe(id(7));
 await resumeTraining(api,id(3),store,state,signal(),()=>true);expect(mutate).toHaveBeenCalledOnce();
});
it('retains the accepted analysis run for explicit opening after route loss',async()=>{
 const f=fixture(),store=f.store();let state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);
 state=await store.begin({kind:'analyze',set_id:id(4),body:{training_set_id:id(4),training_set_revision:2,analysis_options:{alignment_mode:'auto',geometry_enabled:true,seed:0}}});
 const api={mutate:vi.fn().mockResolvedValue({run_id:id(8)})} as unknown as OwnerAPI;await resumeTraining(api,id(3),store,state,signal(),()=>true);
 expect(await f.store().read()).toMatchObject({set_id:id(4),last_run_id:id(8),pending:null});
});
it('rejects changed pending commands and unsafe analysis revision rather than rebasing',async()=>{
 const f=fixture(),store=f.store();await store.begin({kind:'create',body:{}});
 expect(()=>store.begin({kind:'analyze',set_id:id(4),body:{training_set_id:id(4),training_set_revision:Number.MAX_SAFE_INTEGER+1,analysis_options:{alignment_mode:'auto',geometry_enabled:true,seed:0}}})).toThrow('TRAINING_JOURNAL_INVALID');
 await expect(store.begin({kind:'pair',set_id:id(4),body:{before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'scene',confirmed:true,included:true}})).rejects.toThrow('TRAINING_JOURNAL_CONFLICT');
});
it('fences a paused journal write against logout and leaves no resurrected attempt',async()=>{
 const f=fixture();let started!:()=>void,finish!:()=>void;const pending=new Promise<void>(resolve=>{started=resolve;}),original=f.storage.setItem;
 f.storage.setItem=async(key,value)=>{if(key.startsWith('photo.training-attempt.')){started();await new Promise<void>(resolve=>{finish=resolve;});}await original(key,value);};
 const creating=f.store().begin({kind:'create',body:{}}),rejection=expect(creating).rejects.toThrow('TRAINING_JOURNAL_EXPIRED');await pending;
 const logout=clearTrainingAttempts(f.storage,f.origin,f.lock);finish();await rejection;await logout;expect([...f.data.keys()].filter(key=>key.startsWith('photo.training-attempt.'))).toEqual([]);
});

it.each(['0','01','-1','1.0','9223372036854775808','999999999999999999999999'])('refuses invalid training revision %s without accepting a snapshot',async revision=>{
 const read=vi.fn().mockResolvedValue({...trainingSet(),revision});
 await expect(readTrainingSet({trainingSet:read} as unknown as OwnerAPI,id(4),id(3),signal())).rejects.toThrow('TRAINING_SET_INVALID');
});
it('accepts canonical int64 training revisions without numeric rounding',async()=>{
 const read=vi.fn().mockResolvedValue({...trainingSet(),revision:'9223372036854775807',pairs:{items:[],revision:'9223372036854775807'}});
 expect((await readTrainingSet({trainingSet:read} as unknown as OwnerAPI,id(4),id(3),signal())).revision).toBe('9223372036854775807');
});
it('does not call the training adapter for cancelled or malformed scope',async()=>{
 const read=vi.fn(),api={trainingSet:read} as unknown as OwnerAPI,controller=new AbortController();controller.abort();
 await expect(readTrainingSet(api,id(4),id(3),controller.signal)).rejects.toMatchObject({name:'AbortError'});
 await expect(readTrainingSet(api,'invalid',id(3),signal())).rejects.toThrow('TRAINING_SCOPE_INVALID');
 await expect(readTrainingSet(api,id(4),'00000000-0000-0000-0000-000000000000',signal())).rejects.toThrow('TRAINING_SCOPE_INVALID');
 expect(read).not.toHaveBeenCalled();
});
it('refuses changed revision across pages instead of combining different snapshots',async()=>{
 const read=vi.fn().mockResolvedValueOnce({...trainingSet(),pairs:{items:[],revision:'2',next_cursor:'next'}}).mockResolvedValueOnce({...trainingSet(),revision:'3'});
 await expect(readTrainingSet({trainingSet:read} as unknown as OwnerAPI,id(4),id(3),signal())).rejects.toThrow('TRAINING_SET_INVALID');
});

const completePair=()=>({id:id(7),training_set_id:id(4),before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'scene',confirmed:true,included:true,validation_status:'pending',revision:'1',created_at:'2026-10-17T00:00:00Z'});
it.each([{id:'bad'},{revision:'0'},{confirmed:'true'},{included:1},{before_asset_version_id:id(6)},{ignore_mask_artifact_id:null},{session_id:' '},{created_at:'invalid'}])('refuses malformed training pair DTO %j',async patch=>{
 const read=vi.fn().mockResolvedValue(trainingSet([{...completePair(),...patch}]));
 await expect(readTrainingSet({trainingSet:read} as unknown as OwnerAPI,id(4),id(3),signal())).rejects.toThrow('TRAINING_PAIR_INVALID');
});
it('keeps server-compatible Unicode session names in the durable pair command',async()=>{
 const f=fixture(),store=f.store();let state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);
 const body={before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'📷'.repeat(120),confirmed:true as const,included:true as const};
 state=await store.begin({kind:'pair',set_id:id(4),body});
 expect((await store.read()).pending?.command).toEqual({kind:'pair',set_id:id(4),body});
 expect(()=>store.begin({kind:'pair',set_id:id(4),body:{...body,session_id:'📷'.repeat(121)}})).toThrow('TRAINING_JOURNAL_INVALID');
});

it('refuses mismatched embedded training page revision',async()=>{
 const read=vi.fn().mockResolvedValue({...trainingSet(),pairs:{items:[],revision:'1'}});
 await expect(readTrainingSet({trainingSet:read} as unknown as OwnerAPI,id(4),id(3),signal())).rejects.toThrow('TRAINING_SET_INVALID');
});

it('does not bind a returned pair ticket to a command replaced during the POST',async()=>{
 const f=fixture(),store=f.store();let state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);
 state=await store.begin({kind:'pair',set_id:id(4),body:{before_asset_version_id:id(5),after_asset_version_id:id(6),session_id:'original',confirmed:true,included:true}});
 const read=vi.fn(),mutate=vi.fn(async()=>{
  const changed=JSON.parse(f.data.get(store.storageKey)!);changed.pending.command.body.session_id='replaced-during-post';f.data.set(store.storageKey,JSON.stringify(changed));return {id:id(7)};
 });
 await expect(resumeTraining({mutate,trainingSet:read} as unknown as OwnerAPI,id(3),store,state,signal(),()=>true)).rejects.toThrow('TRAINING_JOURNAL_CONFLICT');
 expect(mutate).toHaveBeenCalledOnce();expect(read).not.toHaveBeenCalled();expect(JSON.parse(f.data.get(store.storageKey)!).pending.ticket).toBeNull();
});
