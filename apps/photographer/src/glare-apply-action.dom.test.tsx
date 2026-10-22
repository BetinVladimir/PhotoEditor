// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
const app=vi.hoisted(()=>({listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{app.listeners.add(listener);return {remove:()=>app.listeners.delete(listener)}}}}));
import {GlareApplyAction} from './glare-apply-action';
const review={id:'review',kind:'portrait.glare',state:'decided',decision:'accept',decision_hash:'hash'} as Review;
afterEach(cleanup);
it('opens only after the saved result and actor confirmation',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn(),applyGlareSelected=vi.fn().mockResolvedValue(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 render(<GlareApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyGlareSelected).toHaveBeenCalledTimes(1);
});
it('allows retry of the same decision after a lost response',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn();
 const applyGlareSelected=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 render(<GlareApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyGlareSelected.mock.calls.map(args=>args[0])).toEqual([review,review]);
});
it('does not navigate after the actor changes',async()=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),api={me:vi.fn().mockResolvedValueOnce({id:'first'}).mockResolvedValueOnce({id:'other'}),applyGlareSelected:vi.fn().mockResolvedValue({})} as unknown as OwnerAPI;
 render(<GlareApplyAction api={api} review={review} onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');expect(onOpen).not.toHaveBeenCalled();
 expect(onAccessDenied).toHaveBeenCalledTimes(1);expect(screen.getByRole('button').getAttribute('aria-disabled')).toBe('true');
});

it.each([401,403,404,410])('withdraws Apply after definitive denial %s',async(code)=>{
 const onOpen=vi.fn(),onAccessDenied=vi.fn(),applyGlareSelected=vi.fn().mockRejectedValue(new APIError(code,'DENIED'));
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 render(<GlareApplyAction api={api} review={review} onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));await screen.findByText(/selected result is no longer accessible/);
 expect(screen.getByRole('button').getAttribute('aria-disabled')).toBe('true');expect(onOpen).not.toHaveBeenCalled();expect(applyGlareSelected).toHaveBeenCalledTimes(1);expect(onAccessDenied).toHaveBeenCalledTimes(1);
});

function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it('aborts the selected Apply on unmount and ignores a late successful response',async()=>{
 const pending=deferred<unknown>(),onOpen=vi.fn(),applyGlareSelected=vi.fn().mockReturnValue(pending.promise);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 const mounted=render(<GlareApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyGlareSelected).toHaveBeenCalledTimes(1));
 const signal=applyGlareSelected.mock.calls[0][1] as AbortSignal;
 mounted.unmount();expect(signal.aborted).toBe(true);
 pending.resolve({asset_version_id:'old-version'});
 await pending.promise;await Promise.resolve();
 expect(onOpen).not.toHaveBeenCalled();expect(api.me).toHaveBeenCalledTimes(1);
});
it('does not open an old result after the review decision changes',async()=>{
 const pending=deferred<unknown>(),onOpen=vi.fn(),applyGlareSelected=vi.fn().mockReturnValue(pending.promise);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 const mounted=render(<GlareApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(applyGlareSelected).toHaveBeenCalledTimes(1));
 const signal=applyGlareSelected.mock.calls[0][1] as AbortSignal;
 mounted.rerender(<GlareApplyAction api={api} review={{...review,decision:'reject',decision_hash:'next-hash'}} onOpen={onOpen}/>);
 expect(signal.aborted).toBe(true);
 pending.resolve({asset_version_id:'old-version'});await pending.promise;await Promise.resolve();
 expect(onOpen).not.toHaveBeenCalled();expect(api.me).toHaveBeenCalledTimes(1);
 expect(screen.getByText('Open original in editor')).toBeTruthy();
});

it('ignores a late save in background and recovers the same decision after foreground',async()=>{
 let finish!:(value:unknown)=>void;
 const applyGlareSelected=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({asset_version_id:'version'}),onOpen=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyGlareSelected} as unknown as OwnerAPI;
 render(<GlareApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(applyGlareSelected).toHaveBeenCalledTimes(1));
 act(()=>app.listeners.forEach(listener=>listener('background')));
 await act(async()=>finish({asset_version_id:'version'}));expect(onOpen).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Save result and open editor'));expect(applyGlareSelected).toHaveBeenCalledTimes(1);
 act(()=>app.listeners.forEach(listener=>listener('active')));
 fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(onOpen).toHaveBeenCalledWith({asset_version_id:'version'}));
 expect(applyGlareSelected.mock.calls.map(args=>args[0])).toEqual([review,review]);
});
