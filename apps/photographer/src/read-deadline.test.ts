import {afterEach,expect,it,vi} from 'vitest';
import {readDeadline} from './read-deadline';
afterEach(()=>vi.useRealTimers());
it('rejects noncooperative reads on timeout and aborts only the child transport',async()=>{
 vi.useFakeTimers();const parent=new AbortController();let child!:AbortSignal;let resolve!:(value:string)=>void;
 const promise=readDeadline(parent.signal,signal=>{child=signal;return new Promise<string>(done=>{resolve=done;});});
 const rejection=expect(promise).rejects.toMatchObject({name:'TimeoutError'});
 vi.advanceTimersByTime(8000);await rejection;expect(child.aborted).toBe(true);expect(parent.signal.aborted).toBe(false);resolve('late');
});
it('forwards parent cancellation and does not start an already cancelled read',async()=>{
 const parent=new AbortController();parent.abort();const read=vi.fn(async()=>1);
 await expect(readDeadline(parent.signal,read)).rejects.toMatchObject({name:'AbortError'});expect(read).not.toHaveBeenCalled();
});
it('settles a successful read without leaving timers or abort listeners',async()=>{
 vi.useFakeTimers();const parent=new AbortController(),remove=vi.spyOn(parent.signal,'removeEventListener');
 await expect(readDeadline(parent.signal,async()=>42)).resolves.toBe(42);
 expect(vi.getTimerCount()).toBe(0);expect(remove).toHaveBeenCalledWith('abort',expect.any(Function));
});
it('does not reinterpret a thrown server refusal as success',async()=>{
 const error=Object.assign(new Error('Denied'),{status:403});await expect(readDeadline(undefined,()=>{throw error;})).rejects.toBe(error);
});
