import {expect,it} from 'vitest';
import {cullingDecisionIntent,type CullingSelection} from './culling-selection';
import {CullingDecisionAttemptStore,clearCullingDecisionAttempts} from './culling-decision-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64),origin='https://culling-store.example';
function fixture(){
 const data=new Map<string,string>(),storage={getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};
 const selection:CullingSelection={review_id:id(1),run_id:id(2),revision:1,state:'pending',candidate_hash:sha,snapshot_id:id(3),snapshot_digest:sha,never_delete:true,total:1,groups:{artifact_id:id(4),sha256:sha},proposal:{artifact_id:id(5),sha256:sha},selected_item_ids:null,confirmed_eyes_item_ids:null,items:[{item_id:id(10),asset_id:id(11),asset_version_id:id(12),scene_id:'scene',exact_group_id:'exact',burst_group_id:'burst',rank:1,score:.7,recommended:true,manual:false,eye_review:false,eye_state:'open'}]};
 const body=cullingDecisionIntent(id(6),id(7),selection,[id(10)],[]),record={version:1 as const,key:id(8),body},make=()=>new CullingDecisionAttemptStore(storage,origin,id(7),id(6),id(1));return {storage,make,record,store:make()};
}
it('persists immutable actor/source/selection and only clears a verified matching receipt',async()=>{
 const f=fixture();await f.store.write(f.record);expect(await f.make().read()).toEqual(f.record);
 await expect(async()=>f.store.write({...f.record,key:id(9)})).rejects.toThrow('CONFLICT');
 const receipt={id:id(1),run_id:id(2),kind:'culling',state:'decided',revision:2,candidate_hash:sha,decision:'accept',decision_route:'selection',decision_hash:f.record.body.decision_hash,reasons:[],created_at:'2026-10-17T00:00:00Z'};
 await expect(f.store.clear(f.record.key,{...receipt,decision_hash:'b'.repeat(64)})).rejects.toThrow('CONFLICT');expect(await f.store.read()).toEqual(f.record);
 await f.make().clear(f.record.key,receipt);expect(await f.store.read()).toBeNull();
});
it('rejects another actor intent and fences an old store after logout',async()=>{
 const f=fixture();await expect(async()=>f.store.write({...f.record,body:cullingDecisionIntent(id(6),id(9),f.record.body.selection,[id(10)],[])})).rejects.toThrow('SCOPE');
 await f.store.write(f.record);await clearCullingDecisionAttempts(f.storage,origin);await expect(async()=>f.store.write(f.record)).rejects.toThrow('EXPIRED');expect(await f.make().read()).toBeNull();
});
it('detects cross-window generation changes',async()=>{const f=fixture();await f.store.read();await f.storage.setItem('photo.culling-decision-generation.v1:'+JSON.stringify(origin),id(9));await expect(async()=>f.store.write(f.record)).rejects.toThrow('EXPIRED');});
