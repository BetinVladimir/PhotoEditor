// @vitest-environment jsdom
// Uses the existing owner-app DOM harness against the actual portal component;
// the portal's own package otherwise contains only pure transport/unit tests.
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
const state=vi.hoisted(()=>({status:'ready',failure:0,redeem:vi.fn(),cancel:vi.fn()}));
vi.mock('../../portal/src/lib/api',async()=>{
 const actual=await vi.importActual<typeof import('../../portal/src/lib/api')>('../../portal/src/lib/api');
 return {...actual,
  secretFromFragment:()=>null,
  getCollection:vi.fn(async()=>({id:'collection',name:'Private collection',revision:'1',allowed_presets:['preset'],capabilities:['download'],empty:false})),
  getDownload:vi.fn(async()=>({id:'11111111-1111-4111-8111-111111111111',status:state.status,files_done:1,files_total:1,bytes_written:100,volumes:[{number:1,status:state.status,bytes:100,delivery_kind:'zip'}]})),
  redeemDownload:state.redeem,
  cancelDownload:state.cancel,
  createSelection:vi.fn(),startDownload:vi.fn(),
 };
});
import {APIError,createSelection,startDownload,getDownload} from '../../portal/src/lib/api';
import {clearPreparation} from '../../portal/src/lib/download-preparation';
import {Portal} from '../../portal/src/components/Portal';
import {rememberDownloadJob,readDownloadJob} from '../../portal/src/lib/download-recovery';
const share='22222222-2222-4222-8222-222222222222';
beforeEach(()=>{
 vi.clearAllMocks();clearPreparation(sessionStorage,share);
 state.status='ready';state.redeem.mockReset();state.cancel.mockReset();sessionStorage.clear();
 Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});
 sessionStorage.setItem(`photo:portal:${share}:csrf`,'csrf');
 rememberDownloadJob(sessionStorage,share,'11111111-1111-4111-8111-111111111111');
});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
for(const status of [401,403,404,410]){
 for(const operation of ['volume','cancel'] as const){
  it(`clears scoped portal authority after ${operation} refusal ${status}`,async()=>{
   state.status=operation==='volume'?'ready':'building';
   const call=operation==='volume'?state.redeem:state.cancel;
   call.mockRejectedValue(new APIError(status,'DOWNLOAD_UNAVAILABLE'));
   render(<Portal locale="en" shareID={share}/>);
   await screen.findByText('Private collection');
   const button=await screen.findByText(operation==='volume'?'Download archive 1':'Cancel');
   fireEvent.click(button);
   await screen.findByText('This link is invalid or has expired.');
   expect(screen.queryByText('Private collection')).toBeNull();
   expect(screen.queryByText('Download archive 1')).toBeNull();
   expect(screen.queryByText('Cancel')).toBeNull();
   expect(readDownloadJob(sessionStorage,share)).toBeNull();
   expect(call).toHaveBeenCalledTimes(1);
   await act(async()=>{});expect(call).toHaveBeenCalledTimes(1);
  });
 }
}
it('retains a ready job after transient volume failure and does not automatically retry POST',async()=>{
 state.redeem.mockRejectedValue(new APIError(503,'DEPENDENCY_UNAVAILABLE'));
 render(<Portal locale="en" shareID={share}/>);
 fireEvent.click(await screen.findByText('Download archive 1'));
 await screen.findByText('Something went wrong. Please try again.');
 expect(screen.queryByText('Private collection')).not.toBeNull();
 expect(screen.queryByText('Download archive 1')).not.toBeNull();
 expect(readDownloadJob(sessionStorage,share)).not.toBeNull();
 expect(state.redeem).toHaveBeenCalledTimes(1);
});

it('preserves pending immutable preparation without reviving denied ready job on foreground',async()=>{
 const pending={revision:'1',preset:'33333333-3333-4333-8333-333333333333',
  selectionKey:'44444444-4444-4444-8444-444444444444',startKey:'55555555-5555-4555-8555-555555555555',
  snapshot:{id:'66666666-6666-4666-8666-666666666666',item_count:1}};
 const key=`photo:portal:${share}:download-preparation`;
 sessionStorage.setItem(key,JSON.stringify(pending));
 state.redeem.mockRejectedValue(new APIError(403,'DOWNLOAD_FORBIDDEN'));
 render(<Portal locale="en" shareID={share}/>);
 fireEvent.click(await screen.findByText('Download archive 1'));
 await screen.findByText('This link is invalid or has expired.');
 await act(async()=>{
  Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});
  document.dispatchEvent(new Event('visibilitychange'));
  Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});
  document.dispatchEvent(new Event('visibilitychange'));
 });
 expect(sessionStorage.getItem(key)).toBeNull();
 const guestKey=`photo:portal:${share}:guest:csrf:download-preparation`;
 expect(JSON.parse(sessionStorage.getItem(guestKey)!)).toEqual({...pending,csrf:'csrf'});
 expect(readDownloadJob(sessionStorage,share)).toBeNull();
 expect(screen.queryByText('Download archive 1')).toBeNull();
 expect(createSelection).not.toHaveBeenCalled();expect(startDownload).not.toHaveBeenCalled();
 expect(getDownload).toHaveBeenCalledTimes(1);expect(state.redeem).toHaveBeenCalledTimes(1);
});
