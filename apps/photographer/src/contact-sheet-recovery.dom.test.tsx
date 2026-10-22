// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render as renderUI,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({data:new Map<string,string>(),sheet:vi.fn(),snapshot:vi.fn(),run:vi.fn(),push:vi.fn(),failWrite:false,request:vi.fn()}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:id(3),tenant:id(2)}),router:{push:mocks.push}}));
const client={request:mocks.request},auth={origin:'https://collection-recovery.example',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./gallery-comparison',()=>({GalleryComparison:()=>null}));
vi.mock('./collection-bulk-controls',()=>({CollectionBulkControls:()=>null}));
vi.mock('./collection-organization',()=>({CollectionOrganization:()=>null}));
vi.mock('./collection-access-links',()=>({CollectionAccessLinks:()=>null}));
vi.mock('./collection-guest-uploads',()=>({CollectionGuestUploads:()=>null}));
vi.mock('./collection-metadata',()=>({CollectionMetadata:()=>null}));
vi.mock('./collection-gallery',()=>({CollectionGallery:({children,items}:{children:(items:unknown[])=>unknown;items:unknown[]})=>children(items)}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.data.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failWrite)throw new Error('disk unavailable');mocks.data.set(k,v)},removeItem:async(k:string)=>{mocks.data.delete(k)},getAllKeys:async()=>[...mocks.data.keys()]}}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=async()=>({id:id(1)});projectAccess=async()=>({id:id(4),can_edit:true});mediaLinks=async()=>({items:[]});selectionSnapshot=mocks.snapshot;contactSheet=mocks.sheet;run=mocks.run;}}));
import CollectionDetail from '../screens/collections/[id]/index';
import type {ReactNode} from 'react';
import {WorkspaceAccessContext} from './workspace-access-context';
let canEdit=true;
function render(ui:ReactNode){return renderUI(ui,{wrapper:({children})=><WorkspaceAccessContext.Provider value={{tenant:id(2),actor:id(1),role:canEdit?'editor':'viewer',canEdit,canAdmin:false}}>{children}</WorkspaceAccessContext.Provider>});}
const journalKey='photo.contact-sheet-attempt.v1:'+JSON.stringify([auth.origin,id(1),id(2),id(3)]);
afterEach(()=>{cleanup();canEdit=true;mocks.data.clear();mocks.failWrite=false;vi.resetAllMocks()});
function prepare(){mocks.snapshot.mockResolvedValue({id:id(7)});mocks.run.mockImplementation(async(run:string)=>({id:run}));mocks.request.mockImplementation(async(path:string)=>path.includes('/items?')?{revision:'1',items:[{id:id(5),collection_id:id(3),asset_id:id(6),asset_version_id:id(8),tags:[],position:0,moderation_state:'accepted',visibility:'hidden',rating:0,flag:'none',created_at:'2026-10-17T12:00:00Z'}]}:{id:id(3),tenant_id:id(2),project_id:id(4),name:'Shoot',description:'',sort_mode:'manual',created_at:'2026-10-17T12:00:00Z',revision:'1',state:'draft'});}
async function retry(){const button=await screen.findByText('Retry contact sheet');await waitFor(()=>expectEnabled(button));await clickWhenEnabled(button)}
it('persists before POST and replays exact snapshot/key after remount',async()=>{
 prepare();mocks.sheet.mockImplementationOnce(async()=>{expect(JSON.parse(mocks.data.get(journalKey)!)).toMatchObject({ticket:null,request:{selection_snapshot_id:id(7)}});throw new Error('offline')}).mockResolvedValueOnce({run_id:id(9),status:'queued'});
 const view=render(<CollectionDetail/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');
 const original=mocks.sheet.mock.calls[0];view.unmount();render(<CollectionDetail/>);await retry();
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.sheet.mock.calls[1][5]).toBe(original[5]);expect(mocks.sheet.mock.calls[1].slice(0,4)).toEqual(original.slice(0,4));expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 await waitFor(()=>expect(mocks.data.has(journalKey)).toBe(false));
});
it('recovers accepted run after verification failure without a second POST',async()=>{
 prepare();mocks.sheet.mockResolvedValue({run_id:id(9),status:'queued'});mocks.run.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({id:id(9)});
 const view=render(<CollectionDetail/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');
 expect(JSON.parse(mocks.data.get(journalKey)!)).toMatchObject({ticket:{run_id:id(9)}});view.unmount();render(<CollectionDetail/>);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.sheet).toHaveBeenCalledTimes(1);expect(mocks.run).toHaveBeenCalledTimes(2);
});
it('does not send POST when pre-send persistence fails',async()=>{
 prepare();render(<CollectionDetail/>);await screen.findByText('Shoot');mocks.failWrite=true;fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');expect(mocks.sheet).not.toHaveBeenCalled();expect(mocks.data.has(journalKey)).toBe(false);
});

