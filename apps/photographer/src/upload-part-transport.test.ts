import {afterEach,expect,it,vi} from 'vitest';
import {putUploadPart} from './upload-part-transport';
afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals()});
it('omits credentials and rejects redirects without application headers',async()=>{
 const fetcher=vi.fn(async()=>new Response(null,{status:200,headers:{ETag:'"etag"'}}));vi.stubGlobal('fetch',fetcher);
 expect(await putUploadPart('https://storage.test/part',new Uint8Array(3),new AbortController().signal)).toBe('etag');
 expect(fetcher.mock.calls[0]).toEqual(['https://storage.test/part',expect.objectContaining({method:'PUT',credentials:'omit',redirect:'error',referrerPolicy:'no-referrer'})]);
});
it('times out stalled PUT and aborts the transport signal',async()=>{
 vi.useFakeTimers();let transport:AbortSignal|undefined;
 vi.stubGlobal('fetch',vi.fn((_url:string,options:RequestInit)=>new Promise((_resolve,reject)=>{transport=options.signal as AbortSignal;transport.addEventListener('abort',()=>reject(transport?.reason),{once:true})})));
 const pending=putUploadPart('https://storage.test/part',new Uint8Array(3),new AbortController().signal);const failure=expect(pending).rejects.toThrow('PART_UPLOAD_TIMEOUT');
 await vi.advanceTimersByTimeAsync(120000);await failure;expect(transport?.aborted).toBe(true);expect(vi.getTimerCount()).toBe(0);
});
it('discards a late successful response after caller cancellation',async()=>{
 let release!:(r:Response)=>void;vi.stubGlobal('fetch',vi.fn(()=>new Promise<Response>(r=>{release=r})));
 const c=new AbortController();const pending=putUploadPart('https://storage.test/part',new Uint8Array(3),c.signal);const failure=expect(pending).rejects.toThrow('paused');c.abort(new Error('paused'));release(new Response(null,{headers:{ETag:'etag'}}));await failure;
});
it.each([0,16777217])('refuses invalid part bytes %s before fetch',async size=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher);await expect(putUploadPart('https://storage.test/part',new Uint8Array(size),new AbortController().signal)).rejects.toThrow('PART_SIZE_INVALID');expect(fetcher).not.toHaveBeenCalled();
});
