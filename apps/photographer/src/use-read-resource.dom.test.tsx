// @vitest-environment jsdom
import {act,cleanup,renderHook} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const native=vi.hoisted(()=>({listener:null as null|((value:string)=>void)}));
vi.mock('react-native',()=>({AppState:{currentState:'active',addEventListener:(_name:string,listener:(value:string)=>void)=>{native.listener=listener;return{remove:()=>{native.listener=null;}};}}}));
import {useReadResource} from './use-read-resource';
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return{promise,resolve};}
afterEach(()=>{cleanup();vi.useRealTimers();vi.clearAllMocks();});
it('cancels an in-flight read and explicitly retries without accepting its late response',async()=>{
 const old=deferred<string>(),fresh=deferred<string>();const signals:AbortSignal[]=[];
 const read=vi.fn((signal:AbortSignal)=>{signals.push(signal);return signals.length===1?old.promise:fresh.promise;});
 const {result}=renderHook(()=>useReadResource('tenant-a',read,read));
 act(()=>result.current.cancel());expect(signals[0].aborted).toBe(true);expect(result.current.phase).toBe('cancelled');
 await act(async()=>{old.resolve('stale');});expect(result.current.data).toBeUndefined();
 act(()=>result.current.retry());await act(async()=>{fresh.resolve('current');});expect(result.current.data).toBe('current');
});
it('bounds a noncooperative read and allows a fresh attempt after timeout',async()=>{
 vi.useFakeTimers();const first=deferred<string>(),next=deferred<string>();const read=vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(next.promise);
 const {result}=renderHook(()=>useReadResource('scope',read,read));
 act(()=>vi.advanceTimersByTime(8000));expect(result.current.phase).toBe('error');
 act(()=>result.current.retry());await act(async()=>{first.resolve('late');next.resolve('fresh');});expect(result.current.data).toBe('fresh');
});
it('clears old ownership immediately and refuses late responses and mutation commits after scope/client change',async()=>{
 const first=deferred<string>(),next=deferred<string>();const connectionA={},connectionB={};
 const readA=vi.fn(()=>first.promise),readB=vi.fn(()=>next.promise);
 const {result,rerender}=renderHook(({scope,connection,read})=>useReadResource(scope,connection,read),{initialProps:{scope:'a',connection:connectionA,read:readA}});
 const prior=result.current;rerender({scope:'b',connection:connectionB,read:readB});
 act(()=>prior.commit('cross-tenant'));await act(async()=>{first.resolve('late');});expect(result.current.data).toBeUndefined();
 await act(async()=>{next.resolve('b');});expect(result.current.data).toBe('b');expect(prior.current()).toBe(false);
});
it('hides data in background and requires new authority after returning to foreground',async()=>{
 const fresh=deferred<string>();const read=vi.fn().mockResolvedValueOnce('visible').mockReturnValueOnce(fresh.promise);
 const {result}=renderHook(()=>useReadResource('scope',read,read));await act(async()=>{});expect(result.current.data).toBe('visible');
 act(()=>native.listener?.('background'));expect(result.current.phase).toBe('paused');expect(result.current.data).toBeUndefined();
 act(()=>native.listener?.('active'));expect(result.current.phase).toBe('loading');await act(async()=>{fresh.resolve('renewed');});expect(result.current.data).toBe('renewed');
});
it('does not let retained old cancel/retry callbacks interrupt a new scope',async()=>{
 const old=deferred<string>(),next=deferred<string>(),signals:AbortSignal[]=[];const connection={};
 const read=vi.fn((signal:AbortSignal)=>{signals.push(signal);return signals.length===1?old.promise:next.promise;});
 const{result,rerender}=renderHook(({scope})=>useReadResource(scope,connection,read),{initialProps:{scope:'a'}});const previous=result.current;
 rerender({scope:'b'});act(()=>{previous.cancel();previous.retry();});expect(signals[1].aborted).toBe(false);expect(read).toHaveBeenCalledTimes(2);
 await act(async()=>{next.resolve('b');});expect(result.current.data).toBe('b');
});
it('revokes retained mutation callbacks when the resource owner unmounts',()=>{
 const read=vi.fn(()=>new Promise<string>(()=>{}));const{result,unmount}=renderHook(()=>useReadResource('scope',read,read));const retained=result.current;unmount();expect(retained.current()).toBe(false);retained.commit('late');retained.retry();expect(read).toHaveBeenCalledOnce();
});

it.each(['cancel','retry'] as const)('revokes retained same-scope mutation callbacks synchronously on %s',async action=>{
 const pending=deferred<string>(),read=vi.fn().mockResolvedValueOnce('old').mockReturnValue(pending.promise);
 const {result}=renderHook(()=>useReadResource('same',read,read));await act(async()=>{});
 const prior=result.current;expect(prior.current()).toBe(true);
 act(()=>{prior[action]();expect(prior.current()).toBe(false);prior.commit('late mutation');});
 expect(result.current.data).toBeUndefined();expect(result.current.phase).toBe(action==='cancel'?'cancelled':'loading');
 act(()=>{prior.cancel();prior.retry();});
 expect(read).toHaveBeenCalledTimes(action==='cancel'?1:2);
 if(action==='retry'){await act(async()=>{pending.resolve('new');});expect(result.current.data).toBe('new');}
});
it('timeout revokes retained mutation authority before a late external commit',()=>{
 vi.useFakeTimers();const read=vi.fn(()=>new Promise<string>(()=>{}));
 const{result}=renderHook(()=>useReadResource('same',read,read));const retained=result.current;
 act(()=>{vi.advanceTimersByTime(8000);expect(retained.current()).toBe(false);retained.commit('late');});
 expect(result.current.phase).toBe('error');expect(result.current.data).toBeUndefined();
});

it('read failure revokes retained mutation callbacks in the same scope',async()=>{
 let reject!:(cause:unknown)=>void;
 const read=vi.fn(()=>new Promise<string>((_resolve,fail)=>{reject=fail}));
 const{result}=renderHook(()=>useReadResource('same',read,read));const retained=result.current;
 await act(async()=>{reject(new Error('authority unavailable'));});
 expect(result.current.phase).toBe('error');expect(retained.current()).toBe(false);
 act(()=>{retained.commit('stale authority');retained.retry();retained.cancel();});
 expect(result.current.phase).toBe('error');expect(result.current.data).toBeUndefined();expect(read).toHaveBeenCalledOnce();
});
