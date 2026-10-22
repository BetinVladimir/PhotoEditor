import {describe,it,expect,vi} from 'vitest';
import {LensPreparationStore,beginLensPreparation,LensPreparation} from './lens-preparation';
import {clearLensAttempts} from './lens-journal-lifecycle';
import {renderInputSnapshotDigest} from './render-input-snapshot';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){const values=new Map<string,string>();const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};return {values,storage,store:()=>new LensPreparationStore(storage,'https://lens.test',id(1),id(2),id(3),id(4))};}
const sources=()=>[{asset_version_id:id(5),source:{artifact_id:id(6),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}];
const choices=()=>[{asset_version_id:id(5),profile:{artifact_id:id(7),sha256:'c'.repeat(64)},strength:1}];
const occurrences=()=>[{collection:id(4),item:id(8),asset:id(9),revision:'1',expectedVersion:id(5)}];
const begin=(store:LensPreparationStore)=>beginLensPreparation(store,id(1),id(3),sources(),choices(),occurrences());
describe('Lens frozen selection recovery',()=>{
 it('retries ambiguous snapshot and intent with identical stable keys',async()=>{const f=fixture(),store=f.store();await begin(store);const snapshots:unknown[]=[],intents:unknown[]=[];
 const snapshot=async(project:string,request:{sources:ReturnType<typeof sources>},key:string)=>{snapshots.push({project,request,key});if(snapshots.length===1)throw new Error('snapshot lost');return {id:id(10),digest:renderInputSnapshotDigest(request),item_count:1};};
 const submit=async(body:unknown,versions:readonly string[],key:string)=>{intents.push({body,versions,key});if(intents.length===1)throw new Error('intent lost');return {run_id:id(11),workflow_id:id(12),state:'queued' as const};};
 await expect(new LensPreparation(store,snapshot,submit).resume()).rejects.toThrow('snapshot lost');
 await expect(new LensPreparation(f.store(),snapshot,submit).resume()).rejects.toThrow('intent lost');
 await new LensPreparation(f.store(),snapshot,submit).resume();expect(snapshots[0]).toEqual(snapshots[1]);expect(intents[0]).toEqual(intents[1]);
 await new LensPreparation(f.store(),snapshot,submit).resume();expect(intents).toHaveLength(2);
 });
 it('refuses changes to the frozen collection occurrence while allowing receipt advancement',async()=>{const f=fixture(),store=f.store(),value=await begin(store);const changed=structuredClone(value);changed.occurrences![0].revision='2';await expect(store.write(changed)).rejects.toThrow('LENS_JOURNAL_CONFLICT');changed.occurrences![0].revision='1';changed.occurrences![0].item=id(20);await expect(store.write(changed)).rejects.toThrow('LENS_JOURNAL_CONFLICT');const receipt={...value,snapshot:{id:id(10),digest:renderInputSnapshotDigest({sources:value.sources}),item_count:1}};await store.write(receipt);expect((await store.read())?.snapshot?.id).toBe(id(10));});
 it('journals a late snapshot receipt then stops before task admission',async()=>{const f=fixture(),store=f.store();await begin(store);const controller=new AbortController(),submit=vi.fn();const snapshot=async(_project:string,request:{sources:ReturnType<typeof sources>})=>{controller.abort();return{id:id(10),digest:renderInputSnapshotDigest(request),item_count:1};};await expect(new LensPreparation(store,snapshot,submit).resume(controller.signal)).rejects.toThrow();expect(submit).not.toHaveBeenCalled();expect((await store.read())?.snapshot?.id).toBe(id(10));});
 it('does not resurrect journals after logout during a committed task response',async()=>{const f=fixture(),store=f.store();await begin(store);const snapshot=async(_project:string,request:{sources:ReturnType<typeof sources>})=>({id:id(10),digest:renderInputSnapshotDigest(request),item_count:1});const submit=async()=>{await clearLensAttempts(f.storage,'https://lens.test');return{run_id:id(11),workflow_id:id(12),state:'queued' as const};};await expect(new LensPreparation(store,snapshot,submit).resume()).rejects.toThrow('LENS_JOURNAL_EXPIRED');expect(await f.store().read()).toBeNull();});
 it('archives immutable request and frees a fresh batch without overwriting history',async()=>{const f=fixture(),store=f.store();const value=await begin(store),snapshot={id:id(10),digest:renderInputSnapshotDigest({sources:value.sources}),item_count:1};await store.write({...value,snapshot,ticket:{run_id:id(11),workflow_id:id(12),state:'queued'}});await store.archive();expect(await store.read()).toBeNull();expect([...f.values.keys()].some(k=>k.endsWith(':history:'+id(11)))).toBe(true);await begin(store);expect((await store.read())?.intent_key).not.toBe(value.intent_key);});
});
