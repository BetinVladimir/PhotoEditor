import {it,expect} from 'vitest';
import {CoalescedRefresh} from './coalesced-refresh';
it('coalesces a burst into one trailing refresh',async()=>{
 let release!:()=>void;let calls=0;
 const queue=new CoalescedRefresh(async()=>{calls++;if(calls===1)await new Promise<void>(resolve=>{release=resolve})},new AbortController().signal);
 const first=queue.request();await queue.request();await queue.request();await queue.request();
 expect(calls).toBe(1);release();await first;expect(calls).toBe(2);
});
it('drops queued work after cancellation',async()=>{
 let release!:()=>void;let calls=0;const controller=new AbortController();
 const queue=new CoalescedRefresh(async()=>{calls++;await new Promise<void>(resolve=>{release=resolve})},controller.signal);
 const first=queue.request();await queue.request();controller.abort();release();await first;await queue.request();expect(calls).toBe(1);
});
