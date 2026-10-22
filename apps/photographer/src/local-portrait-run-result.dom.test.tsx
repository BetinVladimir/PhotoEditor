// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
import {LocalPortraitRunResult} from './local-portrait-run-result';
const selected={run_id:'run',result_id:'result'};
const read=()=>vi.fn().mockResolvedValue(selected);
afterEach(cleanup);
it('opens only after the saved result and actor confirmation',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn(),applyLocalPortraitResult=vi.fn().mockResolvedValue(result);
 const api={localPortraitResult:read(),me:vi.fn().mockResolvedValue({id:'actor'}),applyLocalPortraitResult} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1);
});
it('allows retry of the same decision after a lost response',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn();
 const applyLocalPortraitResult=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 const api={localPortraitResult:read(),me:vi.fn().mockResolvedValue({id:'actor'}),applyLocalPortraitResult} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyLocalPortraitResult.mock.calls.map(args=>args[1])).toEqual([selected,selected]);
});
it('does not navigate after the actor changes',async()=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),api={localPortraitResult:read(),me:vi.fn().mockResolvedValueOnce({id:'first'}).mockResolvedValueOnce({id:'other'}),applyLocalPortraitResult:vi.fn().mockResolvedValue({})} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');expect(onOpen).not.toHaveBeenCalled();
 expect(onAccessDenied).toHaveBeenCalledTimes(1);expect(screen.getByRole('button').getAttribute('aria-disabled')).toBe('true');
});

it.each([401,403,404,410])('withdraws Apply after definitive denial %s',async(code)=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),applyLocalPortraitResult=vi.fn().mockRejectedValue(new APIError(code,'DENIED'));
 const api={localPortraitResult:read(),me:vi.fn().mockResolvedValue({id:'actor'}),applyLocalPortraitResult} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));await screen.findByText(/result is no longer accessible/);
 expect(screen.getByRole('button').getAttribute('aria-disabled')).toBe('true');expect(onOpen).not.toHaveBeenCalled();expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1);expect(onAccessDenied).toHaveBeenCalledTimes(1);
});

function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it('aborts the result Apply on unmount and ignores a late successful response',async()=>{
 const pending=deferred<unknown>(),onOpen=vi.fn(),applyLocalPortraitResult=vi.fn().mockReturnValue(pending.promise);
 const api={localPortraitResult:read(),me:vi.fn().mockResolvedValue({id:'actor'}),applyLocalPortraitResult} as unknown as OwnerAPI;
 const mounted=render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1));
 const signal=applyLocalPortraitResult.mock.calls[0][2] as AbortSignal;
 mounted.unmount();expect(signal.aborted).toBe(true);
 pending.resolve({asset_version_id:'old-version'});
 await pending.promise;await Promise.resolve();
 expect(onOpen).not.toHaveBeenCalled();expect(api.me).toHaveBeenCalledTimes(1);
});

it('aborts a stale save when the workflow scope changes',async()=>{
 const pending=deferred<unknown>(),onOpen=vi.fn(),applyLocalPortraitResult=vi.fn().mockReturnValue(pending.promise);
 const api={localPortraitResult:read(),me:vi.fn().mockResolvedValue({id:'actor'}),applyLocalPortraitResult} as unknown as OwnerAPI;
 const mounted=render(<LocalPortraitRunResult api={api} workflow="first" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1));
 const signal=applyLocalPortraitResult.mock.calls[0][2] as AbortSignal;
 mounted.rerender(<LocalPortraitRunResult api={api} workflow="second" run="other-run" onOpen={onOpen}/>);
 expect(signal.aborted).toBe(true);
 pending.resolve({asset_version_id:'stale'});await pending.promise;await Promise.resolve();
 expect(onOpen).not.toHaveBeenCalled();expect(api.me).toHaveBeenCalledTimes(1);
});

it.each([401,403,404,410])('does not mutate after result lookup denial %s',async code=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),applyLocalPortraitResult=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),localPortraitResult:vi.fn().mockRejectedValue(new APIError(code,'DENIED')),applyLocalPortraitResult} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');expect(applyLocalPortraitResult).not.toHaveBeenCalled();expect(onOpen).not.toHaveBeenCalled();
 expect(onAccessDenied).toHaveBeenCalledTimes(1);expect(screen.getByRole('button').getAttribute('aria-disabled')).toBe('true');
});
it('retries a transient lookup failure without sending an unbound Apply',async()=>{
 const localPortraitResult=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(selected);
 const saved={asset_version_id:'saved'},applyLocalPortraitResult=vi.fn().mockResolvedValue(saved),onOpen=vi.fn();
 const api={localPortraitResult,applyLocalPortraitResult,me:vi.fn().mockResolvedValue({id:'actor'})} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));await screen.findByRole('alert');
 expect(applyLocalPortraitResult).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(saved));
 expect(localPortraitResult.mock.calls.map(args=>args.slice(0,2))).toEqual([['workflow','run'],['workflow','run']]);
 expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1);
});
it('never starts Apply after a stale result lookup completes following unmount',async()=>{
 const pending=deferred<unknown>(),localPortraitResult=vi.fn().mockReturnValue(pending.promise),applyLocalPortraitResult=vi.fn(),onOpen=vi.fn();
 const api={localPortraitResult,applyLocalPortraitResult,me:vi.fn().mockResolvedValue({id:'actor'})} as unknown as OwnerAPI;
 const mounted=render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(localPortraitResult).toHaveBeenCalledTimes(1));
 const signal=localPortraitResult.mock.calls[0][2] as AbortSignal;mounted.unmount();expect(signal.aborted).toBe(true);
 pending.resolve(selected);await pending.promise;await Promise.resolve();
 expect(applyLocalPortraitResult).not.toHaveBeenCalled();expect(onOpen).not.toHaveBeenCalled();
});
it('Stop permits a replacement save and the old receipt cannot navigate or unlock it',async()=>{
 const first=deferred<unknown>(),second=deferred<unknown>(),onOpen=vi.fn();const applyLocalPortraitResult=vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),localPortraitResult:read(),applyLocalPortraitResult} as unknown as OwnerAPI;
 render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(applyLocalPortraitResult).toHaveBeenCalledTimes(1));fireEvent.click(screen.getByText('Stop waiting for saved result'));fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(applyLocalPortraitResult).toHaveBeenCalledTimes(2));first.resolve({asset_version_id:'old'});await first.promise;await Promise.resolve();expect(onOpen).not.toHaveBeenCalled();expect(screen.getByText('Saving…')).toBeTruthy();second.resolve({asset_version_id:'new'});await waitFor(()=>expect(onOpen).toHaveBeenCalledWith({asset_version_id:'new'}));
});
it('bounds the whole stalled actor/result/apply chain to fifteen seconds',async()=>{
 vi.useFakeTimers();try{const onOpen=vi.fn(),api={me:vi.fn().mockReturnValue(new Promise(()=>{})),localPortraitResult:read(),applyLocalPortraitResult:vi.fn()} as unknown as OwnerAPI;render(<LocalPortraitRunResult api={api} workflow="workflow" run="run" onOpen={onOpen}/>);fireEvent.click(screen.getByText('Save result and open editor'));await import('@testing-library/react').then(({act})=>act(async()=>{await vi.advanceTimersByTimeAsync(15000)}));expect(screen.getByText('Save result and open editor')).toBeTruthy();expect(api.applyLocalPortraitResult).not.toHaveBeenCalled();expect(onOpen).not.toHaveBeenCalled();}finally{vi.useRealTimers();}
});
