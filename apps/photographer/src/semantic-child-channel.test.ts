import type {SemanticChildStatus} from '@photoeditor/api-client';
import {afterEach,it,expect,vi} from 'vitest';
import {subscribeSemanticChild,retrySemanticChild} from './semantic-child-channel';
const status={id:'run',kind:'apply' as const,status:'running' as const,revision:'1',counts:{total:1,succeeded:0,failed:0,skipped:0,review:0},stage:'running' as const,progress:0,errors:[]};
const tick=async()=>{await Promise.resolve();await Promise.resolve();};
afterEach(()=>vi.useRealTimers());
it('shares pending IO, owns subscriber values and cancels only after the last subscriber',async()=>{let finish!:(value:typeof status)=>void;const client={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))},a=vi.fn(),b=vi.fn();const one=subscribeSemanticChild(client,'tenant','run',a,()=>()=>{}),two=subscribeSemanticChild(client,'tenant','run',b,()=>()=>{});expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);one();expect((client.semanticChildStatus.mock.calls[0][2] as AbortSignal).aborted).toBe(false);finish(status);await tick();expect(a).toHaveBeenCalledTimes(1);expect(b.mock.lastCall?.[0].status).toEqual(status);b.mock.lastCall![0].status.counts.total=9;expect(status.counts.total).toBe(1);two();expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);});
it('polls once per interval and notifies a shared terminal callback once',async()=>{vi.useFakeTimers();const client={semanticChildStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,status:'completed',stage:'completed',progress:1})},done=vi.fn();const one=subscribeSemanticChild(client,'tenant','run',vi.fn(),()=>done),two=subscribeSemanticChild(client,'tenant','run',vi.fn(),()=>done);await tick();expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);await vi.advanceTimersByTimeAsync(3000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);expect(done).toHaveBeenCalledTimes(1);await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);one();two();});
it('isolates authenticated clients, tenants and runs',()=>{const client={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise(()=>{}))},other={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise(()=>{}))};const unsub=[subscribeSemanticChild(client,'a','run',vi.fn(),()=>()=>{}),subscribeSemanticChild(client,'b','run',vi.fn(),()=>()=>{}),subscribeSemanticChild(client,'a','other',vi.fn(),()=>()=>{}),subscribeSemanticChild(other,'a','run',vi.fn(),()=>()=>{})];expect(client.semanticChildStatus).toHaveBeenCalledTimes(3);expect(other.semanticChildStatus).toHaveBeenCalledTimes(1);unsub.forEach(fn=>fn());});
it('starts fresh after all subscribers dispose and suppresses a late old response',async()=>{let finish!:(value:typeof status)=>void;const client={semanticChildStatus:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce(status)},notify=vi.fn();subscribeSemanticChild(client,'tenant','run',notify,()=>()=>{})();const stop=subscribeSemanticChild(client,'tenant','run',vi.fn(),()=>()=>{});finish(status);await tick();expect(notify).toHaveBeenCalledTimes(1);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);stop();});
it.each([401,403,404,500])('clears shared status on HTTP %s and requires a fresh subscription scope to retry',async(code)=>{vi.useFakeTimers();const client={semanticChildStatus:vi.fn().mockResolvedValueOnce(status).mockRejectedValueOnce({status:code}).mockResolvedValueOnce(status)},a=vi.fn(),b=vi.fn(),done=vi.fn();const one=subscribeSemanticChild(client,'tenant','run',a,()=>done),two=subscribeSemanticChild(client,'tenant','run',b,()=>done);await tick();expect(a.mock.lastCall?.[0].status).toEqual(status);await vi.advanceTimersByTimeAsync(3000);expect(a.mock.lastCall?.[0]).toEqual({status:null,failed:true});expect(b.mock.lastCall?.[0]).toEqual({status:null,failed:true});expect(done).not.toHaveBeenCalled();await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);one();two();const fresh=vi.fn(),stop=subscribeSemanticChild(client,'tenant','run',fresh,()=>done);await tick();expect(client.semanticChildStatus).toHaveBeenCalledTimes(3);expect(fresh.mock.lastCall?.[0]).toEqual({status,failed:false});stop();});
it('last unsubscribe clears the scheduled timer and ignores late rejection',async()=>{vi.useFakeTimers();const client={semanticChildStatus:vi.fn().mockResolvedValue(status)},notify=vi.fn();const stop=subscribeSemanticChild(client,'tenant','run',notify,()=>()=>{});await tick();expect(vi.getTimerCount()).toBe(1);stop();expect(vi.getTimerCount()).toBe(0);await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);let reject!:(reason:Error)=>void;const pending={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise((_resolve,fail)=>{reject=fail;}))},listener=vi.fn();subscribeSemanticChild(pending,'tenant','run',listener,()=>()=>{})();reject(new Error('late network failure'));await tick();expect(listener).toHaveBeenCalledTimes(1);expect(vi.getTimerCount()).toBe(0);});

