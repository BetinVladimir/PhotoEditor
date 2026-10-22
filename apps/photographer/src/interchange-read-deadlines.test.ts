import {afterEach,expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const reads=[
 ['prepared',(api:OwnerAPI,signal:AbortSignal)=>api.preparedImportReviewPage(id(2),id(3),'',signal)],
 ['saved',(api:OwnerAPI,signal:AbortSignal)=>api.savedPreparedImportReviewPage(id(2),id(3),'',signal)],
 ['entries',(api:OwnerAPI,signal:AbortSignal)=>api.importPersonMaskEntries(id(2),id(3),signal)],
 ['candidates',(api:OwnerAPI,signal:AbortSignal)=>api.importPersonMaskCandidates(id(2),id(3),id(4),signal)],
 ['statuses',(api:OwnerAPI,signal:AbortSignal)=>api.importPersonMaskStatuses(id(2),id(3),signal)],
] as const;
afterEach(()=>vi.useRealTimers());
it.each(reads)('bounds %s read even when transport ignores AbortSignal without aborting its parent',async(_name,read)=>{
 vi.useFakeTimers();const request=vi.fn((_path:string,_options:{signal:AbortSignal})=>new Promise<never>(()=>{})),api=new OwnerAPI({request} as never,id(1)),controller=new AbortController();
 const pending=read(api,controller.signal),rejection=expect(pending).rejects.toThrow();await vi.advanceTimersByTimeAsync(8000);await rejection;
 expect(request).toHaveBeenCalledOnce();expect(request.mock.calls[0][1].signal.aborted).toBe(true);expect(controller.signal.aborted).toBe(false);
});
