import {it,expect,vi,afterEach} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {watchEditorSourcePreview} from './editor-source-preview-poll';
afterEach(()=>vi.useRealTimers());
const link=(url:string,ttl=60000)=>({url,expires_at:new Date(Date.now()+ttl).toISOString()});
it('waits for durable media readiness and stops after installing preview',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockRejectedValueOnce(new APIError(409,'MEDIA_NOT_READY')).mockResolvedValueOnce(link('https://example.test/preview')),ready=vi.fn(),failed=vi.fn();
 const stop=watchEditorSourcePreview({read,ready,failed});await vi.advanceTimersByTimeAsync(2000);
 expect(ready).toHaveBeenCalledWith('https://example.test/preview');await vi.advanceTimersByTimeAsync(20000);expect(read).toHaveBeenCalledTimes(2);expect(failed).not.toHaveBeenCalled();stop();
});
it('does not poll authorization denial or unrelated conflict',async()=>{
 vi.useFakeTimers();for(const error of [new APIError(403,'DENIED'),new APIError(409,'REVISION_CONFLICT')]){
 const read=vi.fn().mockRejectedValue(error),ready=vi.fn(),failed=vi.fn();watchEditorSourcePreview({read,ready,failed});await vi.advanceTimersByTimeAsync(20000);expect(read).toHaveBeenCalledOnce();expect(failed).toHaveBeenCalledOnce();expect(ready).toHaveBeenCalledWith('');}
});
it('aborts and ignores a late result from a stopped version scope',async()=>{
 let resolve!:(url:ReturnType<typeof link>)=>void;const read=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done})),ready=vi.fn(),failed=vi.fn();
 const stop=watchEditorSourcePreview({read,ready,failed});const signal=read.mock.calls[0][0] as AbortSignal;stop();resolve(link('https://example.test/old'));await Promise.resolve();expect(signal.aborted).toBe(true);expect(ready).not.toHaveBeenCalled();expect(failed).not.toHaveBeenCalled();
});

it('aborts a hung transport once and fences a late successful reply',async()=>{
 vi.useFakeTimers();let resolve!:(url:ReturnType<typeof link>)=>void;
 const read=vi.fn().mockImplementation(()=>new Promise<ReturnType<typeof link>>(done=>{resolve=done})),ready=vi.fn(),failed=vi.fn();
 const stop=watchEditorSourcePreview({read,ready,failed});const signal=read.mock.calls[0][0] as AbortSignal;
 await vi.advanceTimersByTimeAsync(7999);expect(failed).not.toHaveBeenCalled();
 await vi.advanceTimersByTimeAsync(1);expect(signal.aborted).toBe(true);expect(failed).toHaveBeenCalledOnce();
 resolve(link('https://example.test/late'));await vi.advanceTimersByTimeAsync(20000);
 expect(ready.mock.calls).toEqual([['']]);expect(read).toHaveBeenCalledOnce();expect(failed).toHaveBeenCalledOnce();stop();
});
it('clears the request deadline when its version scope is stopped',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockImplementation(()=>new Promise<ReturnType<typeof link>>(()=>{})),ready=vi.fn(),failed=vi.fn();
 const stop=watchEditorSourcePreview({read,ready,failed});stop();await vi.advanceTimersByTimeAsync(20000);
 expect(failed).not.toHaveBeenCalled();expect(ready).not.toHaveBeenCalled();expect(read).toHaveBeenCalledOnce();
});

it('clears expired media and refreshes only within the active scope',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockResolvedValueOnce(link('https://example.test/first',1000)).mockImplementationOnce(async()=>link('https://example.test/second'));
 const ready=vi.fn(),failed=vi.fn(),stop=watchEditorSourcePreview({read,ready,failed});
 await vi.advanceTimersByTimeAsync(1000);
 expect(ready.mock.calls.map(call=>call[0])).toEqual(['https://example.test/first','','https://example.test/second']);
 stop();await vi.advanceTimersByTimeAsync(120000);expect(read).toHaveBeenCalledTimes(2);expect(failed).not.toHaveBeenCalled();
});
it('never installs a malformed or expired signed link',async()=>{
 for(const expires_at of ['invalid',new Date(Date.now()-1).toISOString()]){
 const read=vi.fn().mockResolvedValue({url:'https://example.test/expired',expires_at}),ready=vi.fn(),failed=vi.fn();
 watchEditorSourcePreview({read,ready,failed});await Promise.resolve();await Promise.resolve();
 expect(ready.mock.calls).toEqual([['']]);expect(failed).toHaveBeenCalledOnce();
 }
});

it('recovers a transient signer outage without retaining the old bearer',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockResolvedValueOnce(link('https://example.test/first',1000)).mockRejectedValueOnce(new APIError(503,'DEPENDENCY_UNAVAILABLE')).mockImplementationOnce(async()=>link('https://example.test/recovered'));
 const ready=vi.fn(),failed=vi.fn(),stop=watchEditorSourcePreview({read,ready,failed});
 await vi.advanceTimersByTimeAsync(3000);
 expect(ready.mock.calls.map(call=>call[0])).toEqual(['https://example.test/first','','','https://example.test/recovered']);expect(failed).not.toHaveBeenCalled();stop();
});
it('bounds temporary server failures and cancels a scheduled recovery on stop',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockRejectedValue(new APIError(503,'DEPENDENCY_UNAVAILABLE')),ready=vi.fn(),failed=vi.fn();
 const stop=watchEditorSourcePreview({read,ready,failed});await vi.advanceTimersByTimeAsync(60000);
 expect(read).toHaveBeenCalledTimes(5);expect(failed).toHaveBeenCalledOnce();stop();
 const other=vi.fn().mockRejectedValue(new APIError(502,'BAD_GATEWAY')),cancel=watchEditorSourcePreview({read:other,ready:vi.fn(),failed:vi.fn()});
 await Promise.resolve();cancel();await vi.advanceTimersByTimeAsync(60000);expect(other).toHaveBeenCalledOnce();
});
