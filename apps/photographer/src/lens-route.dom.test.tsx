import {clickWhenEnabled} from './test/ui-readiness';
// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,beforeEach,it,expect,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const h=vi.hoisted(()=>{const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;const values=new Map<string,string>();return {id,values,collection:{id:id(4),tenant_id:id(2),project_id:id(3),revision:'1'},item:{id:id(8),collection_id:id(4),asset_id:id(9),asset_version_id:id(5)},binding:{asset_version_id:id(5),source:{artifact_id:id(6),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},profile:{profile:{artifact_id:id(7),sha256:'c'.repeat(64)},camera_model:'Camera',lens_model:'Lens',focal_mm:50,aperture:2},projectCanEdit:true,request:vi.fn(),snapshot:vi.fn(),submit:vi.fn()};});
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>h.values.get(k)??null,setItem:async(k:string,v:string)=>{h.values.set(k,v);},getAllKeys:async()=>[...h.values.keys()],removeItem:async(k:string)=>{h.values.delete(k);}}}));
vi.mock('./localized-router',()=>({router:{push:vi.fn()},useLocalSearchParams:()=>({id:h.id(4),tenant:h.id(2)})}));
vi.mock('./auth-context',()=>{const client={request:h.request},auth={origin:'https://lens-route.test',client:()=>client};return {useAuth:()=>auth};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:1})}));
vi.mock('./owner-api',()=>({OwnerAPI:class {projectAccess=async(id:string)=>({id,can_edit:h.projectCanEdit});me=async()=>({id:h.id(1)});createRenderInputSnapshot=h.snapshot;}}));
vi.mock('./lens-client',async(importOriginal)=>{const actual=await importOriginal<typeof import('./lens-client')>();return {...actual,LensClient:class {profiles=async()=>({items:[h.profile]});submit=h.submit;}};});
vi.mock('./collection-pages',()=>({loadCollectionPages:async()=>[h.item]}));
vi.mock('./multi-frame-binding',()=>({loadMultiFrameBinding:async()=>h.binding}));
import LensRoute from '../screens/collections/[id]/lens';
import {LocaleProvider} from './locale-context';
import {renderInputSnapshotDigest} from './render-input-snapshot';
beforeEach(()=>{h.projectCanEdit=true;h.request.mockResolvedValue(h.collection);Object.defineProperty(navigator,'locks',{configurable:true,value:{request:async(_key:string,_options:unknown,action:()=>Promise<unknown>)=>action()}});});
afterEach(()=>{cleanup();vi.restoreAllMocks();h.values.clear();h.request.mockReset();h.snapshot.mockReset();h.submit.mockReset();});
it('retries a lost admission response in the same mounted screen with the original keys and selection',async()=>{
 Object.defineProperty(navigator,'locks',{configurable:true,value:{request:async(_key:string,_options:unknown,action:()=>Promise<unknown>)=>action()}});
 h.snapshot.mockImplementation(async(_project:string,request:Parameters<typeof renderInputSnapshotDigest>[0])=>({id:h.id(10),digest:renderInputSnapshotDigest(request),item_count:1}));
 h.submit.mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({run_id:h.id(11),workflow_id:h.id(12),state:'queued'});
 render(<LocaleProvider><LensRoute/></LocaleProvider>);
 await clickWhenEnabled(await screen.findByText('Camera / Lens · 50 mm · f/2'));
 await clickWhenEnabled(screen.getByRole('button',{name:'Process selected photographs'}));
 await screen.findByText('This action could not be completed. Refresh or retry the saved request.');
 expect(h.submit).toHaveBeenCalledTimes(1);expect(h.snapshot).toHaveBeenCalledTimes(1);
 await clickWhenEnabled(screen.getByRole('button',{name:'Resume submission'}));
 await waitFor(()=>expect(h.submit).toHaveBeenCalledTimes(2));
 expect(h.submit.mock.calls[1].slice(0,3)).toEqual(h.submit.mock.calls[0].slice(0,3));expect(h.snapshot).toHaveBeenCalledTimes(1);
 await screen.findByText('Processing request saved');
});

it('retries an initial load error without creating any snapshot or intent',async()=>{
 h.request.mockRejectedValueOnce(new Error('temporarily unavailable'));
 render(<LocaleProvider><LensRoute/></LocaleProvider>);
 await screen.findByText('This action could not be completed. Refresh or retry the saved request.');
 await clickWhenEnabled(screen.getByRole('button',{name:'Reload collection'}));
 await screen.findByText('Camera / Lens · 50 mm · f/2');
 expect(h.request).toHaveBeenCalledTimes(2);expect(h.snapshot).not.toHaveBeenCalled();expect(h.submit).not.toHaveBeenCalled();
 const first=h.request.mock.calls[0][1].signal as AbortSignal,second=h.request.mock.calls[1][1].signal as AbortSignal;
 expect(first.aborted).toBe(true);expect(second.aborted).toBe(false);
});

it('keeps restricted project preview inventory readable but cannot create a Lens intent',async()=>{
 h.projectCanEdit=false;render(<LocaleProvider><LensRoute/></LocaleProvider>);
 fireEvent.click(await screen.findByText('Camera / Lens · 50 mm · f/2'));fireEvent.click(screen.getByText('Process selected photographs'));
 expect(h.snapshot).not.toHaveBeenCalled();expect(h.submit).not.toHaveBeenCalled();
});
