import {afterEach,expect,it,vi} from 'vitest';
import {ApiClient,type AuthStore} from './index';
const auth=():AuthStore=>({principal:()=>({kind:'owner',accessToken:'token'}),clear:vi.fn(),refresh:vi.fn()});
afterEach(()=>vi.useRealTimers());

it('cancels retry backoff without sending another request or retaining the timer',async()=>{
 vi.useFakeTimers();
 const controller=new AbortController(),transport=vi.fn(async()=>{throw new Error('offline')});
 const client=new ApiClient('https://api.test',auth(),transport as typeof fetch);
 const result=client.request('/api/v1/me',{signal:controller.signal});
 const rejected=expect(result).rejects.toThrow();
 await Promise.resolve();await Promise.resolve();
 controller.abort();await rejected;
 expect(transport).toHaveBeenCalledTimes(1);expect(vi.getTimerCount()).toBe(0);
});

it('removes the backoff abort listener after a successful retry',async()=>{
 vi.useFakeTimers();
 const controller=new AbortController(),removed=vi.spyOn(controller.signal,'removeEventListener');
 const transport=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(new Response('{"ok":true}',{headers:{'Content-Type':'application/json'}}));
 const client=new ApiClient('https://api.test',auth(),transport as typeof fetch);
 const result=client.request('/api/v1/me',{signal:controller.signal});
 await vi.advanceTimersByTimeAsync(100);
 await expect(result).resolves.toEqual({ok:true});
 expect(removed).toHaveBeenCalledWith('abort',expect.any(Function));expect(vi.getTimerCount()).toBe(0);
});

it('does not send a mutation after its CSRF read was cancelled',async()=>{
 const controller=new AbortController();let finish!:(response:Response)=>void;
 const transport=vi.fn((_url:RequestInfo|URL,_options?:RequestInit)=>new Promise<Response>(resolve=>{finish=resolve}));
 const client=new ApiClient('https://api.test',auth(),transport as typeof fetch);
 const result=client.request('/api/v1/action',{method:'POST',body:{accept:true},idempotencyKey:'stable-key',signal:controller.signal});
 const rejected=expect(result).rejects.toThrow();
 expect(transport).toHaveBeenCalledTimes(1);expect(transport.mock.calls[0][0]).toBe('https://api.test/api/v1/auth/csrf');
 controller.abort();finish(new Response('{"csrf_token":"csrf"}',{headers:{'Content-Type':'application/json'}}));
 await rejected;expect(transport).toHaveBeenCalledTimes(1);
});

it('does not expose a JSON body whose decoding finishes after cancellation',async()=>{
 const controller=new AbortController();let finish!:(value:unknown)=>void,entered!:()=>void;
 const decoding=new Promise<void>(resolve=>{entered=resolve});
 const response=new Response('{}',{headers:{'Content-Type':'application/json'}});
 vi.spyOn(response,'json').mockImplementation(()=>{entered();return new Promise(resolve=>{finish=resolve})});
 const transport=vi.fn(async()=>response);
 const client=new ApiClient('https://api.test',auth(),transport as typeof fetch);
 const result=client.request('/api/v1/me',{signal:controller.signal});
 const rejected=expect(result).rejects.toThrow();await decoding;
 controller.abort();finish({secret:'late-body'});await rejected;expect(transport).toHaveBeenCalledTimes(1);
});


it('cancels a stalled binary reader and releases its lock',async()=>{
 const controller=new AbortController();let entered!:()=>void;
 const reading=new Promise<void>(resolve=>{entered=resolve}),cancel=vi.fn();
 const stream=new ReadableStream<Uint8Array>({pull(){entered()},cancel});
 const response=new Response(stream,{headers:{'Content-Type':'image/png'}});
 const client=new ApiClient('https://api.test',auth(),vi.fn(async()=>response) as typeof fetch);
 const result=client.requestBytes('/api/v1/image','image/png',1024,controller.signal);
 const rejected=expect(result).rejects.toThrow();await reading;
 controller.abort();await rejected;
 expect(cancel).toHaveBeenCalledTimes(1);expect(stream.locked).toBe(false);
});

it('does not refresh authentication after a late cancelled binary 401 response',async()=>{
 const controller=new AbortController(),store=auth();
 const response=new Response('unauthorized',{status:401});
 const client=new ApiClient('https://api.test',store,vi.fn(async()=>{controller.abort();return response}) as typeof fetch);
 await expect(client.requestBytes('/api/v1/image','image/png',1024,controller.signal)).rejects.toThrow();
 expect(store.refresh).not.toHaveBeenCalled();expect(store.clear).not.toHaveBeenCalled();expect(response.bodyUsed).toBe(true);
});