it.each([401,403,404,500,'offline'])('explicitly retries a shared failed channel once after %s',async(code)=>{
 vi.useFakeTimers();const failure=code==='offline'?new Error('offline'):{status:code};
 const client={semanticChildStatus:vi.fn().mockRejectedValueOnce(failure).mockResolvedValueOnce({...status,status:'completed',stage:'completed',progress:1})},a=vi.fn(),b=vi.fn(),done=vi.fn();
 const one=subscribeSemanticChild(client,'tenant','run',a,()=>done),two=subscribeSemanticChild(client,'tenant','run',b,()=>done);
 await tick();expect(a.mock.lastCall?.[0]).toEqual({status:null,failed:true});
 await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(1);
 const oldSignal=client.semanticChildStatus.mock.calls[0][2] as AbortSignal;
 expect(retrySemanticChild(client,'tenant','run')).toBe(true);expect(retrySemanticChild(client,'tenant','run')).toBe(false);
 // The failed read has settled and detached its deadline signal. The replacement must own a new signal.
 expect(client.semanticChildStatus.mock.calls[1][2]).not.toBe(oldSignal);
 await tick();expect(a.mock.lastCall?.[0].status.status).toBe('completed');expect(b.mock.lastCall?.[0].failed).toBe(false);expect(done).toHaveBeenCalledOnce();
 await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);
 one();two();expect(retrySemanticChild(client,'tenant','run')).toBe(false);expect(vi.getTimerCount()).toBe(0);
});
it('does not restart a pending or foreign scope through retry',async()=>{
 const client={semanticChildStatus:vi.fn().mockImplementation(()=>new Promise(()=>{}))};const stop=subscribeSemanticChild(client,'a','run',vi.fn(),()=>()=>{});
 expect(retrySemanticChild(client,'a','run')).toBe(false);expect(retrySemanticChild(client,'b','run')).toBe(false);expect(client.semanticChildStatus).toHaveBeenCalledOnce();stop();
});

it('keeps repeated denied reads stopped until another explicit action',async()=>{
 vi.useFakeTimers();const client={semanticChildStatus:vi.fn().mockRejectedValue({status:403})},notify=vi.fn(),done=vi.fn();const stop=subscribeSemanticChild(client,'tenant','run',notify,()=>done);
 await tick();expect(retrySemanticChild(client,'tenant','run')).toBe(true);await tick();
 expect(notify.mock.lastCall?.[0]).toEqual({status:null,failed:true});
 await vi.advanceTimersByTimeAsync(30000);expect(client.semanticChildStatus).toHaveBeenCalledTimes(2);expect(done).not.toHaveBeenCalled();stop();expect(vi.getTimerCount()).toBe(0);
});
it('aborts a pending manual retry and ignores its late terminal response after dispose',async()=>{
 vi.useFakeTimers();let finish!:(value:SemanticChildStatus)=>void;const client={semanticChildStatus:vi.fn().mockRejectedValueOnce(new Error('offline')).mockImplementationOnce(()=>new Promise<SemanticChildStatus>(resolve=>{finish=resolve;}))},notify=vi.fn(),done=vi.fn();
 const stop=subscribeSemanticChild(client,'tenant','run',notify,()=>done);await tick();expect(retrySemanticChild(client,'tenant','run')).toBe(true);
 const count=notify.mock.calls.length,signal=client.semanticChildStatus.mock.calls[1][2] as AbortSignal;stop();expect(signal.aborted).toBe(true);
 finish({...status,status:'completed',stage:'completed',progress:1});await tick();expect(notify).toHaveBeenCalledTimes(count);expect(done).not.toHaveBeenCalled();expect(vi.getTimerCount()).toBe(0);
});

it('stalled child polling becomes retryable and late old terminal cannot notify subscribers',async()=>{
 vi.useFakeTimers();let finish!:(value:SemanticChildStatus)=>void;
 const client={semanticChildStatus:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue(status)},notify=vi.fn(),done=vi.fn();const stop=subscribeSemanticChild(client,'tenant','bounded-run',notify,()=>done);
 await vi.advanceTimersByTimeAsync(8001);expect(notify.mock.lastCall?.[0]).toEqual({status:null,failed:true});expect(retrySemanticChild(client,'tenant','bounded-run')).toBe(true);await tick();
 finish({...status,status:'completed',stage:'completed',progress:1});await tick();expect(done).not.toHaveBeenCalled();expect(notify.mock.lastCall?.[0].status?.status).toBe('running');stop();
});
