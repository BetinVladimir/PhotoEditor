// @vitest-environment jsdom
import {APIError} from "@photoeditor/api-client";
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
const state=vi.hoisted(()=>({currentState:'active',listeners:new Set<(state:string)=>void>(),open:vi.fn()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{get currentState(){return state.currentState},addEventListener:(_:string,fn:(state:string)=>void)=>{state.listeners.add(fn);return{remove:()=>state.listeners.delete(fn)}}},Linking:{openURL:state.open}}));
import {DownloadJobPanel} from './download-job-panel';
import type {OwnerAPI} from './owner-api';
afterEach(()=>{cleanup();state.currentState='active';state.open.mockReset()});
const job={id:'job',status:'ready',files_done:1,files_total:1,bytes_written:100,volumes:[{number:1,status:'ready',bytes:100,delivery_kind:'zip'}]};
it('exposes ready volume and opens only the returned current link',async()=>{
 const api={downloadJob:vi.fn(async()=>job),downloadJobVolume:vi.fn(async()=> 'https://objects.test/file?versionId=v1')};
 render(<DownloadJobPanel api={api as unknown as OwnerAPI} id="job"/>);
 fireEvent.click(await screen.findByText('Download 1'));
 await act(async()=>{});expect(state.open).toHaveBeenCalledWith('https://objects.test/file?versionId=v1');
});
it('does not open a delayed link after the app leaves foreground',async()=>{
 let finish!:(value:string)=>void;
 const api={downloadJob:vi.fn(async()=>job),downloadJobVolume:vi.fn(()=>new Promise<string>(resolve=>{finish=resolve}))};
 render(<DownloadJobPanel api={api as unknown as OwnerAPI} id="job"/>);
 fireEvent.click(await screen.findByText('Download 1'));
 await act(async()=>{state.currentState='background';for(const listener of state.listeners)listener('background');finish('https://objects.test/private?versionId=v1')});
 expect(state.open).not.toHaveBeenCalled();
});

it('hides ready volumes after definitive access refusal',async()=>{
 const api={downloadJob:vi.fn(async()=>job),downloadJobVolume:vi.fn(async()=>{throw new APIError(403,'DOWNLOAD_FORBIDDEN')})};
 render(<DownloadJobPanel api={api as unknown as OwnerAPI} id="job"/>);
 fireEvent.click(await screen.findByText('Download 1'));
 await screen.findByText('Download is no longer available');
 expect(screen.queryByText('Download 1')).toBeNull();expect(state.open).not.toHaveBeenCalled();
});
it('cancels link acquisition and does not open its late URL',async()=>{
 let finish!:(value:string)=>void;const api={downloadJob:vi.fn(async()=>job),downloadJobVolume:vi.fn(()=>new Promise<string>(resolve=>{finish=resolve}))};
 render(<DownloadJobPanel api={api as unknown as OwnerAPI} id="job"/>);fireEvent.click(await screen.findByText('Download 1'));
 fireEvent.click(screen.getByText('Stop loading'));await act(async()=>finish('https://objects.test/private?versionId=late'));
 expect(state.open).not.toHaveBeenCalled();expect(screen.getByText('Download 1')).toBeTruthy();
});
it('rejects pre-background polling data even if its transport completes after foreground return',async()=>{
 let finish!:(value:unknown)=>void;const api={downloadJob:vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({...job,status:'running',volumes:[]})};
 render(<DownloadJobPanel api={api as unknown as OwnerAPI} id="job"/>);
 await act(async()=>{state.currentState='background';for(const listener of state.listeners)listener('background')});
 await act(async()=>{state.currentState='active';for(const listener of state.listeners)listener('active');finish(job)});
 expect(screen.queryByText('Download 1')).toBeNull();expect(state.open).not.toHaveBeenCalled();
});
