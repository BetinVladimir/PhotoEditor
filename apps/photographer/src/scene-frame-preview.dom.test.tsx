// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {render,screen,cleanup,act,waitFor,fireEvent} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import en from '../../../locales/en/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {SceneFramePreview} from './scene-frame-preview';
const frame={ordinal:0,assetId:'asset',versionId:'version',sourceSha:'a'.repeat(64)};
const link=()=>({asset_version_id:'version',variant:'thumb320',media_type:'image/jpeg',url:'https://example.test/signed.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:320,height:200});
afterEach(()=>{cleanup();vi.useRealTimers();});
it('loads only the frozen version thumbnail and removes its expired URL',async()=>{vi.useFakeTimers();const mediaLinks=vi.fn().mockResolvedValue({items:[link()]});render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await act(async()=>{});expect(mediaLinks).toHaveBeenCalledWith('asset','version',['thumb320'],expect.any(AbortSignal));expect(screen.getByRole('img')).toBeTruthy();await act(async()=>vi.advanceTimersByTime(60001));expect(screen.queryByRole('img')).toBeNull();expect(screen.getByText(en.scene_preview_unavailable)).toBeTruthy();});
it('refuses rebound, expired and active-content URLs',async()=>{for(const patch of [{asset_version_id:'other'},{expires_at:new Date(0).toISOString()},{url:'javascript:alert(1)'},{url:'http://example.test/signed.jpg'},{url:'https://example.test/signed.jpg#fragment'},{width:1600}]){const mediaLinks=vi.fn().mockResolvedValue({items:[{...link(),...patch}]});const view=render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await screen.findByText(en.scene_preview_unavailable);expect(screen.queryByRole('img')).toBeNull();view.unmount();}});
it('shows a local object-store thumbnail',async()=>{const mediaLinks=vi.fn().mockResolvedValue({items:[{...link(),url:'http://127.0.0.1:9000/signed.jpg'}]});render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);expect(await screen.findByRole('img')).toBeTruthy();});
it('aborts unmounted cells and suppresses late links',async()=>{let resolve!:(v:unknown)=>void;const mediaLinks=vi.fn().mockReturnValue(new Promise(r=>resolve=r));const view=render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await waitFor(()=>expect(mediaLinks).toHaveBeenCalledTimes(1));const signal=mediaLinks.mock.calls[0][3] as AbortSignal;view.unmount();await act(async()=>resolve({items:[link()]}));expect(signal.aborted).toBe(true);expect(screen.queryByRole('img')).toBeNull();});
it('reports revoked access to the board owner',async()=>{const denied=vi.fn(),mediaLinks=vi.fn().mockRejectedValue(new APIError(403,'FORBIDDEN'));render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame} onAccessDenied={denied}/>);await waitFor(()=>expect(denied).toHaveBeenCalledTimes(1));expect(screen.queryByRole('img')).toBeNull();});
it('drops the completed old image when a virtual cell changes version',async()=>{const mediaLinks=vi.fn().mockResolvedValueOnce({items:[link()]}).mockReturnValueOnce(new Promise(()=>{})),api={mediaLinks} as unknown as OwnerAPI;const view=render(<SceneFramePreview api={api} frame={frame}/>);await screen.findByRole('img');view.rerender(<SceneFramePreview api={api} frame={{...frame,versionId:'other'}}/>);expect(screen.queryByRole('img')).toBeNull();expect(mediaLinks).toHaveBeenLastCalledWith('asset','other',['thumb320'],expect.any(AbortSignal));});
it('removes scheduled expiry timers on unmount',async()=>{vi.useFakeTimers();const mediaLinks=vi.fn().mockResolvedValue({items:[link()]});const view=render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await act(async()=>{});expect(vi.getTimerCount()).toBeGreaterThan(0);view.unmount();expect(vi.getTimerCount()).toBe(0);});

it('refreshes an expired thumbnail without rebinding its frozen version',async()=>{
 vi.useFakeTimers();const mediaLinks=vi.fn().mockImplementation(async(_asset:string,_version:string,_variants:string[],_signal:AbortSignal)=>({items:[link()]}));render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await act(async()=>{});await act(async()=>vi.advanceTimersByTimeAsync(60001));expect(screen.queryByRole('img')).toBeNull();
 fireEvent.click(screen.getByText(en.scene_preview_refresh));await act(async()=>{});expect(screen.getByRole('img')).toBeTruthy();expect(mediaLinks.mock.calls.map(call=>call.slice(0,3))).toEqual([['asset','version',['thumb320']],['asset','version',['thumb320']]]);
});
it('does not let an old same-frame image error clear the refreshed link',async()=>{
 const mediaLinks=vi.fn().mockResolvedValue({items:[link()]}),api={mediaLinks} as unknown as OwnerAPI;render(<SceneFramePreview api={api} frame={frame}/>);const old=await screen.findByRole('img');fireEvent.click(screen.getByText(en.scene_preview_refresh));await waitFor(()=>expect(screen.getByRole('img')).not.toBe(old));fireEvent.error(old);expect(screen.getByRole('img')).toBeTruthy();
});
it('bounds an ignored thumbnail transport and provides explicit refresh',async()=>{
 vi.useFakeTimers();const mediaLinks=vi.fn(()=>new Promise<never>(()=>{}));render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await act(async()=>vi.advanceTimersByTimeAsync(8000));expect(screen.getByText(en.scene_preview_unavailable)).toBeTruthy();expect(screen.getByText(en.scene_preview_refresh)).toBeTruthy();
});
it('cancels an initial thumbnail and refuses its late URL',async()=>{
 let finish!:(value:unknown)=>void;const mediaLinks=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({items:[link()]});render(<SceneFramePreview api={{mediaLinks} as unknown as OwnerAPI} frame={frame}/>);await waitFor(()=>expect(mediaLinks).toHaveBeenCalledOnce());fireEvent.click(screen.getByText(en.read_cancel));await act(async()=>finish({items:[link()]}));expect(screen.queryByRole('img')).toBeNull();fireEvent.click(screen.getByText(en.scene_preview_refresh));await screen.findByRole('img');
});
