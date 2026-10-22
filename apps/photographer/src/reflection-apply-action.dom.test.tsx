// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
const app=vi.hoisted(()=>({listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{app.listeners.add(listener);return {remove:()=>app.listeners.delete(listener)}}}}));
import {APIError} from '@photoeditor/api-client';
import {ReflectionApplyAction} from './reflection-apply-action';
const review={id:'review',kind:'image.reflection',state:'decided',decision:'accept',decision_hash:'hash'} as Review;
afterEach(cleanup);
it('opens only after the saved result and actor confirmation',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn(),applyReflectionSelected=vi.fn().mockResolvedValue(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyReflectionSelected} as unknown as OwnerAPI;
 render(<ReflectionApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyReflectionSelected).toHaveBeenCalledTimes(1);
});
it('allows retry of the same decision after a lost response',async()=>{
 const result={asset_version_id:'version'},onOpen=vi.fn();
 const applyReflectionSelected=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyReflectionSelected} as unknown as OwnerAPI;
 render(<ReflectionApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');
 fireEvent.click(screen.getByText('Save result and open editor'));
 await waitFor(()=>expect(onOpen).toHaveBeenCalledWith(result));
 expect(applyReflectionSelected.mock.calls.map(args=>args[0])).toEqual([review,review]);
});
it('does not navigate after the actor changes',async()=>{
 const onOpen=vi.fn(),api={me:vi.fn().mockResolvedValueOnce({id:'first'}).mockResolvedValueOnce({id:'other'}),applyReflectionSelected:vi.fn().mockResolvedValue({})} as unknown as OwnerAPI;
 render(<ReflectionApplyAction api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));
 await screen.findByRole('alert');expect(onOpen).not.toHaveBeenCalled();
});

it.each([401,403,404,410])('blocks repeated Apply after definitive resource denial %s',async status=>{
 const applyReflectionSelected=vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN')),onAccessDenied=vi.fn(),onOpen=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyReflectionSelected} as unknown as OwnerAPI;
 render(<ReflectionApplyAction api={api} review={review} onOpen={onOpen} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(screen.getByText('Save result and open editor'));await screen.findByRole('alert');fireEvent.click(screen.getByText('Save result and open editor'));
 expect(applyReflectionSelected).toHaveBeenCalledTimes(1);expect(onAccessDenied).toHaveBeenCalledTimes(1);expect(onOpen).not.toHaveBeenCalled();
});
it('suppresses a late saved result in background and retries the same decision after foreground',async()=>{
 let finish!:(value:unknown)=>void;const applyReflectionSelected=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({asset_version_id:'version'}),onOpen=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),applyReflectionSelected} as unknown as OwnerAPI;
 render(<ReflectionApplyAction api={api} review={review} onOpen={onOpen}/>);fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(applyReflectionSelected).toHaveBeenCalledTimes(1));
 act(()=>app.listeners.forEach(listener=>listener('background')));await act(async()=>finish({asset_version_id:'version'}));expect(onOpen).not.toHaveBeenCalled();
 act(()=>app.listeners.forEach(listener=>listener('active')));fireEvent.click(screen.getByText('Save result and open editor'));await waitFor(()=>expect(onOpen).toHaveBeenCalled());expect(applyReflectionSelected.mock.calls.map(args=>args[0])).toEqual([review,review]);
});
