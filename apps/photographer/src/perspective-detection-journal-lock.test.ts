import {it,expect} from 'vitest';
import {withDetectionJournalLock} from './perspective-detection-journal-lock';
it('serializes the same journal key and releases after failure',async()=>{
 const journal={};const signal=new AbortController().signal;const order:string[]=[];let finish!:()=>void;let markStarted!:()=>void;const started=new Promise<void>(resolve=>{markStarted=resolve;});
 const first=withDetectionJournalLock(journal,'key',signal,async()=>{order.push('first');markStarted();await new Promise<void>(resolve=>{finish=resolve;});throw new Error('first failed');});
 const failed=expect(first).rejects.toThrow('first failed');
 const second=withDetectionJournalLock(journal,'key',signal,async()=>{order.push('second');return 2;});
 await started;expect(order).toEqual(['first']);finish();await failed;expect(await second).toBe(2);expect(order).toEqual(['first','second']);
});
it('does not execute cancelled queued writes or block unrelated keys',async()=>{
 const journal={};let finish!:()=>void;const c=new AbortController();let called=false;
 const first=withDetectionJournalLock(journal,'a',new AbortController().signal,async()=>await new Promise<void>(resolve=>{finish=resolve;}));
 const queued=withDetectionJournalLock(journal,'a',c.signal,async()=>{called=true;});const cancelled=expect(queued).rejects.toThrow();
 expect(await withDetectionJournalLock(journal,'b',new AbortController().signal,async()=>3)).toBe(3);
 c.abort();finish();await first;await cancelled;expect(called).toBe(false);
});
it('settles queued cancellation before predecessor and keeps third writer ordered',async()=>{
 const journal={};let finish!:()=>void;let firstStarted!:()=>void;
 const started=new Promise<void>(resolve=>{firstStarted=resolve;});const c=new AbortController();const order:string[]=[];
 const first=withDetectionJournalLock(journal,'k',new AbortController().signal,async()=>{order.push('first');firstStarted();await new Promise<void>(resolve=>{finish=resolve;});});
 await started;
 const second=withDetectionJournalLock(journal,'k',c.signal,async()=>{order.push('cancelled');});const failure=expect(second).rejects.toThrow();
 c.abort();await failure;
 const third=withDetectionJournalLock(journal,'k',new AbortController().signal,async()=>{order.push('third');});
 await Promise.resolve();expect(order).toEqual(['first']);finish();await first;await third;expect(order).toEqual(['first','third']);
});
it('uses the supplied browser exclusive lock and forwards cancellation',async()=>{
 const c=new AbortController();const seen:string[]=[];
 const manager={request:async<T>(name:string,options:{mode:'exclusive';signal:AbortSignal},callback:()=>Promise<T>)=>{seen.push(name);expect(options.mode).toBe('exclusive');expect(options.signal).toBe(c.signal);return callback();}};
 expect(await withDetectionJournalLock({},'scoped-key',c.signal,async()=>7,manager)).toBe(7);expect(seen).toEqual(['photoeditor:scoped-key']);
});
