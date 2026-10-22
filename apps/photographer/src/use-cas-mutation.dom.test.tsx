// @vitest-environment jsdom
import {act,cleanup,renderHook} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {useCASMutation} from './use-cas-mutation';
afterEach(()=>{cleanup();vi.useRealTimers();});
it('cancels an uncertain write, reloads once and refuses its late reply',async()=>{
 const scope={},reload=vi.fn(),accept=vi.fn();let finish!:(value:string)=>void;
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finish=resolve}));
 const hook=renderHook(()=>useCASMutation(scope,()=>true,reload));
 let pending!:Promise<void>;act(()=>{pending=hook.result.current.run(write,accept);});
 act(()=>hook.result.current.cancel());
 expect(write.mock.calls[0][0].aborted).toBe(true);expect(reload).toHaveBeenCalledOnce();
 await act(async()=>{finish('late');await pending;});
 expect(accept).not.toHaveBeenCalled();expect(hook.result.current.busy).toBe(false);
});
it('bounds a hung CAS request and requires read reconciliation without replaying it',async()=>{
 vi.useFakeTimers();const scope={},reload=vi.fn(),accept=vi.fn();
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(()=>{}));
 const hook=renderHook(()=>useCASMutation(scope,()=>true,reload));
 act(()=>{void hook.result.current.run(write,accept);});
 await act(async()=>{await vi.advanceTimersByTimeAsync(15001);});
 expect(write.mock.calls[0][0].aborted).toBe(true);expect(write).toHaveBeenCalledOnce();
 expect(reload).toHaveBeenCalledOnce();expect(accept).not.toHaveBeenCalled();
 expect(hook.result.current.uncertain).toBe(true);expect(hook.result.current.busy).toBe(false);
});
it('aborts an old scope and cannot commit its response into the new settings',async()=>{
 const first={},second={},reload=vi.fn(),accept=vi.fn();let finish!:(value:string)=>void;
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finish=resolve}));
 const hook=renderHook(({scope})=>useCASMutation(scope,()=>true,reload),{initialProps:{scope:first}});
 let pending!:Promise<void>;act(()=>{pending=hook.result.current.run(write,accept);});
 hook.rerender({scope:second});expect(write.mock.calls[0][0].aborted).toBe(true);
 await act(async()=>{finish('old');await pending;});
 expect(accept).not.toHaveBeenCalled();expect(reload).not.toHaveBeenCalled();
});
it('refuses retained callbacks from an old scope before touching a new flight',async()=>{
 const first={},second={},reload=vi.fn(),accept=vi.fn();let finish!:(value:string)=>void;
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finish=resolve}));
 const hook=renderHook(({scope})=>useCASMutation(scope,()=>true,reload),{initialProps:{scope:first}});
 const old=hook.result.current;hook.rerender({scope:second});
 await act(async()=>{await old.run(write,accept);});expect(write).not.toHaveBeenCalled();
 let pending!:Promise<void>;act(()=>{pending=hook.result.current.run(write,accept);});
 act(()=>old.cancel());expect(write.mock.calls[0][0].aborted).toBe(false);
 await act(async()=>{finish('current');await pending;});
 expect(accept).toHaveBeenCalledWith('current');expect(reload).not.toHaveBeenCalled();
});
it('refuses a retained cancel callback after authority refresh within the same scope',async()=>{
 const scope={},reload=vi.fn(),accept=vi.fn();let activeRevision=1,finish!:(value:string)=>void;
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finish=resolve}));
 const hook=renderHook(({revision})=>useCASMutation(scope,()=>revision===activeRevision,reload),{initialProps:{revision:1}});
 const old=hook.result.current;activeRevision=2;hook.rerender({revision:2});
 let pending!:Promise<void>;act(()=>{pending=hook.result.current.run(write,accept);});
 act(()=>old.cancel());expect(write.mock.calls[0][0].aborted).toBe(false);
 await act(async()=>{finish('fresh');await pending;});
 expect(accept).toHaveBeenCalledWith('fresh');expect(reload).not.toHaveBeenCalled();
});
it('revokes a write when its read lifetime expires and fences its late rejection from a new flight',async()=>{
 const scope={},reload=vi.fn(),accept=vi.fn();let rejectOld!:(reason:Error)=>void,finishNew!:(value:string)=>void;
 const oldWrite=vi.fn((_signal:AbortSignal)=>new Promise<string>((_resolve,reject)=>{rejectOld=reject}));
 const newWrite=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finishNew=resolve}));
 let generation=1;
 const hook=renderHook(({active,revision})=>useCASMutation(scope,()=>active&&revision===generation,reload),{initialProps:{active:true,revision:1}});
 let oldPending!:Promise<void>;act(()=>{oldPending=hook.result.current.run(oldWrite,accept);});
 hook.rerender({active:false,revision:1});
 expect(oldWrite.mock.calls[0][0].aborted).toBe(true);expect(hook.result.current.busy).toBe(false);expect(hook.result.current.uncertain).toBe(true);
 generation=2;hook.rerender({active:true,revision:2});
 let newPending!:Promise<void>;act(()=>{newPending=hook.result.current.run(newWrite,accept);});
 await act(async()=>{rejectOld(new Error('late old write'));await oldPending;});
 expect(hook.result.current.busy).toBe(true);expect(newWrite.mock.calls[0][0].aborted).toBe(false);expect(accept).not.toHaveBeenCalled();expect(reload).not.toHaveBeenCalled();
 await act(async()=>{finishNew('fresh');await newPending;});
 expect(accept).toHaveBeenCalledExactlyOnceWith('fresh');expect(hook.result.current.busy).toBe(false);
});
it('releases an expired write even when a same-scope refresh already supplies a fresh valid callback',async()=>{
 const scope={},reload=vi.fn(),accept=vi.fn();let generation=1,finish!:(value:string)=>void;
 const write=vi.fn((_signal:AbortSignal)=>new Promise<string>(resolve=>{finish=resolve}));
 const hook=renderHook(({revision})=>useCASMutation(scope,()=>revision===generation,reload),{initialProps:{revision:1}});
 let pending!:Promise<void>;act(()=>{pending=hook.result.current.run(write,accept);});
 generation=2;hook.rerender({revision:2});
 expect(write.mock.calls[0][0].aborted).toBe(true);expect(hook.result.current.busy).toBe(false);expect(hook.result.current.uncertain).toBe(true);
 await act(async()=>{finish('old');await pending;});
 expect(accept).not.toHaveBeenCalled();expect(reload).not.toHaveBeenCalled();
});
