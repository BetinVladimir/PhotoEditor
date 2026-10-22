// @vitest-environment jsdom
import {act,cleanup,renderHook,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {Capabilities} from './catalog-types';
const state=vi.hoisted(()=>({listener:null as null|((value:string)=>void)}));
vi.mock('react-native',()=>({AppState:{currentState:'active',addEventListener:(_name:string,listener:(value:string)=>void)=>{state.listener=listener;return {remove:()=>{state.listener=null;}};}}}));
import {useEditorCapabilities} from './use-editor-capabilities';
const ready:Capabilities={operations:[{operation:'image.generate',resource_class:'gpu',supported_backends:['cuda'],available_backends:['cuda'],available:true,status:'ready'}]};
afterEach(()=>{cleanup();vi.useRealTimers();});
it('clears availability in background and discards a late response before foreground refresh',async()=>{
 let finish!:(value:Capabilities)=>void;
 const client={capabilities:vi.fn().mockImplementationOnce(()=>new Promise<Capabilities>(resolve=>{finish=resolve;})).mockResolvedValue(ready)};
 const {result}=renderHook(()=>useEditorCapabilities(client,'source',vi.fn()));
 act(()=>state.listener?.('background'));expect(result.current.current.current).toBeUndefined();
 await act(async()=>finish(ready));expect(result.current.capabilities).toBeUndefined();
 act(()=>state.listener?.('active'));await waitFor(()=>expect(result.current.capabilities).toEqual(ready));
 act(()=>state.listener?.('inactive'));expect(result.current.capabilities).toBeUndefined();expect(result.current.current.current).toBeUndefined();
});
it('does not reuse the old source snapshot or accept its late response',async()=>{
 let finish!:(value:Capabilities)=>void;
 const client={capabilities:vi.fn().mockImplementationOnce(()=>new Promise<Capabilities>(resolve=>{finish=resolve;})).mockResolvedValue({operations:[]})};
 const {result,rerender}=renderHook(({scope})=>useEditorCapabilities(client,scope,vi.fn()),{initialProps:{scope:'first'}});
 rerender({scope:'second'});await waitFor(()=>expect(result.current.capabilities).toEqual({operations:[]}));
 await act(async()=>finish(ready));expect(result.current.capabilities).toEqual({operations:[]});
});
it('clears previously ready availability when polling fails',async()=>{
 vi.useFakeTimers();const onError=vi.fn(),client={capabilities:vi.fn().mockResolvedValueOnce(ready).mockRejectedValue(new Error('offline'))};
 const {result}=renderHook(()=>useEditorCapabilities(client,'source',onError));
 await act(async()=>{});expect(result.current.capabilities).toEqual(ready);
 await act(async()=>vi.advanceTimersByTimeAsync(10000));
 expect(result.current.capabilities).toBeUndefined();expect(result.current.current.current).toBeUndefined();expect(onError).toHaveBeenCalledOnce();
});

it('expires availability on a hung refresh and rejects its late successful response',async()=>{
 vi.useFakeTimers();let finish!:(value:Capabilities)=>void;
 const onError=vi.fn(),client={capabilities:vi.fn().mockResolvedValueOnce(ready).mockImplementationOnce(()=>new Promise<Capabilities>(resolve=>{finish=resolve;})).mockResolvedValue({operations:[]})};
 const {result}=renderHook(()=>useEditorCapabilities(client,'source',onError));
 await act(async()=>{});expect(result.current.capabilities).toEqual(ready);
 await act(async()=>vi.advanceTimersByTimeAsync(10000));
 const signal=client.capabilities.mock.calls[1][0] as AbortSignal;
 await act(async()=>vi.advanceTimersByTimeAsync(8000));expect(signal.aborted).toBe(true);expect(result.current.capabilities).toBeUndefined();expect(onError).toHaveBeenCalledOnce();
 await act(async()=>finish(ready));expect(result.current.capabilities).toBeUndefined();
 await act(async()=>vi.advanceTimersByTimeAsync(2000));expect(result.current.capabilities).toEqual({operations:[]});
});


it('withdraws the live reference when a ready screen unmounts',async()=>{
 const client={capabilities:vi.fn().mockResolvedValue(ready)};
 const {result,unmount}=renderHook(()=>useEditorCapabilities(client,'source',vi.fn()));
 await waitFor(()=>expect(result.current.capabilities).toEqual(ready));
 const reference=result.current.current;
 unmount();
 expect(reference.current).toBeUndefined();
});

it('aborts pending availability and ignores its late response after unmount',async()=>{
 let finish!:(value:Capabilities)=>void;
 const client={capabilities:vi.fn((signal?:AbortSignal)=>new Promise<Capabilities>(resolve=>{finish=resolve;}))};
 const onError=vi.fn();
 const {result,unmount}=renderHook(()=>useEditorCapabilities(client,'source',onError));
 const reference=result.current.current;
 const signal=client.capabilities.mock.calls[0][0];
 unmount();expect(signal?.aborted).toBe(true);
 await act(async()=>finish(ready));
 expect(reference.current).toBeUndefined();expect(onError).not.toHaveBeenCalled();
});
