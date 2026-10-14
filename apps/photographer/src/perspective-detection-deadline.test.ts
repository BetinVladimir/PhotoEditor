import {it,expect,vi} from 'vitest';
import {withPerspectiveDetectionDeadline} from './perspective-detection-deadline';
it('times out an uncooperative request and aborts its transport',async()=>{
 vi.useFakeTimers();try{
 let child:AbortSignal|undefined;
 const pending=withPerspectiveDetectionDeadline(new AbortController().signal,100,async signal=>{child=signal;return await new Promise<never>(()=>{});});
 const failed=expect(pending).rejects.toThrow('PERSPECTIVE_DETECTION_TIMEOUT');
 await vi.advanceTimersByTimeAsync(100);await failed;expect(child?.aborted).toBe(true);expect(vi.getTimerCount()).toBe(0);
 }finally{vi.useRealTimers();}
});
it('propagates cancellation and cleans up timers on success and failure',async()=>{
 vi.useFakeTimers();try{
 const c=new AbortController();const pending=withPerspectiveDetectionDeadline(c.signal,100,async()=>await new Promise<never>(()=>{}));
 const failed=expect(pending).rejects.toThrow('PERSPECTIVE_DETECTION_CANCELLED');c.abort();await failed;expect(vi.getTimerCount()).toBe(0);
 expect(await withPerspectiveDetectionDeadline(new AbortController().signal,100,async()=>42)).toBe(42);expect(vi.getTimerCount()).toBe(0);
 await expect(withPerspectiveDetectionDeadline(new AbortController().signal,100,async()=>{throw new Error('transport');})).rejects.toThrow('transport');expect(vi.getTimerCount()).toBe(0);
 }finally{vi.useRealTimers();}
});
