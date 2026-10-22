// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,it,expect,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
const captured=vi.hoisted(()=>({loads:[] as Array<()=>void>}));
vi.mock('react-native',async()=>{const React=await import('react');return {...await import('react-native-web'),Image:({source,onLoad,onError,accessibilityLabel}:any)=>{captured.loads.push(onLoad);return React.createElement('img',{src:source.uri,onLoad,onError,alt:accessibilityLabel});}};});
vi.mock('./perspective-preview-download',()=>({downloadPerspectivePreview:vi.fn(async()=>new Uint8Array([1,2,3]))}));
import {LensPreviewView} from './lens-preview-view';
import {LocaleProvider} from './locale-context';
import type {LensClient} from './lens-client';
import type {LensProposal} from './lens-proposal';
const proposal={tenant_id:'tenant',project_id:'project',producer_task_id:'producer',result_id:'result',result:{sha256:'a'.repeat(64)},base:{source:{artifact_id:'source',sha256:'b'.repeat(64)}},canvas:{width:100,height:80}} as LensProposal;
afterEach(()=>{cleanup();captured.loads=[];vi.useRealTimers();vi.unstubAllGlobals();vi.restoreAllMocks();});
it('keeps Apply readiness false until the displayed image loads, and clears it on expiry',async()=>{
 vi.useFakeTimers();vi.stubGlobal('createImageBitmap',vi.fn(async()=>({width:100,height:80,close:vi.fn()})));
 const create=vi.fn(()=> 'blob:lens'),revoke=vi.fn();vi.stubGlobal('URL',Object.assign(class extends URL{}, {createObjectURL:create,revokeObjectURL:revoke}));
 const expires=new Date(Date.now()+1000).toISOString(),ready=vi.fn();
 const api={preview:vi.fn(async()=>({media_type:'image/jpeg',width:100,height:80,expires_at:expires}))} as unknown as LensClient;
 render(<LocaleProvider><LensPreviewView api={api} proposal={proposal} onReady={ready}/></LocaleProvider>);
 await act(async()=>{for(let i=0;i<8;i++)await Promise.resolve();});
 const image=screen.getByRole('img',{name:'Lens correction'});expect(ready.mock.calls.every(call=>call[0]===false)).toBe(true);
 fireEvent.load(image);expect(ready).toHaveBeenLastCalledWith(true);
 await act(async()=>{await vi.advanceTimersByTimeAsync(1001);});
 expect(ready).toHaveBeenLastCalledWith(false);expect(screen.queryByRole('img')).toBeNull();expect(revoke).toHaveBeenCalledWith('blob:lens');
});
it('aborts and withdraws readiness when exact source scope changes',()=>{
 const preview=vi.fn((_proposal:LensProposal,_signal?:AbortSignal)=>new Promise<never>(()=>{})),api={preview} as unknown as LensClient,ready=vi.fn();
 const view=render(<LocaleProvider><LensPreviewView api={api} proposal={proposal} onReady={ready}/></LocaleProvider>);
 const signal=preview.mock.calls[0][1]!;
 const changed={...proposal,base:{...proposal.base,source:{...proposal.base.source,sha256:'c'.repeat(64)}}};
 view.rerender(<LocaleProvider><LensPreviewView api={api} proposal={changed} onReady={ready}/></LocaleProvider>);
 expect(signal.aborted).toBe(true);expect(ready).toHaveBeenLastCalledWith(false);expect(preview).toHaveBeenCalledTimes(2);
});

it('ignores the late load event of a withdrawn display after refresh',async()=>{
 vi.stubGlobal('createImageBitmap',vi.fn(async()=>({width:100,height:80,close:vi.fn()})));
 vi.stubGlobal('URL',Object.assign(class extends URL{}, {createObjectURL:()=> 'blob:lens-old',revokeObjectURL:vi.fn()}));
 const preview=vi.fn().mockResolvedValueOnce({media_type:'image/jpeg',width:100,height:80,expires_at:new Date(Date.now()+60000).toISOString()}).mockImplementationOnce(()=>new Promise(()=>{}));
 const ready=vi.fn(),api={preview} as unknown as LensClient;
 render(<LocaleProvider><LensPreviewView api={api} proposal={proposal} onReady={ready}/></LocaleProvider>);
 await act(async()=>{for(let i=0;i<8;i++)await Promise.resolve();});
 const oldLoad=captured.loads.at(-1)!;fireEvent.load(screen.getByRole('img'));expect(ready).toHaveBeenLastCalledWith(true);
 fireEvent.click(screen.getByRole('button',{name:'Reload perspective preview'}));expect(ready).toHaveBeenLastCalledWith(false);
 act(()=>oldLoad());expect(ready).toHaveBeenLastCalledWith(false);expect(screen.queryByRole('img')).toBeNull();
});
