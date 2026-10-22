// @vitest-environment jsdom
import en from '../../../locales/en/common.json';
import {expectEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const state=vi.hoisted(()=>({params:{tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002',collection:undefined as string|undefined},client:{request:vi.fn()},me:vi.fn(),projectAccess:vi.fn(),collection:vi.fn(),foreground:{active:true,generation:0},create:vi.fn(),pick:vi.fn(),webSource:vi.fn()}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>state.params}));
vi.mock('expo-document-picker',()=>({getDocumentAsync:vi.fn()}));
vi.mock('./auth-context',()=>{const auth={origin:'https://import.test',client:()=>state.client};return{useAuth:()=>auth}});
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./owner-api',()=>({OwnerAPI:class{me=state.me;projectAccess=state.projectAccess;collection=state.collection}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>state.foreground}));
vi.mock('./raw-upload-settings',()=>({RAWUploadSettings:()=>null}));
vi.mock('./uploads',()=>({createUploadQueue:state.create,pickWebFiles:state.pick,webSource:state.webSource,nativeSource:vi.fn()}));
import ImportRoute from '../screens/imports/new';
function queue(){return{records:[],subscribe:vi.fn(()=>vi.fn()),restore:vi.fn(async()=>[]),close:vi.fn(),enqueue:vi.fn(),resume:vi.fn(),refresh:vi.fn()}}
beforeEach(()=>{state.params.collection=undefined;state.foreground={active:true,generation:0};state.me.mockResolvedValue({id:'00000000-0000-4000-8000-000000000009'});state.projectAccess.mockResolvedValue({id:state.params.project,can_edit:true})});
afterEach(()=>{cleanup();vi.resetAllMocks()});
it('gates admission on restore and closes its queue on teardown',async()=>{
 const q=queue();let release!:(rows:never[])=>void;q.restore.mockImplementation(()=>new Promise(r=>{release=r}));state.create.mockReturnValue(q);
 const view=render(<ImportRoute/>);const choose=await screen.findByRole('button',{name:en.import_choose});
 expect(choose.getAttribute('aria-disabled')==='true'||choose.hasAttribute('disabled')).toBe(true);fireEvent.click(choose);expect(state.pick).not.toHaveBeenCalled();
 await waitFor(()=>expect(q.restore).toHaveBeenCalledTimes(1));await act(async()=>{release([])});await waitFor(()=>expectEnabled(choose));view.unmount();expect(q.close).toHaveBeenCalledTimes(1);
 expect(state.create.mock.calls[0].slice(1,6)).toEqual(['https://import.test','00000000-0000-4000-8000-000000000009',state.params.tenant,state.params.project,undefined]);
});
it('closes failed restore queue and creates a fresh one on explicit retry',async()=>{
 const first=queue(),second=queue();first.restore.mockRejectedValue(new Error('storage'));state.create.mockReturnValueOnce(first).mockReturnValueOnce(second);
 render(<ImportRoute/>);await screen.findByText(en.import_restore_failed);fireEvent.click(screen.getByRole('button',{name:en.retry}));await waitFor(()=>expect(second.restore).toHaveBeenCalledTimes(1));
 expect(first.close).toHaveBeenCalledTimes(1);expect(screen.queryByText(en.import_restore_failed)).toBeNull();
});
it('discards a late file picker result after route unmount',async()=>{
 const q=queue();state.create.mockReturnValue(q);let release!:(files:File[])=>void;state.pick.mockImplementation(()=>new Promise(r=>{release=r}));
 const view=render(<ImportRoute/>);await waitFor(()=>expect(q.restore).toHaveBeenCalledTimes(1));await act(async()=>{});fireEvent.click(screen.getByRole('button',{name:en.import_choose}));await waitFor(()=>expect(state.pick).toHaveBeenCalledTimes(1));view.unmount();
 await act(async()=>{release([new File(['x'],'x.jpg')])});expect(state.webSource).not.toHaveBeenCalled();expect(q.enqueue).not.toHaveBeenCalled();expect(q.close).toHaveBeenCalledTimes(1);
});
it('does not create a queue after an identity failure',async()=>{
 state.me.mockRejectedValue(new Error('offline'));render(<ImportRoute/>);await screen.findByRole('alert');expect(state.create).not.toHaveBeenCalled();
});

it('does not create a queue for a viewer project',async()=>{
 state.projectAccess.mockResolvedValue({id:state.params.project,can_edit:false});render(<ImportRoute/>);
 await screen.findByRole('alert');expect(state.create).not.toHaveBeenCalled();expect(state.pick).not.toHaveBeenCalled();
});
it('closes and hides uploads immediately on transport authority loss',async()=>{
 const q=queue();state.create.mockReturnValue(q);render(<ImportRoute/>);
 await waitFor(()=>expect(q.restore).toHaveBeenCalledTimes(1));
 await act(async()=>{state.create.mock.calls[0][6]()});
 await screen.findByRole('alert');expect(q.close).toHaveBeenCalled();expect(screen.queryByRole('button',{name:en.import_choose})).toBeNull();
});
it('closes the prior queue in background and reloads authority before restoring on return',async()=>{
 const first=queue(),second=queue();state.create.mockReturnValueOnce(first).mockReturnValueOnce(second);
 const view=render(<ImportRoute/>);await waitFor(()=>expect(first.restore).toHaveBeenCalledTimes(1));
 state.foreground={active:false,generation:0};view.rerender(<ImportRoute/>);expect(first.close).toHaveBeenCalledTimes(1);
 expect(screen.queryByRole('button',{name:en.import_choose})).toBeNull();
 state.foreground={active:true,generation:1};view.rerender(<ImportRoute/>);await waitFor(()=>expect(second.restore).toHaveBeenCalledTimes(1));
 expect(state.me).toHaveBeenCalledTimes(2);expect(state.projectAccess).toHaveBeenCalledTimes(2);
});
it('does not create a queue while initially backgrounded',()=>{
 state.foreground={active:false,generation:0};render(<ImportRoute/>);expect(state.me).not.toHaveBeenCalled();expect(state.create).not.toHaveBeenCalled();
});

it.each(['archived','foreign-project'])('refuses an %s target collection before queue creation',async mode=>{
 state.params.collection='00000000-0000-4000-8000-000000000003';
 state.collection.mockResolvedValue({id:state.params.collection,tenant_id:state.params.tenant,project_id:mode==='foreign-project'?'00000000-0000-4000-8000-000000000004':state.params.project,state:mode==='archived'?'archived':'draft'});
 render(<ImportRoute/>);await screen.findByRole('alert');expect(state.create).not.toHaveBeenCalled();
});

it('cancels initial identity before creating a queue and refuses its late principal',async()=>{
 let finish!:(value:unknown)=>void;state.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));render(<ImportRoute/>);
 fireEvent.click(await screen.findByText(en.read_cancel));await act(async()=>{finish({id:'00000000-0000-4000-8000-000000000009'})});expect(state.create).not.toHaveBeenCalled();await screen.findByRole('button',{name:en.retry});
});
it('cancels queue restoration, closes it and retries with a fresh queue after a late restore',async()=>{
 const old=queue(),next=queue();let finish!:(rows:never[])=>void;old.restore.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));state.create.mockReturnValueOnce(old).mockReturnValueOnce(next);render(<ImportRoute/>);
 await waitFor(()=>expect(old.restore).toHaveBeenCalledOnce());fireEvent.click(screen.getByText(en.read_cancel));expect(old.close).toHaveBeenCalled();fireEvent.click(screen.getByText(en.retry));await waitFor(()=>expect(next.restore).toHaveBeenCalledOnce());
 await act(async()=>{finish([])});expect(state.pick).not.toHaveBeenCalled();expect(old.enqueue).not.toHaveBeenCalled();
});

it.each([undefined,'srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'] as const)('captures the explicit profile %s before asynchronous file selection',async profile=>{
 const q=queue();state.create.mockReturnValue(q);let release!:(files:File[])=>void;state.pick.mockImplementation(()=>new Promise(r=>{release=r}));
 const source={name:'untagged.jpg',size:1,lastModified:0,sha256:'a'.repeat(64),read:async()=>new Uint8Array([1])};state.webSource.mockResolvedValue(source);
 render(<ImportRoute/>);const choose=await screen.findByRole('button',{name:en.import_choose});await waitFor(()=>expectEnabled(choose));
 const labels={srgb_v1:en.import_profile_srgb,linear_srgb_v1:en.import_profile_linear,work_rec2020_gamma24_v1:en.import_profile_rec2020};
 expect(screen.getByRole('radio',{name:en.import_profile_embedded}).getAttribute('aria-checked')).toBe('true');
 if(profile)fireEvent.click(screen.getByRole('radio',{name:labels[profile]}));
 fireEvent.click(choose);await waitFor(()=>expect(state.pick).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByRole('radio',{name:en.import_profile_embedded}));
 await act(async()=>release([new File(['x'],'untagged.jpg')]));await waitFor(()=>expect(q.enqueue).toHaveBeenCalledOnce());
 expect(q.enqueue.mock.calls[0][1]).toEqual({tenant:state.params.tenant,projectId:state.params.project,collectionId:undefined,...(profile?{assumeProfile:profile}:{})});
});
