import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it('actual OwnerAPI posts full supported layout and validates closed immutable acceptance',async()=>{
 const request=vi.fn(async(..._args:unknown[])=>({run_id:id(3),status:'queued'})),api=new OwnerAPI({request} as never,id(1)),signal=new AbortController().signal;
 expect(await api.contactSheet(id(2),'bg',8,10,signal,id(4))).toEqual({run_id:id(3),status:'queued'});
 expect(request.mock.calls[0][0]).toContain(`/tenants/${id(1)}/contact-sheets`);expect(request.mock.calls[0][1]).toMatchObject({method:'POST',body:{selection_snapshot_id:id(2),locale:'bg',columns:8,rows:10},idempotencyKey:id(4),signal});
 request.mockResolvedValueOnce({run_id:id(3),status:'queued',extra:true} as never);await expect(api.contactSheet(id(2),'en',4,5)).rejects.toThrow('RECEIPT_INVALID');
});
it('rejects invalid request or preaborted action before transport and ignores aborted reply',async()=>{
 const request=vi.fn(async(..._args:unknown[])=>({run_id:id(3),status:'queued'})),api=new OwnerAPI({request} as never,id(1));await expect(api.contactSheet(id(2),'en',9,5)).rejects.toThrow('REQUEST_INVALID');expect(request).not.toHaveBeenCalled();
 const controller=new AbortController();controller.abort();await expect(api.contactSheet(id(2),'en',4,5,controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const live=new AbortController();request.mockImplementationOnce(async()=>{live.abort();return {run_id:id(3),status:'queued'}});await expect(api.contactSheet(id(2),'en',4,5,live.signal)).rejects.toThrow();
});
