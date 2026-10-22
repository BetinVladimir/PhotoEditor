import {expect,it} from 'vitest';
import {ApiClient} from './index';

it('calls browser transports without binding the API client as receiver on every transport path',async()=>{
 const seen:unknown[]=[];
 const transport:typeof fetch=async function(this:unknown,input){
  seen.push(this);if(this!==undefined)throw new TypeError('Illegal invocation');
  const path=String(input);
  if(path.endsWith('/bytes'))return new Response(new Uint8Array([1]),{headers:{'Content-Type':'image/png'}});
  if(path.endsWith('/events'))return new Response('',{headers:{'Content-Type':'text/event-stream'}});
  return new Response('{"ok":true}',{headers:{'Content-Type':'application/json'}});
 };
 const client=new ApiClient('http://localhost:58080',{principal:()=>({kind:'owner',accessToken:'test'}),clear:()=>{},refresh:async()=>''},transport);
 await expect(client.request('/api/v1/me')).resolves.toEqual({ok:true});
 await expect(client.requestBytes('/api/v1/bytes','image/png',8)).resolves.toEqual(new Uint8Array([1]));
 await client.eventStream('/api/v1/events',undefined,()=>{},new AbortController().signal);
 expect(seen).toEqual([undefined,undefined,undefined]);
});
