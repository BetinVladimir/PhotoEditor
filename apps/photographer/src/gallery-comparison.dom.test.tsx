// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError,canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import en from '../../../locales/en/common.json';
import type {CollectionItem,EditorDraft,MediaLink} from './catalog-types';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {GalleryComparison} from './gallery-comparison';
afterEach(()=>{cleanup();vi.useRealTimers();});
const icc='b'.repeat(64),source='a'.repeat(64);
const profile=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON({schema_version:1,variant:'preview1600',long_edge:1600,quality:88,icc_sha256:icc,no_upscale:true,metadata_policy:'stripped'}))));
const items=Array.from({length:7},(_,i)=>({id:'item'+i,asset_id:'asset'+i,asset_version_id:'version'+i,position:i} as CollectionItem));
const draft=(asset:string)=>({asset_version_id:asset.replace('asset','version'),source_sha256:source} as EditorDraft);
const preview=(version:string):MediaLink=>({asset_version_id:version,artifact_sha256:'c'.repeat(64),icc_sha256:icc,profile_hash:profile,variant:'preview1600',url:'https://media.test/'+version,expires_at:new Date(Date.now()+600000).toISOString(),width:1600,height:1000,media_type:'image/jpeg'});
function api(){return {draft:vi.fn(async(asset:string)=>draft(asset)),mediaLinks:vi.fn(async(_asset:string,version:string,_variants:MediaLink['variant'][],_signal?:AbortSignal)=>({items:[preview(version)]}))};}
it('loads two authorized previews and hides them immediately on clear',async()=>{
 const owner=api();render(<GalleryComparison items={items} api={owner}/>);
 fireEvent.click(screen.getByLabelText('Select frame 1'));expect(owner.draft).not.toHaveBeenCalled();fireEvent.click(screen.getByLabelText('Select frame 2'));
 await screen.findByText(en.compare_local_decisions);expect(owner.mediaLinks).toHaveBeenCalledWith('asset0','version0',['preview1600'],expect.any(AbortSignal));
 fireEvent.click(screen.getByText(en.compare_clear));expect(screen.queryByText(en.compare_local_decisions)).toBeNull();
});
it('ignores late previews after selection change even if transport ignores abort',async()=>{
 const owner=api();let release!:()=>void;const gate=new Promise<void>(resolve=>release=resolve);owner.mediaLinks.mockImplementation(async(_asset,version)=>{await gate;return {items:[preview(version)]};});
 render(<GalleryComparison items={items} api={owner}/>);fireEvent.click(screen.getByLabelText('Select frame 1'));fireEvent.click(screen.getByLabelText('Select frame 2'));
 await waitFor(()=>expect(owner.mediaLinks).toHaveBeenCalledTimes(2));fireEvent.click(screen.getByText(en.compare_clear));await act(async()=>release());expect(screen.queryByText(en.compare_local_decisions)).toBeNull();
 expect(owner.mediaLinks.mock.calls.every(call=>call[3]?.aborted)).toBe(true);
});
it('rejects changed version binding and enforces six frame selection limit',async()=>{
 const owner=api();owner.draft.mockResolvedValue({...draft('asset0'),asset_version_id:'foreign'});
 render(<GalleryComparison items={items} api={owner}/>);for(let i=1;i<=6;i++)fireEvent.click(screen.getByLabelText('Select frame '+i));
 expect(screen.getByLabelText('Select frame 7').getAttribute('aria-disabled')).toBe('true');await screen.findByText(en.compare_unavailable);expect(screen.queryByText(en.compare_local_decisions)).toBeNull();
});
it('removes comparison when the earliest authorized preview expires',async()=>{
 vi.useFakeTimers();const owner=api();owner.mediaLinks.mockImplementation(async(_asset,version)=>({items:[{...preview(version),expires_at:new Date(Date.now()+1000).toISOString()}]}));
 render(<GalleryComparison items={items} api={owner}/>);fireEvent.click(screen.getByLabelText('Select frame 1'));fireEvent.click(screen.getByLabelText('Select frame 2'));
 await act(async()=>{await Promise.resolve();await Promise.resolve();});expect(screen.getByText(en.compare_local_decisions)).toBeDefined();
 await act(async()=>vi.advanceTimersByTime(1000));expect(screen.queryByText(en.compare_local_decisions)).toBeNull();expect(screen.getByText(en.compare_unavailable)).toBeDefined();
});

it.each([401,403])('reports preview access loss %s and cancels sibling reads',async status=>{
 const owner=api(),denied=vi.fn(),error=new APIError(status,'ACCESS_UNAVAILABLE');
 owner.mediaLinks.mockRejectedValue(error);
 render(<GalleryComparison items={items} api={owner} onAccessDenied={denied}/>);
 fireEvent.click(screen.getByLabelText('Select frame 1'));fireEvent.click(screen.getByLabelText('Select frame 2'));
 await waitFor(()=>expect(denied).toHaveBeenCalledWith(error));
 expect(denied).toHaveBeenCalledTimes(1);
 expect(owner.mediaLinks.mock.calls.every(call=>call[3]?.aborted)).toBe(true);
 expect(screen.queryByText(en.compare_local_decisions)).toBeNull();
});
it('keeps missing derivative separate from revoked collection access',async()=>{
 const owner=api(),denied=vi.fn();owner.mediaLinks.mockRejectedValue(new APIError(404,'NOT_FOUND'));
 render(<GalleryComparison items={items} api={owner} onAccessDenied={denied}/>);
 fireEvent.click(screen.getByLabelText('Select frame 1'));fireEvent.click(screen.getByLabelText('Select frame 2'));
 await screen.findByText(en.compare_unavailable);expect(denied).not.toHaveBeenCalled();
});
