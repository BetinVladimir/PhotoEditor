import {describe,it,expect,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {MultiIntent,MultiOperation} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const frame=(n:number)=>({asset_version_id:id(n),source:{artifact_id:id(n+1),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)});
const body=(operation:MultiOperation):MultiIntent=>({schema_version:1,snapshot_id:id(1),params:{schema_version:1,frames:[frame(2),frame(4)],reference_ordinal:0,max_pixels:10000,calibration:{artifact_id:id(6),sha256:'c'.repeat(64)},...(operation==='optical.panorama'?{projection:'cylindrical' as const}:{motion_policy:'reject' as const})}});
describe('multi operation API',()=>{
 for(const [operation,route] of [['optical.panorama','panorama-intents'],['optical.focus_stack','focus-stack-intents']] as const){
  it(`${operation} forwards frozen request, key and cancellation`,async()=>{
   const ticket={run_id:id(7),workflow_id:id(8),state:'queued'},request=vi.fn().mockResolvedValue(ticket);
   const api=new OwnerAPI({request} as unknown as ApiClient,id(9)),controller=new AbortController(),intent=body(operation);
   const pending=api.submitMulti(operation,id(10),intent,'stable-key',controller.signal);
   intent.params.frames.reverse();expect(await pending).toEqual(ticket);
   expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id(9)}/projects/${id(10)}/${route}`,expect.objectContaining({method:'POST',idempotencyKey:'stable-key',signal:controller.signal,body:expect.objectContaining({params:expect.objectContaining({frames:[frame(2),frame(4)]})})}));
  });
 }
 it('rejects path/key injection before transport',async()=>{
  const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(9));
  await expect(api.submitMulti('optical.panorama','../foreign',body('optical.panorama'),'key')).rejects.toThrow();
  await expect(api.submitMulti('optical.focus_stack',id(10),body('optical.focus_stack'),'bad key')).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
 });
});
