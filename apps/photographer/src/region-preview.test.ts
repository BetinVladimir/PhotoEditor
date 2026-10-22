import {describe,it,expect,vi} from 'vitest';
import {freezeRegionRequest,regionJobTicket,type RegionPreviewRequest} from './region-preview';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id='12345678-1234-1234-1234-123456789abc';
const tenant='11111111-1111-4111-8111-111111111111';
const value=():RegionPreviewRequest=>({version_id:id,recipe:{artifact_id:id,sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:1024});
describe('RegionPreview client',()=>{
 it('freezes nested request before async transport and forwards cancellation/key',async()=>{
  const ticket={run_id:id,workflow_id:id,state:'queued'};
  const call=vi.fn().mockResolvedValue(ticket);
  const api=new OwnerAPI({request:call} as unknown as ApiClient,tenant);
  const body=value(),controller=new AbortController();
  const pending=api.regionPreview(id,body,'retry-key',controller.signal);
  body.rect.x1=.5;
  expect(await pending).toEqual(ticket);
  expect(call).toHaveBeenCalledWith(`/api/v1/tenants/${tenant}/assets/${id}/region-previews`,expect.objectContaining({method:'POST',idempotencyKey:'retry-key',signal:controller.signal,body:expect.objectContaining({rect:{x0:0,y0:0,x1:1,y1:1}})}));
 });
 it('rejects invalid ROI, missing fields, runtime injection and invalid ticket',()=>{
  for(const rect of [{x0:0,y0:0,x1:0,y1:1},{x0:NaN,y0:0,x1:1,y1:1},{x0:-1,y0:0,x1:1,y1:1}])expect(()=>freezeRegionRequest({...value(),rect})).toThrow();
  expect(()=>freezeRegionRequest({...value(),max_edge:2048})).toThrow();
  expect(()=>freezeRegionRequest({...value(),runtime:'cuda'} as RegionPreviewRequest)).toThrow();
  for(const ticket of [null,{run_id:id,workflow_id:id,state:'running'},{run_id:'bad',workflow_id:id,state:'queued'}])expect(()=>regionJobTicket(ticket)).toThrow();
 });
});
