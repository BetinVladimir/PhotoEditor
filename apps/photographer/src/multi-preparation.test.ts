import {MultiAttempt} from './multi-attempt';
import {describe,it,expect} from 'vitest';
import {MultiPreparationStore,clearMultiAttempts} from './multi-attempt-store';
import {beginMultiPreparation,MultiPreparation,type MultiPreparationAPI} from './multi-preparation';
import {renderInputSnapshotDigest} from './render-input-snapshot';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const params=()=>({schema_version:1 as const,frames:[4,6].map(n=>({asset_version_id:id(n),source:{artifact_id:id(n+1),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)})),reference_ordinal:1,max_pixels:1000000,calibration:{artifact_id:id(8),sha256:'c'.repeat(64)},projection:'planar' as const});
function fixture(){
 const values=new Map<string,string>();const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
 const store=()=>new MultiPreparationStore(storage,'https://multi.test',id(1),id(2),id(3),'optical.panorama',async(_origin,action)=>action());
 return {storage,store};
}
describe('multi preparation recovery',()=>{
 it('restarts both ambiguous POSTs with original order and keys',async()=>{
  const f=fixture(),store=f.store(),selection=params(),snapshots:unknown[]=[],intents:unknown[]=[];
  await beginMultiPreparation(store,'optical.panorama',id(3),selection,{snapshot:id(10),intent:id(11)});selection.frames.reverse();
  const api:MultiPreparationAPI={createRenderInputSnapshot:async(project,body,key)=>{snapshots.push({project,body:structuredClone(body),key});if(snapshots.length===1)throw new Error('snapshot response lost');return {id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length};},submitMulti:async(operation,project,body,key)=>{intents.push({operation,project,body:structuredClone(body),key});if(intents.length===1)throw new Error('intent response lost');return {run_id:id(13),workflow_id:id(14),state:'queued'};}};
  await expect(new MultiPreparation(store,api).submit()).rejects.toThrow('snapshot response lost');
  await expect(new MultiPreparation(f.store(),api).submit()).rejects.toThrow('intent response lost');
  await new MultiPreparation(f.store(),api).submit();
  expect(snapshots[0]).toEqual(snapshots[1]);expect(intents[0]).toEqual(intents[1]);expect((await store.read())?.params.frames[0].asset_version_id).toBe(id(4));
  await new MultiPreparation(f.store(),api).submit();expect(snapshots).toHaveLength(2);expect(intents).toHaveLength(2);
 });
 it('refuses unresolved replacement and logout resurrection',async()=>{
  const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
  await expect(beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(20),intent:id(21)})).rejects.toThrow('MULTI_JOURNAL_CONFLICT');
  await expect(store.clear(id(11))).rejects.toThrow();const record=(await store.read())!;
  await clearMultiAttempts(f.storage,'https://multi.test',async(_origin,action)=>action());await expect(store.write(record)).rejects.toThrow('MULTI_JOURNAL_EXPIRED');expect(await f.store().read()).toBeNull();
 });
 it('does not send cancelled preparation',async()=>{
  const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
  const controller=new AbortController();controller.abort();const fail=async()=>{throw new Error('unexpected IO');};
  await expect(new MultiPreparation(store,{createRenderInputSnapshot:fail,submitMulti:fail}).submit(controller.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');expect((await store.read())?.snapshot).toBeNull();
 });
});

it('owns snapshot receipt before a yielding journal write',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 let response:{id:string;digest:string;item_count:number}|undefined;
 const write=store.write.bind(store);
 store.write=async record=>{if(record.snapshot&&response)response.id=id(99);await write(record);};
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_project,body)=>{response={id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length};return response;},submitMulti:async(_operation,_project,body)=>{expect(body.snapshot_id).toBe(id(12));return{run_id:id(13),workflow_id:id(14),state:'queued'};}};
 await new MultiPreparation(store,api).submit();expect((await store.read())?.snapshot?.id).toBe(id(12));
});


it('journals a late acknowledged snapshot after cancellation and retries without a new snapshot',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 const controller=new AbortController();let snapshots=0,intents=0;
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_project,body,key)=>{snapshots++;expect(key).toBe(id(10));controller.abort();return{id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length};},submitMulti:async(_operation,_project,body,key)=>{intents++;expect(body.snapshot_id).toBe(id(12));expect(key).toBe(id(11));return{run_id:id(13),workflow_id:id(14),state:'queued'};}};
 await expect(new MultiPreparation(store,api).submit(controller.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');
 expect(intents).toBe(0);expect((await store.read())?.snapshot?.id).toBe(id(12));expect((await store.read())?.attempt).toBeNull();
 await new MultiPreparation(f.store(),api).submit();expect(snapshots).toBe(1);expect(intents).toBe(1);
});
it('does not admit a task when cancellation occurs during snapshot persistence',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 const controller=new AbortController(),write=store.write.bind(store);let sent=false;
 store.write=async record=>{await write(record);if(record.snapshot)controller.abort();};
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_project,body)=>({id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length}),submitMulti:async()=>{sent=true;throw new Error('unexpected admission');}};
 await expect(new MultiPreparation(store,api).submit(controller.signal)).rejects.toThrow('MULTI_ATTEMPT_CANCELLED');
 expect(sent).toBe(false);expect((await store.read())?.snapshot?.id).toBe(id(12));
});


