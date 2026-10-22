// @vitest-environment jsdom
import {expectEnabled} from './test/ui-readiness';
import {APIError} from '@photoeditor/api-client';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {Review} from './catalog-types';
import type {OwnerAPI} from './owner-api';
const native=vi.hoisted(()=>({handlers:[] as Array<{name:string;invoke:()=>void}>,listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>{const web=await import('react-native-web'),React=await import('react');return {...web,AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{native.listeners.add(listener);return {remove:()=>native.listeners.delete(listener)}}}};});
import {RelightApplyAction} from './relight-apply-action';
import {OpenEyesApplyAction} from './open-eyes-apply-action';
import {ReflectionApplyAction} from './reflection-apply-action';
import {NegativeApplyAction} from './negative-apply-action';
import {GlareApplyAction} from './glare-apply-action';
import {LocalPortraitApplyAction} from './local-portrait-apply-action';
afterEach(()=>{cleanup();vi.useRealTimers();native.handlers=[];native.listeners.clear();});
const cases=[{Component:OpenEyesApplyAction,kind:'portrait.open_eyes',method:'applyOpenEyesSelected'},{Component:ReflectionApplyAction,kind:'image.reflection',method:'applyReflectionSelected'},{Component:NegativeApplyAction,kind:'optical.negative',method:'applyNegativeSelected'},{Component:RelightApplyAction,kind:'image.relight',method:'applyRelightSelected'},{Component:GlareApplyAction,kind:'portrait.glare',method:'applyGlareSelected'},{Component:LocalPortraitApplyAction,kind:'portrait.hair',method:'applyLocalPortraitSelected'}] as const;
const review=(kind:string)=>({id:'review',kind,state:'decided',decision:'accept',decision_hash:'hash'} as Review);
const flush=async()=>{for(let n=0;n<35;n++)await Promise.resolve();};
it.each(cases)('$kind ties Stop and its total deadline to the displayed selected-result request',async({Component,kind,method})=>{
 const pending:Array<(value:unknown)=>void>=[],save=vi.fn((_review:Review,_signal:AbortSignal)=>new Promise(resolve=>pending.push(resolve))),onOpen=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),[method]:save} as unknown as OwnerAPI;
 render(<Component api={api} review={review(kind)} onOpen={onOpen}/>);
 vi.useFakeTimers();fireEvent.click(screen.getByText('Save result and open editor'));await act(flush);expect(save).toHaveBeenCalledOnce();
 const oldStop=native.handlers.filter(h=>h.name==='Cancel saving result').at(-1)!.invoke;
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000);oldStop();});
 fireEvent.click(screen.getByText('Save result and open editor'));await act(flush);expect(save).toHaveBeenCalledTimes(2);
 const signal=save.mock.calls[1][1];expect(save.mock.calls[1][0]).toEqual(save.mock.calls[0][0]);
 await act(async()=>{oldStop();await vi.advanceTimersByTimeAsync(5001);});expect(signal.aborted).toBe(false);
 await act(async()=>{pending[0]({asset_version_id:'stale'});});expect(onOpen).not.toHaveBeenCalled();
 await act(async()=>{await vi.advanceTimersByTimeAsync(9999);});expect(signal.aborted).toBe(true);
 expectEnabled(screen.getByText('Save result and open editor'));
 await act(async()=>{pending[1]({asset_version_id:'late'});});expect(onOpen).not.toHaveBeenCalled();
});
it.each(cases)('$kind refuses a retained Apply after unmount and while backgrounded',async({Component,kind,method})=>{
 const save=vi.fn(),api={me:vi.fn().mockResolvedValue({id:'actor'}),[method]:save} as unknown as OwnerAPI;
 const view=render(<Component api={api} review={review(kind)} onOpen={vi.fn()}/>);
 const old=native.handlers.find(h=>h.name==='Save result and open editor')!.invoke;
 await act(async()=>{for(const listener of native.listeners)listener('background');old();});expect(api.me).not.toHaveBeenCalled();expect(save).not.toHaveBeenCalled();
 view.unmount();await act(async()=>{old();});expect(api.me).not.toHaveBeenCalled();
});
it.each(cases)('$kind bounds an abort-ignoring actor lookup before selected Apply',async({Component,kind,method})=>{
 let release!:(value:unknown)=>void;const save=vi.fn(),api={me:vi.fn(()=>new Promise(resolve=>{release=resolve;})),[method]:save} as unknown as OwnerAPI;
 render(<Component api={api} review={review(kind)} onOpen={vi.fn()}/>);vi.useFakeTimers();fireEvent.click(screen.getByText('Save result and open editor'));await act(flush);
 await act(async()=>{await vi.advanceTimersByTimeAsync(15000);});expect(screen.getByRole('alert')).toBeTruthy();
 await act(async()=>{release({id:'actor'});});expect(save).not.toHaveBeenCalled();
});


it.each(cases)('$kind blocks a retained pre-refusal Apply after current access is denied',async({Component,kind,method})=>{
 const save=vi.fn().mockRejectedValue(new APIError(403,'FORBIDDEN')),onAccessDenied=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),[method]:save} as unknown as OwnerAPI;
 render(<Component api={api} review={review(kind)} onOpen={vi.fn()} onAccessDenied={onAccessDenied}/>);
 const old=native.handlers.find(h=>h.name==='Save result and open editor')!.invoke;
 await act(async()=>{old();});await screen.findByRole('alert');expect(save).toHaveBeenCalledOnce();
 const reads=vi.mocked(api.me).mock.calls.length;await act(async()=>{old();});
 expect(save).toHaveBeenCalledOnce();expect(api.me).toHaveBeenCalledTimes(reads);expect(onAccessDenied).toHaveBeenCalledOnce();
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(typeof label==='string')native.handlers.push({name:label,invoke:props.onPress});};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