it('retains a known accepted run when its local receipt write fails and retries only GET',async()=>{
 prepare();mocks.sheet.mockImplementation(async()=>{mocks.failWrite=true;return {run_id:id(9),status:'queued'}});
 render(<CollectionDetail/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');
 expect(JSON.parse(mocks.data.get(journalKey)!)).toMatchObject({ticket:null});mocks.failWrite=false;
 await retry();await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.sheet).toHaveBeenCalledTimes(1);expect(mocks.run).toHaveBeenCalledWith(id(9),expect.anything());
});

it('does not erase an unknown prior admission when its retry receives a generic conflict',async()=>{
 const {APIError}=await import('@photoeditor/api-client');prepare();
 mocks.sheet.mockRejectedValueOnce(new Error('response lost')).mockRejectedValueOnce(new APIError(409,'CONTACT_SHEET_RESOURCE_STALE'));
 render(<CollectionDetail/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');
 const frozen=mocks.data.get(journalKey);await retry();await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('could not be confirmed'));
 expect(mocks.data.get(journalKey)).toBe(frozen);expect(mocks.sheet.mock.calls[1][5]).toBe(mocks.sheet.mock.calls[0][5]);
});

it('closes an unknown attempt with the original body/key and admits no replacement until confirmed',async()=>{
 prepare();mocks.sheet.mockRejectedValueOnce(new Error('response lost'));
 const read=mocks.request.getMockImplementation()!;
 mocks.request.mockImplementation(async(path:string,options:unknown)=>path.endsWith('/contact-sheets/abandon')?{status:'abandoned',key:JSON.parse(mocks.data.get(journalKey)!).key,selection_snapshot_id:id(7)}:read(path,options));
 render(<CollectionDetail/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('Create contact sheet'));await screen.findByRole('alert');
 const saved=JSON.parse(mocks.data.get(journalKey)!);fireEvent.click(screen.getByText('Close pending contact sheet'));
 await waitFor(()=>expect(mocks.data.has(journalKey)).toBe(false));
 expect(mocks.request).toHaveBeenCalledWith(expect.stringContaining('/contact-sheets/abandon'),expect.objectContaining({body:saved.request,idempotencyKey:saved.key}));
 expect(mocks.sheet).toHaveBeenCalledTimes(1);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});

it('opens an acknowledged contact sheet after workspace edit permission is revoked',async()=>{
 prepare();canEdit=false;
 mocks.data.set(journalKey,JSON.stringify({schema_version:1,collection_id:id(3),key:id(10),request:{selection_snapshot_id:id(7),locale:'en',columns:4,rows:5},ticket:{run_id:id(9)}}));
 render(<CollectionDetail/>);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.sheet).not.toHaveBeenCalled();expect(mocks.snapshot).not.toHaveBeenCalled();
});

it('refuses malformed collection and item metadata before exposing owner actions',async()=>{
 for(const failure of ['collection','item']){
  prepare();const original=mocks.request.getMockImplementation()!;
  mocks.request.mockImplementation(async(path:string,...args:unknown[])=>{const value=await original(path,...args);return path.includes('/items?')?failure==='item'?{...value,items:value.items.map((item:unknown)=>({...item as object,position:9007199254740992}))}:value:failure==='collection'?{...value,tenant_id:id(99)}:value;});
  const view=render(<CollectionDetail/>);await screen.findByText('Unable to load collection');const action=screen.queryByText('Create contact sheet');if(action)expect(action.closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');expect(mocks.sheet).not.toHaveBeenCalled();view.unmount();
 }
});
