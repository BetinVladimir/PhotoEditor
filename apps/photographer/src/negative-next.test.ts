import {it,expect} from 'vitest';
import {prepareNextNegative} from './negative-next';
import type {NegativeAttemptRecord} from './negative-attempt';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const record=():NegativeAttemptRecord=>({schema_version:1,project:id(1),key:'old',ticket:{run_id:id(2),workflow_id:id(3),state:'queued'},body:{schema_version:1,snapshot_id:id(4),params:{schema_version:1,base:{asset_version_id:id(5),source:{artifact_id:id(6),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:.8,exposure_ev:-1,film_base_rgb:[1,.8,.6],calibration:{artifact_id:id(7),sha256:'c'.repeat(64)}}}});
it.each(['success','unresolved','workflow','source','canvas','calibration','snapshot','cancel'])('preserves old receipt until next input is ready: %s',async fault=>{
 const r=record(),calls:string[]=[],controller=new AbortController();if(fault==='unresolved')r.ticket=null;
 const api={get:async()=>{calls.push('workflow');return {run_id:fault==='workflow'?id(99):id(2),workflow_id:id(3)}},v2Binding:async()=>{calls.push('source');const base=structuredClone(r.body.params.base);if(fault==='source')base.source.sha256='d'.repeat(64);if(fault==='canvas')base.canvas_hash='d'.repeat(64);return base},negativeCapabilities:async()=>{calls.push('calibration');if(fault==='calibration')throw new Error('unavailable');return {calibration:{artifact_id:id(8),sha256:'d'.repeat(64)}}},createRenderInputSnapshot:async()=>{calls.push('snapshot');if(fault==='snapshot')throw new Error('unavailable');if(fault==='cancel')controller.abort();return {id:id(9)}}} as unknown as Pick<OwnerAPI,'get'|'v2Binding'|'negativeCapabilities'|'createRenderInputSnapshot'>;
 const store={clear:async(key:string)=>{expect(key).toBe('old');calls.push('clear')}};
 if(fault==='success'){const next=await prepareNextNegative(api,id(10),r,store,controller.signal);expect(next.snapshot_id).toBe(id(9));expect(next.params.calibration.artifact_id).toBe(id(8));expect(next.params.film_base_rgb).toEqual([1,.8,.6]);expect(r.body.snapshot_id).toBe(id(4));expect(calls).toEqual(['workflow','source','calibration','snapshot','clear'])}
 else {await expect(prepareNextNegative(api,id(10),r,store,controller.signal)).rejects.toThrow();expect(calls).not.toContain('clear')}
});
