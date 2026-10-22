// @vitest-environment jsdom
import {beforeEach,afterEach,it,expect,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {webcrypto,randomUUID} from 'node:crypto';
import {Buffer} from 'node:buffer';
const state=vi.hoisted(()=>({begin:vi.fn(),parts:vi.fn(),complete:vi.fn()}));
vi.mock('../../portal/src/lib/api',async()=>({...await vi.importActual('../../portal/src/lib/api'),secretFromFragment:()=>null,getCollection:async()=>({id:'collection',name:'Gallery',revision:'1',allowed_presets:[],capabilities:['upload'],empty:true}),beginUpload:state.begin,signParts:state.parts,completeUpload:state.complete}));
import portalEnglish from '../../portal/src/catalogs/en.json';
import {Portal} from '../../portal/src/components/Portal';
const share='22222222-2222-4222-8222-222222222222';
const upload={id:'11111111-1111-4111-8111-111111111111',state:'uploading',revision:'1',filename:'photo.jpg',expected_size:4,part_bytes:16777216,parts_total:1,expires_at:'2099-01-01T00:00:00Z'};
beforeEach(()=>{sessionStorage.clear();sessionStorage.setItem(`photo:portal:${share}:csrf`,randomUUID());vi.clearAllMocks();vi.stubGlobal('crypto',{subtle:{digest:(algorithm:string,data:ArrayBuffer|ArrayBufferView)=>webcrypto.subtle.digest(algorithm,Buffer.from(ArrayBuffer.isView(data)?new Uint8Array(data.buffer,data.byteOffset,data.byteLength):new Uint8Array(data)))},randomUUID});Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});state.begin.mockResolvedValue(upload);state.parts.mockResolvedValue({items:[{part_no:1,url:'https://example.invalid/part'}]});state.complete.mockResolvedValue({job_id:upload.id,status:'queued'});vi.stubGlobal('fetch',vi.fn(async()=>new Response('',{headers:{etag:'etag'}})));});
afterEach(()=>{cleanup();vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals();});
async function choose(container:HTMLElement){await screen.findByText('Gallery');const f=new File(['data'],'photo.jpg');Object.defineProperty(f,'arrayBuffer',{value:async()=>new Uint8Array([1,2,3,4]).buffer});fireEvent.change(container.querySelector('input[type=file]')!,{target:{files:[f]}});await waitFor(()=>expect(state.begin).toHaveBeenCalledTimes(1));}
it('Stop releases hung upload locally, late begin cannot sign parts, Retry keeps exact begin key',async()=>{
 let release!:(v:typeof upload)=>void;state.begin.mockImplementationOnce(()=>new Promise(r=>{release=r}));
 const view=render(<Portal locale="en" shareID={share}/>);await choose(view.container);
 const key=state.begin.mock.calls[0][3];fireEvent.click(screen.getByText(portalEnglish.stop_waiting));
 await act(async()=>release(upload));expect(state.parts).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Try again'));await waitFor(()=>expect(state.complete).toHaveBeenCalledTimes(1));
 expect(state.begin.mock.calls[1][3]).toBe(key);
});
it('background aborts upload and late reply cannot continue or publish accepted notice',async()=>{
 let release!:(v:typeof upload)=>void;state.begin.mockImplementationOnce(()=>new Promise(r=>{release=r}));
 const view=render(<Portal locale="en" shareID={share}/>);await choose(view.container);
 Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});fireEvent(document,new Event('visibilitychange'));
 await act(async()=>release(upload));expect(state.parts).not.toHaveBeenCalled();expect(screen.queryByText('Upload accepted for processing.')).toBeNull();
});
it('whole upload deadline releases noncooperative transport and preserves exact retry intent',async()=>{
 const callbacks:Array<()=>void>=[];const native=window.setTimeout.bind(window);
 vi.spyOn(window,'setTimeout').mockImplementation(((callback:TimerHandler,delay?:number,...args:unknown[])=>{if(delay===300000)callbacks.push(callback as ()=>void);return native(callback,delay,...args);}) as typeof window.setTimeout);
 state.begin.mockReturnValue(new Promise(()=>{}));const view=render(<Portal locale="en" shareID={share}/>);await choose(view.container);
 expect(callbacks).toHaveLength(1);await act(async()=>callbacks[0]());
 expect(screen.queryByText(portalEnglish.stop_waiting)).toBeNull();expect(screen.getByText('Try again')).toBeTruthy();expect(state.parts).not.toHaveBeenCalled();
});
it('replacement share withdraws old upload before late reply and never resumes its queue in the new gallery',async()=>{
 let release!:(v:typeof upload)=>void;state.begin.mockImplementationOnce(()=>new Promise(r=>{release=r}));
 const view=render(<Portal locale="en" shareID={share}/>);await choose(view.container);
 const replacement='33333333-3333-4333-8333-333333333333';sessionStorage.setItem(`photo:portal:${replacement}:csrf`,'different-token');
 view.rerender(<Portal locale="en" shareID={replacement}/>);await screen.findByText('Gallery');
 await act(async()=>release(upload));expect(state.parts).not.toHaveBeenCalled();expect(screen.queryByText('Upload accepted for processing.')).toBeNull();
 expect(screen.queryByText('Try again')).toBeNull();
});
