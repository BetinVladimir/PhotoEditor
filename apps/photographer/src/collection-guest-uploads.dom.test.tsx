// @vitest-environment jsdom
import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const fixture=vi.hoisted(()=>({list:vi.fn(),moderate:vi.fn(),listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{fixture.listeners.add(listener);return {remove:()=>fixture.listeners.delete(listener)};}}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./collection-guest-uploads-api',()=>({CollectionGuestUploadsAPI:class{list=fixture.list;moderate=fixture.moderate;}}));
import {CollectionGuestUploads} from './collection-guest-uploads';
const id='11111111-1111-4111-8111-111111111111';
const row={id,collection_id:id,share_id:id,filename:'portrait.jpg',state:'verified',revision:'1',expected_size:100,actual_size:100,created_at:'2026-10-17T00:00:00Z',item_id:id,asset_version_id:id,moderation_state:'pending_review'};
const props=()=>({client:{} as any,origin:'https://photo.test',tenant:id,collectionId:id,state:'draft',canEdit:true,current:()=>true,onChanged:vi.fn(),acquire:vi.fn(()=>Symbol()),release:vi.fn()});
async function flush(){await act(async()=>{for(let i=0;i<12;i++)await Promise.resolve();});}
function setup(){vi.clearAllMocks();fixture.list.mockResolvedValue({items:[row],next_cursor:null});fixture.moderate.mockResolvedValue(undefined);}
afterEach(()=>{cleanup();vi.useRealTimers();fixture.listeners.clear();});
it('does not read upload metadata for a Viewer and never admits a decision for deleting collections',async()=>{
 setup();const p=props(),view=render(<CollectionGuestUploads {...p} canEdit={false}/>);await flush();expect(fixture.list).not.toHaveBeenCalled();
 view.rerender(<CollectionGuestUploads {...p} state="deleting"/>);await flush();fireEvent.click(screen.getByText('Accept photo'));await flush();expect(fixture.moderate).not.toHaveBeenCalled();
});
it('bounds a hung decision, reloads authoritative state, and ignores the late accepted response',async()=>{
 setup();vi.useFakeTimers();let finish!:()=>void;fixture.moderate.mockImplementation(()=>new Promise<void>(resolve=>{finish=resolve;}));
 const p=props();render(<CollectionGuestUploads {...p}/>);await flush();fireEvent.click(screen.getByText('Accept photo'));await flush();
 await act(async()=>{await vi.advanceTimersByTimeAsync(15000);});await flush();expect(p.release).toHaveBeenCalledTimes(1);expect(fixture.list).toHaveBeenCalledTimes(2);expect(fixture.moderate).toHaveBeenCalledTimes(1);
 finish();await flush();expect(p.onChanged).not.toHaveBeenCalled();expect(p.release).toHaveBeenCalledTimes(1);
});
it('aborts immediately in background and does not replay the decision on foreground return',async()=>{
 setup();let signal!:AbortSignal,finish!:()=>void;
 fixture.moderate.mockImplementation((_row:unknown,_decision:string,s:AbortSignal)=>{signal=s;return new Promise<void>(resolve=>{finish=resolve;});});
 const p=props();render(<CollectionGuestUploads {...p}/>);await flush();fireEvent.click(screen.getByText('Reject photo'));await flush();
 await act(async()=>{for(const listener of fixture.listeners)listener('background');});expect(signal.aborted).toBe(true);expect(p.release).toHaveBeenCalledTimes(1);
 await act(async()=>{for(const listener of fixture.listeners)listener('active');});await flush();finish();await flush();expect(fixture.moderate).toHaveBeenCalledTimes(1);expect(p.onChanged).not.toHaveBeenCalled();
});
it('withdraws an old decision when switching collection and ignores its noncooperative response',async()=>{
 setup();let finish!:()=>void;fixture.moderate.mockImplementation(()=>new Promise<void>(resolve=>{finish=resolve;}));
 const p=props(),view=render(<CollectionGuestUploads {...p}/>);await flush();fireEvent.click(screen.getByText('Accept photo'));await flush();
 view.rerender(<CollectionGuestUploads {...p} collectionId="22222222-2222-4222-8222-222222222222"/>);await flush();finish();await flush();expect(p.release).toHaveBeenCalledTimes(1);expect(p.onChanged).not.toHaveBeenCalled();expect(fixture.moderate).toHaveBeenCalledTimes(1);
});
it('refuses a pending metadata response after the parent withdraws its scope',async()=>{
 setup();let allowed=true,finish!:(page:unknown)=>void;
 fixture.list.mockImplementation(()=>new Promise(resolve=>{finish=resolve;}));
 const p={...props(),current:()=>allowed};render(<CollectionGuestUploads {...p}/>);await flush();
 allowed=false;finish({items:[row],next_cursor:null});await flush();
 expect(screen.queryByText('portrait.jpg')).toBeNull();
 expect(fixture.moderate).not.toHaveBeenCalled();
 expect(p.onChanged).not.toHaveBeenCalled();
});
