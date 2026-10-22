import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {cancelEditorRender} from './editor-render-cancel';
const parent='11111111-1111-4111-8111-111111111111',child='22222222-2222-4222-8222-222222222222';
const ticket=()=>({run_id:parent,state:'queued' as const,child_run_ids:[child]});
const status=()=>({id:parent,kind:'bulk',status:'cancelling',revision:'2',counts:{total:1,succeeded:0,failed:0,skipped:0,review:0},stage:'cancelling',progress:0,errors:[]});
it('calls tenant-scoped cancellation and retains the server state',async()=>{
 const request=vi.fn((..._args:unknown[])=>Promise.resolve(status())),api=new OwnerAPI({request} as never,parent);
 const signal=new AbortController().signal;
 const response=await cancelEditorRender(api,ticket(),signal);
 expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${parent}/runs/${parent}/cancel`,expect.objectContaining({method:'POST',signal,idempotencyKey:expect.any(String)})]);
 expect(response.status).toBe('cancelling');
});
it('rejects a response for another run',async()=>{
 await expect(cancelEditorRender({cancelRun:async()=>({...status(),id:child})},ticket(),new AbortController().signal)).rejects.toThrow('RENDER_STATUS_INVALID');
});
it('ignores a late response after scope abort',async()=>{
 let resolve!:(v:ReturnType<typeof status>)=>void;
 const controller=new AbortController(),cancelRun=vi.fn(()=>new Promise<ReturnType<typeof status>>(r=>{resolve=r;}));
 const pending=cancelEditorRender({cancelRun},ticket(),controller.signal),rejected=expect(pending).rejects.toThrow();
 controller.abort();resolve(status());await rejected;expect(cancelRun).toHaveBeenCalledTimes(1);
});
