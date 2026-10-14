import {it,expect} from 'vitest';
import {prepareNextPerspective} from './perspective-next';
import type {PerspectiveAttemptRecord} from './perspective-attempt';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function record():PerspectiveAttemptRecord{return {schema_version:1,project:id(1),key:'old-key',ticket:{run_id:id(2),workflow_id:id(3),state:'queued'},body:{schema_version:1,snapshot_id:id(4),params:{schema_version:1,base:{asset_version_id:id(5),source:{artifact_id:id(6),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}}};}
it('retains journal until workflow/source/snapshot authority has been checked',async()=>{
 for(const fault of ['','unresolved','workflow','source','snapshot','cancel']){
  const r=record(),calls:string[]=[],controller=new AbortController();
  if(fault==='unresolved')r.ticket=null;
  const api={get:async()=>{calls.push('workflow');return {run_id:fault==='workflow'?id(99):id(2),workflow_id:id(3)};},v2Binding:async()=>{calls.push('source');return {...r.body.params.base,asset_version_id:fault==='source'?id(99):id(5)};},createRenderInputSnapshot:async()=>{calls.push('snapshot');if(fault==='snapshot')throw new Error('unavailable');if(fault==='cancel')controller.abort();return {id:id(7)};}} as unknown as Pick<OwnerAPI,'get'|'v2Binding'|'createRenderInputSnapshot'>;
  const store={clear:async(key:string)=>{calls.push('clear');expect(key).toBe('old-key');}};
  if(fault===''){const next=await prepareNextPerspective(api,id(8),r,store,controller.signal);expect(next.snapshot_id).toBe(id(7));expect(r.body.snapshot_id).toBe(id(4));expect(calls).toEqual(['workflow','source','snapshot','clear']);}
  else {await expect(prepareNextPerspective(api,id(8),r,store,controller.signal)).rejects.toThrow();expect(calls).not.toContain('clear');}
 }
});
