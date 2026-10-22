import {expect,it,vi} from 'vitest';
import {ApiClient,type AuthStore} from './index';

for(const binary of [false,true]){
 it(`preserves authentication when ${binary?'binary':'JSON'} refresh is cancelled`,async()=>{
  const controller=new AbortController();
  const clear=vi.fn();
  const refresh=vi.fn(async()=>{controller.abort();throw new DOMException('Aborted','AbortError')});
  const auth:AuthStore={principal:()=>({kind:'owner',accessToken:'expired'}),clear,refresh};
  const transport=vi.fn(async()=>new Response('{"code":"AUTH_REQUIRED"}',{status:401,headers:{'Content-Type':'application/json'}}));
  const client=new ApiClient('https://api.test',auth,transport as typeof fetch);
  const result=binary?client.requestBytes('/api/v1/image','image/png',1024,controller.signal):client.request('/api/v1/me',{signal:controller.signal});
  await expect(result).rejects.toThrow();
  expect(refresh).toHaveBeenCalledTimes(1);expect(clear).not.toHaveBeenCalled();expect(transport).toHaveBeenCalledTimes(1);
 });
 it(`preserves authentication when a shared ${binary?'binary':'JSON'} refresh reports cancellation`,async()=>{
  const clear=vi.fn(),refresh=vi.fn(async()=>{throw new DOMException('Aborted','AbortError')});
  const auth:AuthStore={principal:()=>({kind:'owner',accessToken:'expired'}),clear,refresh};
  const transport=vi.fn(async()=>new Response('{"code":"AUTH_REQUIRED"}',{status:401,headers:{'Content-Type':'application/json'}}));
  const client=new ApiClient('https://api.test',auth,transport as typeof fetch);
  await expect(binary?client.requestBytes('/api/v1/image','image/png',1024):client.request('/api/v1/me')).rejects.toThrow();
  expect(clear).not.toHaveBeenCalled();
 });
}

it('still clears authentication after a non-cancellation refresh refusal',async()=>{
 const clear=vi.fn(),refresh=vi.fn(async()=>{throw new Error('refresh refused')});
 const auth:AuthStore={principal:()=>({kind:'owner',accessToken:'expired'}),clear,refresh};
 const transport=vi.fn(async()=>new Response('{"code":"AUTH_REQUIRED"}',{status:401,headers:{'Content-Type':'application/json'}}));
 const client=new ApiClient('https://api.test',auth,transport as typeof fetch);
 await expect(client.request('/api/v1/me')).rejects.toThrow('refresh refused');expect(clear).toHaveBeenCalledTimes(1);
});

it('does not send an already cancelled JSON request',async()=>{
 const controller=new AbortController();controller.abort();
 const transport=vi.fn();
 const auth:AuthStore={principal:()=>({kind:'owner',accessToken:'token'}),clear:vi.fn(),refresh:vi.fn()};
 const client=new ApiClient('https://api.test',auth,transport as typeof fetch);
 await expect(client.request('/api/v1/me',{signal:controller.signal})).rejects.toThrow();expect(transport).not.toHaveBeenCalled();
});
