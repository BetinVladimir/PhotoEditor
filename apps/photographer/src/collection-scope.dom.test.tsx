// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
const id=vi.hoisted(()=>(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`);
import {APIError} from '@photoeditor/api-client';
import type {ReactNode} from 'react';
import {WorkspaceAccessContext} from './workspace-access-context';
const workspaceAccess={tenant:id(2),actor:'00000000-0000-4000-8000-000000000001',role:'editor' as const,canEdit:true,canAdmin:false};
function render(ui:ReactNode){return renderUI(ui,{wrapper:({children})=><WorkspaceAccessContext.Provider value={{...workspaceAccess,tenant:mocks.params.tenant}}>{children}</WorkspaceAccessContext.Provider>});}

import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render as renderUI,screen,waitFor,act} from '@testing-library/react';
const mocks=vi.hoisted(()=>({saved:null as any,access:vi.fn(),params:{id:id(9),tenant:id(2)},request:vi.fn(),snapshot:vi.fn(),sheet:vi.fn(),push:vi.fn(),media:vi.fn(),moderate:vi.fn(),label:vi.fn()}));
vi.mock('./collection-access-links',()=>({CollectionAccessLinks:()=>null}));
vi.mock('./collection-organization',()=>({CollectionOrganization:()=>null}));
vi.mock('./collection-bulk-controls',()=>({CollectionBulkControls:()=>null}));
vi.mock('./collection-metadata',()=>({CollectionMetadata:()=>null}));
vi.mock('./collection-culling-groups',()=>({CollectionCullingGroups:()=>null}));
vi.mock('./collection-guest-uploads',()=>({CollectionGuestUploads:()=>null}));
vi.mock('./gallery-comparison',()=>({GalleryComparison:()=>null}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
const client={request:mocks.request};const auth={origin:'https://collection.example',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=async()=>({id:id(11)});run=async(id:string)=>({id});projectAccess=mocks.access;mediaLinks=mocks.media;selectionSnapshot=mocks.snapshot;contactSheet=mocks.sheet;moderateApplyItem=mocks.moderate;setItemColorLabel=mocks.label;}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./contact-sheet-attempt-store',()=>({ContactSheetAttemptStore:class {async read(){return mocks.saved}async write(value:unknown){mocks.saved=structuredClone(value)}async clearAccepted(){mocks.saved=null}async discardRejected(){mocks.saved=null}}}));
import CollectionDetail from '../screens/collections/[id]/index';
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(r=>resolve=r);return {promise,resolve};}
afterEach(()=>{cleanup();workspaceAccess.canEdit=true;vi.clearAllMocks();vi.useRealTimers();mocks.params={id:id(9),tenant:id(2)};lifecycle.active=true;lifecycle.generation=0;mocks.saved=null;});
function load(){mocks.access.mockResolvedValue({id:id(3),can_edit:true});mocks.request.mockImplementation((path:string)=>Promise.resolve(path.includes('/items?')?{revision:'1',items:[{collection_id:mocks.params.id,tags:[],id:id(4),asset_id:id(5),asset_version_id:id(6),position:0,visibility:'visible',rating:0,flag:'none',created_at:'2026-10-17T10:00:00Z',moderation_state:'pending_review'}]}:{id:mocks.params.id,tenant_id:id(2),project_id:id(3),name:mocks.params.id,description:'',state:'draft',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z',revision:'1',source_run_id:id(7)}));mocks.media.mockResolvedValue({items:[]});}
it('changing collections suppresses late previews and resets visible collection',async()=>{
 load();const preview=deferred<{items:{variant:string;url:string}[]}>();mocks.media.mockReturnValueOnce(preview.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));
 mocks.params={id:id(10),tenant:id(2)};view.rerender(<CollectionDetail/>);await screen.findByText(id(10));
 await act(async()=>preview.resolve({items:[{variant:'thumb640',url:'https://old.test/preview'}]}));
 expect(screen.queryByText(id(9))).toBeNull();expect(document.querySelector('img[src="https://old.test/preview"]')).toBeNull();
});
it('a late snapshot cannot submit a contact sheet for the previous collection',async()=>{
 load();const snapshot=deferred<{id:string}>();mocks.snapshot.mockReturnValue(snapshot.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));
 fireEvent.click(screen.getByText('Create contact sheet'));fireEvent.click(screen.getByText('Creating contact sheet…'));await waitFor(()=>expect(mocks.snapshot).toHaveBeenCalledTimes(1));
 mocks.params={id:id(10),tenant:id(2)};view.rerender(<CollectionDetail/>);await screen.findByText(id(10));
 await act(async()=>snapshot.resolve({id:id(14)}));expect(mocks.sheet).not.toHaveBeenCalled();expect(mocks.push).not.toHaveBeenCalled();
});
it('a committed old contact sheet cannot navigate the new collection',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:id(8)});const sheet=deferred<{run_id:string;status?:'queued'}>();mocks.sheet.mockReturnValue(sheet.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));fireEvent.click(screen.getByText('Create contact sheet'));await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(1));
 mocks.params={id:id(10),tenant:id(2)};view.rerender(<CollectionDetail/>);await screen.findByText(id(10));
 await act(async()=>sheet.resolve({run_id:id(13),status:'queued'}));expect(mocks.push).not.toHaveBeenCalled();
});
it('reloads authoritative metadata after failed moderation without retrying the mutation',async()=>{
 load();mocks.moderate.mockRejectedValue(new Error('uncertain transport'));
 // Add the exact collection membership required by moderation preflight.
 const original=mocks.request.getMockImplementation()!;
 mocks.request.mockImplementation(async(path:string,...rest:unknown[])=>{const value=await original(path,...rest);return path.includes('/items?')?{revision:value.revision,items:value.items.map((item:object)=>({...item,collection_id:id(9)}))}:value;});
 render(<CollectionDetail/>);await screen.findByText(id(9));await waitFor(()=>expect(mocks.media).toHaveBeenCalled());
 await clickWhenEnabled(await screen.findByRole('button',{name:'Accept photo 1'}));await screen.findByText('Some decisions were not saved. Reload the collection before retrying.');expect(mocks.moderate).toHaveBeenCalledTimes(1);
 expect(screen.queryByLabelText('Accept photo 1')).toBeNull();
 const reload=screen.getByRole('button',{name:'Reload collection'});
 await waitFor(()=>expect(reload.hasAttribute('disabled')).toBe(false));
 const count=mocks.request.mock.calls.length;fireEvent.click(reload);await screen.findByText(id(9));await waitFor(()=>expect(mocks.request.mock.calls.length).toBe(count+2));expect(mocks.moderate).toHaveBeenCalledTimes(1);expect(await screen.findByRole('button',{name:'Accept photo 1'})).toBeDefined();
});
it('reloads the full collection after changing a color label',async()=>{
 load();mocks.label.mockResolvedValue({id:id(4),color_label:'Blue'});
 const original=mocks.request.getMockImplementation()!;let pages=0;
 mocks.request.mockImplementation(async(path:string,...rest:unknown[])=>{const value=await original(path,...rest);if(path.includes('/items?')){pages++;return {...value,items:value.items.map((item:object)=>({...item,color_label:pages>1?'Blue':undefined}))}}return value});
 render(<CollectionDetail/>);await screen.findByLabelText('Set Blue color label for photo 1');
 fireEvent.click(screen.getByLabelText('Set Blue color label for photo 1'));
 await waitFor(()=>expect(mocks.label).toHaveBeenCalledWith(expect.objectContaining({revision:'1'}),expect.objectContaining({id:id(4)}),'Blue',expect.any(AbortSignal)));
 await screen.findByLabelText('Color label Blue');expect(pages).toBe(2);
});

it('cancels contact sheet transport on background and ignores its late receipt',async()=>{
 load();const result=deferred<{run_id:string;status?:'queued'}>();mocks.snapshot.mockResolvedValue({id:id(8)});mocks.sheet.mockReturnValue(result.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));fireEvent.click(screen.getByText('Create contact sheet'));
 await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(1));
 const signal=mocks.sheet.mock.calls[0][4] as AbortSignal;
 expect(mocks.snapshot.mock.calls[0][2]).toBe(signal);
 lifecycle.active=false;lifecycle.generation++;view.rerender(<CollectionDetail/>);
 expect(signal.aborted).toBe(true);await act(async()=>result.resolve({run_id:'late'}));expect(mocks.push).not.toHaveBeenCalled();
});
it('does not read a collection while initially backgrounded',()=>{
 lifecycle.active=false;render(<CollectionDetail/>);expect(mocks.request).not.toHaveBeenCalled();
});

it('disables collection mutation actions for a project viewer',async()=>{
 load();mocks.access.mockResolvedValue({id:id(3),can_edit:false});render(<CollectionDetail/>);await screen.findByText(id(9));
 expect(screen.getByText('Apply style').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(screen.getByText('Create contact sheet').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(screen.queryByLabelText('Accept photo 1')).toBeNull();
 expect(mocks.label).not.toHaveBeenCalled();expect(mocks.sheet).not.toHaveBeenCalled();
});
it('clears collection metadata and previews when media access is revoked',async()=>{
 load();mocks.media.mockRejectedValue(new APIError(403,'FORBIDDEN'));
 render(<CollectionDetail/>);await screen.findByText('Collection access is unavailable');
 expect(screen.queryByText(id(9))).toBeNull();expect(screen.queryByText('Photo 1')).toBeNull();
});

it('restores uncertain contact sheet after remount with the same key and snapshot',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:id(8)});mocks.sheet.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({run_id:id(12),status:'queued'});
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));fireEvent.click(screen.getByText('Create contact sheet'));
 await screen.findByRole('alert');const original=mocks.sheet.mock.calls[0];view.unmount();
 render(<CollectionDetail/>);const retry=await screen.findByText('Retry contact sheet');
 await waitFor(()=>expectEnabled(retry));
 await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.sheet.mock.calls[1].slice(0,4)).toEqual(original.slice(0,4));expect(mocks.sheet.mock.calls[1][5]).toBe(original[5]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});

it('keeps an archived collection readable and disables fresh mutations',async()=>{
 load();const original=mocks.request.getMockImplementation()!;
 mocks.request.mockImplementation(async(path:string,...rest:unknown[])=>{const result=await original(path,...rest);return path.includes('/items?')?result:{...result,state:'archived'};});
 render(<CollectionDetail/>);await screen.findByText(id(9));
 expect(screen.getByText('Apply style').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(screen.getByText('Create contact sheet').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(await screen.findByLabelText('Open photo 1')).toBeDefined();
 const color=screen.queryByLabelText('Set Blue color label for photo 1');if(color){expect(color.getAttribute('aria-disabled')).toBe('true');fireEvent.click(color);}
 const accept=screen.queryByLabelText('Accept photo 1');if(accept){expect(accept.getAttribute('aria-disabled')).toBe('true');fireEvent.click(accept);}
 expect(mocks.label).not.toHaveBeenCalled();expect(mocks.sheet).not.toHaveBeenCalled();
});
it('blocks contact sheet and Apply navigation while moderation is in flight',async()=>{
 load();const decision=deferred<any>();mocks.moderate.mockReturnValue(decision.promise);
 render(<CollectionDetail/>);await screen.findByLabelText('Accept photo 1');
 fireEvent.click(screen.getByLabelText('Accept photo 1'));
 await waitFor(()=>expect(mocks.moderate).toHaveBeenCalledTimes(1));
 expect(screen.getByText('Apply style').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(screen.getByText('Create contact sheet').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 await act(async()=>decision.resolve({collection_id:id(9),id:id(4),asset_id:id(5),asset_version_id:id(6),moderation_state:'accepted'}));
 expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.push).not.toHaveBeenCalled();
});

it('does not show expired thumbnails or thumbnails bound to another version',async()=>{
 load();mocks.media.mockResolvedValue({items:[
  {variant:'thumb640',asset_version_id:'foreign',url:'https://media.test/foreign',expires_at:new Date(Date.now()+60000).toISOString()},
  {variant:'thumb640',asset_version_id:id(6),url:'https://media.test/expired',expires_at:new Date(Date.now()-1000).toISOString()},
 ]});
 render(<CollectionDetail/>);await screen.findByText(id(9));await waitFor(()=>expect(mocks.media).toHaveBeenCalled());
 expect(document.querySelector('img[src="https://media.test/foreign"]')).toBeNull();
 expect(document.querySelector('img[src="https://media.test/expired"]')).toBeNull();
});
it('removes an authorized thumbnail when its media link expires',async()=>{
 load();vi.useFakeTimers();mocks.media.mockResolvedValue({items:[{variant:'thumb640',asset_version_id:id(6),url:'https://media.test/current',expires_at:new Date(Date.now()+1000).toISOString()}]});
 render(<CollectionDetail/>);
 await act(async()=>{for(let i=0;i<20;i++)await Promise.resolve();});
 expect(document.querySelector('img[src="https://media.test/current"]')).not.toBeNull();
 await act(async()=>{vi.advanceTimersByTime(1000);});
 expect(document.querySelector('img[src="https://media.test/current"]')).toBeNull();
 expect(screen.getByText('Preview pending')).toBeDefined();
});

it('cancels an initial read, permits readonly retry and suppresses late metadata',async()=>{
 load();const stale=deferred<any>();mocks.request.mockReturnValueOnce(stale.promise);
 render(<CollectionDetail/>);
 await waitFor(()=>expect(mocks.request).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.request.mock.calls[0][1].signal as AbortSignal;
 fireEvent.click(screen.getByRole('button',{name:'Cancel loading'}));
 expect(oldSignal.aborted).toBe(true);
 expect(screen.getByRole('alert').textContent).toContain('cancelled');
 fireEvent.click(screen.getByRole('button',{name:'Reload collection'}));
 await screen.findByText(id(9));
 await act(async()=>stale.resolve({id:id(9),name:'STALE COLLECTION',project_id:id(3),state:'ready',revision:'1'}));
 expect(screen.queryByText('STALE COLLECTION')).toBeNull();
 expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.sheet).not.toHaveBeenCalled();
});

it('keeps collection actions usable while thumbnail transport is stalled',async()=>{
 load();mocks.media.mockReturnValue(new Promise(()=>{}));
 render(<CollectionDetail/>);await screen.findByText(id(9));
 await waitFor(()=>expect(mocks.media).toHaveBeenCalled());
 expectEnabled(screen.getByRole('button',{name:'Reload collection'}));
 expect(screen.queryByRole('button',{name:'Cancel loading'})).toBeNull();
 expect(await screen.findByLabelText('Open photo 1')).toBeDefined();
});

it('times out metadata even if transport ignores abort and ignores its late response',async()=>{
 vi.useFakeTimers();load();const stale=deferred<any>();mocks.request.mockReturnValueOnce(stale.promise);
 render(<CollectionDetail/>);
 await act(async()=>{for(let n=0;n<20;n++)await Promise.resolve();});
 expect(mocks.request).toHaveBeenCalledTimes(1);
 const oldSignal=mocks.request.mock.calls[0][1].signal as AbortSignal;
 await act(async()=>{await vi.advanceTimersByTimeAsync(8001);});
 expect(oldSignal.aborted).toBe(true);
 expect(screen.getByRole('alert').textContent).toContain('Unable to load collection');
 await act(async()=>stale.resolve({id:id(9),name:'LATE COLLECTION',project_id:id(3),state:'ready',revision:'1'}));
 expect(screen.queryByText('LATE COLLECTION')).toBeNull();
 expect(mocks.access).not.toHaveBeenCalled();
 expect(mocks.sheet).not.toHaveBeenCalled();
});

it('cancels uncertain contact sheet confirmation and retries the persisted exact request',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:id(8)});const old=deferred<{run_id:string;status?:'queued'}>();
 mocks.sheet.mockReturnValueOnce(old.promise).mockResolvedValueOnce({run_id:id(12),status:'queued'});
 render(<CollectionDetail/>);await screen.findByText(id(9));
 fireEvent.click(screen.getByText('Create contact sheet'));
 await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(1));
 const first=mocks.sheet.mock.calls[0],saved=structuredClone(mocks.saved);
 fireEvent.click(screen.getByRole('button',{name:'Cancel contact sheet confirmation'}));
 await screen.findByText('Retry contact sheet');
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Cancel contact sheet confirmation'})).toBeNull());
 expect((first[4] as AbortSignal).aborted).toBe(true);expect(mocks.saved).toEqual(saved);
 fireEvent.click(screen.getByText('Retry contact sheet'));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.sheet.mock.calls[1].slice(0,4)).toEqual(first.slice(0,4));
 expect(mocks.sheet.mock.calls[1][5]).toBe(first[5]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 await act(async()=>old.resolve({run_id:id(16),status:'queued'}));
 expect(mocks.push).toHaveBeenCalledTimes(1);expect(mocks.saved).toBeNull();
});

it('bounds uncertain contact sheet transport without discarding its journal',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:id(8)});const pending=deferred<{run_id:string;status?:'queued'}>();mocks.sheet.mockReturnValue(pending.promise);
 render(<CollectionDetail/>);await screen.findByText(id(9));
 vi.useFakeTimers();fireEvent.click(screen.getByText('Create contact sheet'));
 await act(async()=>{for(let n=0;n<30;n++)await Promise.resolve();});
 expect(mocks.sheet).toHaveBeenCalledTimes(1);const saved=structuredClone(mocks.saved);
 await act(async()=>{await vi.advanceTimersByTimeAsync(15001);});
 expect((mocks.sheet.mock.calls[0][4] as AbortSignal).aborted).toBe(true);
 expect(screen.queryByRole('button',{name:'Cancel contact sheet confirmation'})).toBeNull();
 expect(screen.getByRole('alert').textContent).toContain('could not be confirmed');expect(mocks.saved).toEqual(saved);
 await act(async()=>pending.resolve({run_id:id(15),status:'queued'}));
 expect(mocks.push).not.toHaveBeenCalled();expect(mocks.saved).toEqual(saved);
});

it('cancels color label confirmation and requires readonly reconciliation before another write',async()=>{
 load();const old=deferred<any>();mocks.label.mockReturnValue(old.promise);
 render(<CollectionDetail/>);await screen.findByLabelText('Set Blue color label for photo 1');
 fireEvent.click(screen.getByLabelText('Set Blue color label for photo 1'));
 await waitFor(()=>expect(mocks.label).toHaveBeenCalledTimes(1));
 fireEvent.click(screen.getByRole('button',{name:'Cancel decision confirmation'}));
 await screen.findByText('Color label could not be confirmed. Reload the collection before retrying.');
 expect((mocks.label.mock.calls[0][3] as AbortSignal).aborted).toBe(true);
 expect(screen.queryByLabelText('Set Blue color label for photo 1')).toBeNull();
 const reads=mocks.request.mock.calls.length;
 await act(async()=>old.resolve({id:id(4),color_label:'Blue'}));
 expect(mocks.request).toHaveBeenCalledTimes(reads);expect(screen.queryByLabelText('Color label Blue')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Reload collection'}));await screen.findByText(id(9));
 expect(mocks.label).toHaveBeenCalledTimes(1);
});

it('times out moderation without publishing a late success or automatically resubmitting',async()=>{
 load();const old=deferred<any>();mocks.moderate.mockReturnValue(old.promise);
 render(<CollectionDetail/>);await screen.findByLabelText('Accept photo 1');
 vi.useFakeTimers();fireEvent.click(screen.getByLabelText('Accept photo 1'));
 await act(async()=>{for(let n=0;n<20;n++)await Promise.resolve();});
 expect(mocks.moderate).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(15001);});
 expect((mocks.moderate.mock.calls[0][3] as AbortSignal).aborted).toBe(true);
 expect(screen.getByRole('alert').textContent).toContain('Decisions could not be confirmed');
 expect(screen.queryByLabelText('Accept photo 1')).toBeNull();
 await act(async()=>old.resolve({id:id(4),collection_id:id(9),asset_id:id(5),asset_version_id:id(6),moderation_state:'accepted'}));
 expect(screen.queryByLabelText('Accept photo 1')).toBeNull();expect(mocks.moderate).toHaveBeenCalledTimes(1);
});


it('requires workspace edit authority even when project access allows editing',async()=>{
 workspaceAccess.canEdit=false;load();
 render(<CollectionDetail/>);await screen.findByText(id(9));
 const button=screen.getByText('Create contact sheet').closest('[role="button"]')!;
 expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
 expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.sheet).not.toHaveBeenCalled();
});

it('revoking workspace editing aborts a pending snapshot before contact sheet submission',async()=>{
 load();const snapshot=deferred<{id:string}>();mocks.snapshot.mockReturnValue(snapshot.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));
 fireEvent.click(screen.getByText('Create contact sheet'));
 await waitFor(()=>expect(mocks.snapshot).toHaveBeenCalledTimes(1));
 const signal=mocks.snapshot.mock.calls[0][2] as AbortSignal;
 workspaceAccess.canEdit=false;view.rerender(<CollectionDetail/>);
 await waitFor(()=>expect(signal.aborted).toBe(true));
 await act(async()=>snapshot.resolve({id:id(17)}));
 expect(mocks.sheet).not.toHaveBeenCalled();expect(mocks.push).not.toHaveBeenCalled();
});
it('revoking workspace editing ignores a late contact sheet receipt and retains its pending identity',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:id(8)});
 const receipt=deferred<{run_id:string;status?:'queued'}>();mocks.sheet.mockReturnValue(receipt.promise);
 const view=render(<CollectionDetail/>);await screen.findByText(id(9));
 fireEvent.click(screen.getByText('Create contact sheet'));
 await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(1));
 const key=mocks.saved.key,signal=mocks.sheet.mock.calls[0][4] as AbortSignal;
 workspaceAccess.canEdit=false;view.rerender(<CollectionDetail/>);
 await waitFor(()=>expect(signal.aborted).toBe(true));
 await act(async()=>receipt.resolve({run_id:id(18),status:'queued'}));
 expect(mocks.push).not.toHaveBeenCalled();expect(mocks.saved).toMatchObject({key,ticket:null});
 expect(mocks.sheet).toHaveBeenCalledTimes(1);
});

it('role withdrawal clears old label busy state and old completion cannot unlock a fresh label write',async()=>{
 load();const old=deferred<any>(),fresh=deferred<any>();mocks.label.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
 const view=render(<CollectionDetail/>);await screen.findByLabelText('Set Blue color label for photo 1');fireEvent.click(screen.getByLabelText('Set Blue color label for photo 1'));await waitFor(()=>expect(mocks.label).toHaveBeenCalledOnce());
 workspaceAccess.canEdit=false;view.rerender(<CollectionDetail/>);await waitFor(()=>expect((mocks.label.mock.calls[0][3] as AbortSignal).aborted).toBe(true));
 workspaceAccess.canEdit=true;view.rerender(<CollectionDetail/>);await waitFor(()=>expectEnabled(screen.getByLabelText('Set Red color label for photo 1')));
 await clickWhenEnabled(screen.getByLabelText('Set Red color label for photo 1'));await waitFor(()=>expect(mocks.label).toHaveBeenCalledTimes(2));
 await act(async()=>old.resolve({id:id(4),color_label:'Blue'}));fireEvent.click(screen.getByLabelText('Set Green color label for photo 1'));expect(mocks.label).toHaveBeenCalledTimes(2);
 expect(screen.getByLabelText('Set Green color label for photo 1').getAttribute('aria-disabled')).toBe('true');
 await act(async()=>fresh.resolve({id:id(4),color_label:'Red'}));
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});

it('shows a thumbnail after MEDIA_NOT_READY without reloading collection metadata',async()=>{
 load();vi.useFakeTimers();mocks.media.mockRejectedValueOnce(new APIError(409,'MEDIA_NOT_READY')).mockResolvedValue({items:[{variant:'thumb640',asset_version_id:id(6),url:'https://media.test/ready-after-retry',expires_at:new Date(Date.now()+60000).toISOString()}]});
 render(<CollectionDetail/>);
 await act(async()=>{for(let i=0;i<30;i++)await Promise.resolve();});
 expect(mocks.media).toHaveBeenCalledTimes(1);const metadataReads=mocks.request.mock.calls.length;
 await act(async()=>{await vi.advanceTimersByTimeAsync(1000);});
 expect(mocks.media).toHaveBeenCalledTimes(2);
 expect(document.querySelector('img[src="https://media.test/ready-after-retry"]')).not.toBeNull();
 expect(mocks.request).toHaveBeenCalledTimes(metadataReads);
});
it('refreshes an expired signed thumbnail and cancels further refreshes on unmount',async()=>{
 load();vi.useFakeTimers();mocks.media.mockResolvedValueOnce({items:[{variant:'thumb640',asset_version_id:id(6),url:'https://media.test/first-link',expires_at:new Date(Date.now()+1000).toISOString()}]}).mockResolvedValue({items:[{variant:'thumb640',asset_version_id:id(6),url:'https://media.test/second-link',expires_at:new Date(Date.now()+60000).toISOString()}]});
 const view=render(<CollectionDetail/>);
 await act(async()=>{for(let i=0;i<30;i++)await Promise.resolve();});
 expect(document.querySelector('img[src="https://media.test/first-link"]')).not.toBeNull();
 await act(async()=>{await vi.advanceTimersByTimeAsync(1000);});
 expect(mocks.media).toHaveBeenCalledTimes(2);
 expect(document.querySelector('img[src="https://media.test/first-link"]')).toBeNull();
 expect(document.querySelector('img[src="https://media.test/second-link"]')).not.toBeNull();
 view.unmount();await act(async()=>{await vi.advanceTimersByTimeAsync(120000);});expect(mocks.media).toHaveBeenCalledTimes(2);
});

it.each([new Error('network unavailable'),new APIError(503,'UNAVAILABLE'),new APIError(429,'RATE_LIMITED')])('retries transient preview failures without blocking collection actions: %s',async failure=>{
 load();vi.useFakeTimers();mocks.media.mockRejectedValueOnce(failure).mockResolvedValue({items:[{variant:'thumb640',asset_version_id:id(6),url:'https://media.test/recovered-transient',expires_at:new Date(Date.now()+60000).toISOString()}]});
 render(<CollectionDetail/>);await act(async()=>{for(let i=0;i<30;i++)await Promise.resolve();});
 expect(mocks.media).toHaveBeenCalledTimes(1);expectEnabled(screen.getByRole('button',{name:'Reload collection'}));
 await act(async()=>{await vi.advanceTimersByTimeAsync(1000);});
 expect(mocks.media).toHaveBeenCalledTimes(2);expect(document.querySelector('img[src="https://media.test/recovered-transient"]')).not.toBeNull();
});

// Display sequence must follow the loaded page order, not sparse business sort keys.
it('numbers sparse paginated photos consecutively and preserves editor identity',async()=>{
 load();const original=mocks.request.getMockImplementation()!;
 mocks.request.mockImplementation(async(path:string,...rest:unknown[])=>{
  const value=await original(path,...rest);if(!path.includes('/items?'))return value;
  const second=path.includes('cursor=second-page');
  return {revision:'1',items:[{...value.items[0],id:id(second?14:4),asset_id:id(second?15:5),asset_version_id:id(second?16:6),position:second?2048:1024}],next_cursor:second?undefined:'second-page'};
 });
 render(<CollectionDetail/>);
 await screen.findByLabelText('Open photo 2');expect(screen.getByText('Photo 1')).toBeDefined();expect(screen.getByText('Photo 2')).toBeDefined();
 expect(screen.queryByText('Photo 1025')).toBeNull();expect(screen.queryByText('Photo 2049')).toBeNull();
 expect(screen.getByLabelText('Set Blue color label for photo 2')).toBeDefined();expect(screen.getByLabelText('Accept photo 2')).toBeDefined();
 fireEvent.click(screen.getByLabelText('Open photo 2'));
 expect(mocks.push).toHaveBeenLastCalledWith({pathname:'/editor/[version]',params:{version:id(16),asset:id(15),tenant:id(2),collection:id(9),item:id(14)}});
 expect(mocks.request.mock.calls.some(([path])=>String(path).includes('cursor=second-page'))).toBe(true);
});