it('keeps acknowledged ticket independent of persistence adapter writes',async()=>{
 const body={schema_version:1 as const,snapshot_id:id(12),params:params()};let sends=0;
 const attempt=new MultiAttempt('optical.panorama',id(3),id(11),body,{write:async record=>{if(record.ticket)record.ticket.run_id=id(99);}});
 const send=async()=>{sends++;return{run_id:id(13),workflow_id:id(14),state:'queued' as const};};
 const first=await attempt.submit(send);expect(first.run_id).toBe(id(13));first.run_id=id(98);
 const replay=await attempt.submit(send);expect(replay.run_id).toBe(id(13));expect(attempt.snapshot.ticket?.run_id).toBe(id(13));expect(sends).toBe(1);
});

it('retains the exact snapshot and request when admission is revoked between stages',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 let snapshots=0,intents=0,checks=0;
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_project,body)=>{snapshots++;return{id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length};},submitMulti:async()=>{intents++;return{run_id:id(13),workflow_id:id(14),state:'queued'};}};
 await expect(new MultiPreparation(store,api,async()=>{if(++checks===2)throw new Error('UNAVAILABLE');}).submit()).rejects.toThrow('UNAVAILABLE');
 const saved=await store.read();expect(saved?.snapshot?.id).toBe(id(12));expect(saved?.attempt?.key).toBe(id(11));expect(saved?.params.frames).toEqual(params().frames);
 expect(snapshots).toBe(1);expect(intents).toBe(0);
 await new MultiPreparation(store,api,async()=>{}).submit();expect(snapshots).toBe(1);expect(intents).toBe(1);
 await new MultiPreparation(store,api,async()=>{throw new Error('UNAVAILABLE');}).submit();expect(intents).toBe(1);
});
it('does not create a snapshot after cancellation during the availability read',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 let mutations=0;const controller=new AbortController();
 const api:MultiPreparationAPI={createRenderInputSnapshot:async()=>{mutations++;throw new Error('unexpected');},submitMulti:async()=>{mutations++;throw new Error('unexpected');}};
 await expect(new MultiPreparation(store,api,async()=>{controller.abort();}).submit(controller.signal)).rejects.toThrow('CANCELLED');
 expect(mutations).toBe(0);expect((await store.read())?.snapshot_key).toBe(id(10));
});

it('replays durable unknown inference after worker withdrawal while retaining fresh admission',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 let admissions=0,posts=0,available=true;const commands:unknown[]=[];
 const authorize=async()=>{admissions++;if(!available)throw new Error('worker unavailable');};
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_p,body)=>({id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length}),submitMulti:async(op,project,body,key)=>{commands.push({op,project,body,key});if(++posts===1)throw new Error('lost inference response');return {run_id:id(13),workflow_id:id(14),state:'queued'};}};
 await expect(new MultiPreparation(store,api,authorize).submit()).rejects.toThrow('lost inference response');
 available=false;const previous=admissions;
 await expect(new MultiPreparation(f.store(),api,authorize).submit()).resolves.toMatchObject({run_id:id(13)});
 expect(admissions).toBe(previous);expect(commands[1]).toEqual(commands[0]);
 const fresh=fixture();await beginMultiPreparation(fresh.store(),'optical.panorama',id(3),params());
 await expect(new MultiPreparation(fresh.store(),api,authorize).submit()).rejects.toThrow('worker unavailable');expect(posts).toBe(2);
});

it('publishes known acceptance even when its local receipt write fails',async()=>{
 const f=fixture(),store=f.store();await beginMultiPreparation(store,'optical.panorama',id(3),params(),{snapshot:id(10),intent:id(11)});
 const write=store.write.bind(store);store.write=async record=>{if(record.attempt?.ticket)throw new Error('accepted storage unavailable');return write(record);};
 let posts=0;const receipt={run_id:id(13),workflow_id:id(14),state:'queued' as const};
 const api:MultiPreparationAPI={createRenderInputSnapshot:async(_p,body)=>({id:id(12),digest:renderInputSnapshotDigest(body),item_count:body.sources.length}),submitMulti:async()=>{posts++;return receipt;}};
 await expect(new MultiPreparation(store,api).submit()).resolves.toEqual(receipt);expect(posts).toBe(1);
 expect((await store.read())?.attempt?.ticket).toBeNull(); // No fabricated durable acknowledgement.
 await expect(new MultiPreparation(store,api,async()=>{throw new Error('worker gone');}).submit()).resolves.toEqual(receipt);expect(posts).toBe(1);
 const saved=(await store.read())!;
 await write({...saved,attempt:{...saved.attempt!,ticket:{run_id:id(15),workflow_id:id(16),state:'queued'}}});
 await expect(new MultiPreparation(store,api).submit()).rejects.toThrow('MULTI_TICKET_SCOPE_CHANGED');expect(posts).toBe(1);
 await clearMultiAttempts(f.storage,'https://multi.test',async(_origin,action)=>action());
 await expect(new MultiPreparation(store,api).submit()).rejects.toThrow('MULTI_JOURNAL_EXPIRED');expect(posts).toBe(1);
});
