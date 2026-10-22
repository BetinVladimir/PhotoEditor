import {expect,it,vi} from 'vitest';
import {collectionPreviewQueue} from './collection-preview-queue';
function deferred(){let resolve!:()=>void;const promise=new Promise<void>(done=>resolve=done);return {promise,resolve};}
it('bounds simultaneous reads and serves frames beyond the initial gallery window',async()=>{
 const gates=Array.from({length:30},deferred);let active=0,max=0;
 const load=vi.fn(async(item:{id:string})=>{active++;max=Math.max(max,active);await gates[Number(item.id)].promise;active--;});
 const queue=collectionPreviewQueue(new AbortController().signal,load);
 const reads=Array.from({length:30},(_,i)=>queue({id:String(i)}));
 expect(queue({id:'0'})).toBe(reads[0]);
 await Promise.resolve();expect(load).toHaveBeenCalledTimes(4);
 for(const gate of gates)gate.resolve();await Promise.all(reads);
 expect(max).toBe(4);expect(load).toHaveBeenCalledTimes(30);
});
it('does not start queued reads after scope cancellation',async()=>{
 const controller=new AbortController(),gate=deferred(),load=vi.fn(async()=>gate.promise);
 const queue=collectionPreviewQueue(controller.signal,load,1);
 const first=queue({id:'first'}),pending=queue({id:'pending'});
 await Promise.resolve();controller.abort();await pending;gate.resolve();await first;
 await queue({id:'later'});expect(load).toHaveBeenCalledTimes(1);
});
it('releases capacity after a failed read',async()=>{
 const load=vi.fn(async(item:{id:string})=>{if(item.id==='bad')throw new Error('unready');});
 const queue=collectionPreviewQueue(new AbortController().signal,load,1);
 const first=queue({id:'bad'}),second=queue({id:'next'});
 await expect(first).rejects.toThrow('unready');await second;expect(load).toHaveBeenCalledTimes(2);
});

it('retries pending previews with backoff and stops after success',async()=>{
 vi.useFakeTimers();const controller=new AbortController();
 try{
  const load=vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(false).mockResolvedValue(true);
  const queue=collectionPreviewQueue(controller.signal,load);
  const first=queue({id:'photo'});await first;expect(queue({id:'photo'})).toBe(first);
  await vi.advanceTimersByTimeAsync(999);expect(load).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1);expect(load).toHaveBeenCalledTimes(2);
  await vi.advanceTimersByTimeAsync(1999);expect(load).toHaveBeenCalledTimes(2);
  await vi.advanceTimersByTimeAsync(1);expect(load).toHaveBeenCalledTimes(3);
  await vi.advanceTimersByTimeAsync(60000);expect(load).toHaveBeenCalledTimes(3);
 }finally{controller.abort();vi.useRealTimers();}
});
it('retries only visible photos and cancels timers on abort',async()=>{
 vi.useFakeTimers();const controller=new AbortController();let visible=true;
 try{
  const load=vi.fn(async()=>false);
  const queue=collectionPreviewQueue(controller.signal,load,1,{shouldRetry:()=>visible});
  await queue({id:'photo'});visible=false;
  await vi.advanceTimersByTimeAsync(1000);expect(load).toHaveBeenCalledTimes(1);
  visible=true;await queue({id:'photo'});expect(load).toHaveBeenCalledTimes(2);
  controller.abort();await vi.advanceTimersByTimeAsync(60000);expect(load).toHaveBeenCalledTimes(2);
 }finally{controller.abort();vi.useRealTimers();}
});
it('refreshes invalidated links and allows retry after failed reads',async()=>{
 const controller=new AbortController();const load=vi.fn(async(item:{id:string})=>{if(item.id==='bad'&&load.mock.calls.length===3)throw new Error('network');});
 const queue=collectionPreviewQueue(controller.signal,load);
 await queue({id:'photo'});await Promise.resolve();queue.invalidate('photo');
 await queue({id:'photo'});await expect(queue({id:'bad'})).rejects.toThrow('network');
 await queue({id:'bad'});expect(load).toHaveBeenCalledTimes(4);controller.abort();
});
it('does not execute invalidated queued reads or duplicate active reads',async()=>{
 const controller=new AbortController(),gate=deferred();
 const load=vi.fn(async(item:{id:string})=>{if(item.id==='active')await gate.promise;});
 const queue=collectionPreviewQueue(controller.signal,load,1);
 const active=queue({id:'active'}),pending=queue({id:'pending'});await Promise.resolve();
 queue.invalidate('active');expect(queue({id:'active'})).toBe(active);queue.invalidate('pending');
 gate.resolve();await Promise.all([active,pending]);await Promise.resolve();
 expect(load).toHaveBeenCalledTimes(1);await queue({id:'pending'});expect(load).toHaveBeenCalledTimes(2);controller.abort();
});
