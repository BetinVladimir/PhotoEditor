// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const state=vi.hoisted(()=>({params:{tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002'},client:{request:vi.fn()},me:vi.fn(),create:vi.fn(),pick:vi.fn(),webSource:vi.fn()}));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>state.params}));
vi.mock('expo-document-picker',()=>({getDocumentAsync:vi.fn()}));
vi.mock('./auth-context',()=>{const auth={origin:'https://import.test',client:()=>state.client};return{useAuth:()=>auth}});
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=state.me}}));
vi.mock('./raw-upload-settings',()=>({RAWUploadSettings:()=>null}));
vi.mock('./uploads',()=>({createUploadQueue:state.create,pickWebFiles:state.pick,webSource:state.webSource,nativeSource:vi.fn()}));
import ImportRoute from '../app/imports/new';
function queue(){return{records:[],subscribe:vi.fn(()=>vi.fn()),restore:vi.fn(async()=>[]),close:vi.fn(),enqueue:vi.fn(),resume:vi.fn(),refresh:vi.fn()}}
beforeEach(()=>{state.me.mockResolvedValue({id:'00000000-0000-4000-8000-000000000009'})});
afterEach(()=>{cleanup();vi.resetAllMocks()});
it('gates admission on restore and closes its queue on teardown',async()=>{
 const q=queue();let release!:(rows:never[])=>void;q.restore.mockImplementation(()=>new Promise(r=>{release=r}));state.create.mockReturnValue(q);
 const view=render(<ImportRoute/>);const choose=await screen.findByRole('button',{name:'import_choose'});
 expect(choose.getAttribute('aria-disabled')==='true'||choose.hasAttribute('disabled')).toBe(true);fireEvent.click(choose);expect(state.pick).not.toHaveBeenCalled();
 await act(async()=>{release([])});await waitFor(()=>expect(choose.getAttribute('aria-disabled')).not.toBe('true'));view.unmount();expect(q.close).toHaveBeenCalledTimes(1);
 expect(state.create.mock.calls[0].slice(1)).toEqual(['https://import.test','00000000-0000-4000-8000-000000000009',state.params.tenant,state.params.project,undefined]);
});
it('closes failed restore queue and creates a fresh one on explicit retry',async()=>{
 const first=queue(),second=queue();first.restore.mockRejectedValue(new Error('storage'));state.create.mockReturnValueOnce(first).mockReturnValueOnce(second);
 render(<ImportRoute/>);await screen.findByText('import_restore_failed');fireEvent.click(screen.getByRole('button',{name:'retry'}));await waitFor(()=>expect(second.restore).toHaveBeenCalledTimes(1));
 expect(first.close).toHaveBeenCalledTimes(1);expect(screen.queryByText('import_restore_failed')).toBeNull();
});
it('discards a late file picker result after route unmount',async()=>{
 const q=queue();state.create.mockReturnValue(q);let release!:(files:File[])=>void;state.pick.mockImplementation(()=>new Promise(r=>{release=r}));
 const view=render(<ImportRoute/>);await waitFor(()=>expect(q.restore).toHaveBeenCalledTimes(1));await act(async()=>{});fireEvent.click(screen.getByRole('button',{name:'import_choose'}));await waitFor(()=>expect(state.pick).toHaveBeenCalledTimes(1));view.unmount();
 await act(async()=>{release([new File(['x'],'x.jpg')])});expect(state.webSource).not.toHaveBeenCalled();expect(q.enqueue).not.toHaveBeenCalled();expect(q.close).toHaveBeenCalledTimes(1);
});
it('does not create a queue after an identity failure',async()=>{
 state.me.mockRejectedValue(new Error('offline'));render(<ImportRoute/>);await screen.findByRole('alert');expect(state.create).not.toHaveBeenCalled();
});
