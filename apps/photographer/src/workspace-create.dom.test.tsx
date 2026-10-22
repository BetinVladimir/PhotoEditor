// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,it,expect,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const catalogs={en,ru,bg};
const mocks=vi.hoisted(()=>({request:vi.fn(),commit:vi.fn(),data:new Map<string,string>(),locale:'en' as 'en'|'ru'|'bg'}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=async()=>({id:'00000000-0000-4000-8000-000000000001'})}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.data.get(k)??null,setItem:async(k:string,v:string)=>{mocks.data.set(k,v)},removeItem:async(k:string)=>{mocks.data.delete(k)}}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:vi.fn()}}));
vi.mock('./auth-context',()=>{const client={request:mocks.request},auth={origin:"https://workspace.test",client:()=>client};return{useAuth:()=>auth}});
vi.mock('./use-read-page',()=>({useReadPage:()=>({phase:'ready',data:{items:[]},current:()=>true,commit:mocks.commit})}));
vi.mock('./read-page-state',()=>({ReadPageState:()=>null}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:mocks.locale,t:(key:keyof typeof en)=>catalogs[mocks.locale][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import Workspaces from '../screens/workspaces';
afterEach(()=>{cleanup();mocks.data.clear();mocks.locale='en';vi.resetAllMocks()});
it('retries an ambiguous create with the same key and immutable name',async()=>{
 mocks.request.mockRejectedValueOnce(new Error('lost response')).mockResolvedValue({id:'00000000-0000-4000-8000-000000000002',name:'Shoot',role:'owner',revision:'1'});
 render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Shoot'}});await clickWhenEnabled(screen.getByText('Create'));
 await screen.findByRole('alert');expect(screen.getByLabelText('Workspace name').getAttribute('readonly')).not.toBeNull();
 await clickWhenEnabled(screen.getByText('Retry creation'));await waitFor(()=>expect(mocks.commit).toHaveBeenCalledTimes(1));
 expect(mocks.request.mock.calls[0][1].idempotencyKey).toBe(mocks.request.mock.calls[1][1].idempotencyKey);
 expect(mocks.request.mock.calls[1][1].body).toEqual({name:'Shoot'});
});
it('cancels creation transport and ignores a late successful response',async()=>{
 let resolve!:(value:unknown)=>void;mocks.request.mockReturnValue(new Promise(done=>{resolve=done}));
 render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Shoot'}});await clickWhenEnabled(screen.getByText('Create'));
 await waitFor(()=>expect(mocks.request).toHaveBeenCalledTimes(1));
 const signal=mocks.request.mock.calls[0][1].signal as AbortSignal;
 fireEvent.click(screen.getByText('Cancel creation request'));await screen.findByRole('alert');expect(signal.aborted).toBe(true);
 resolve({id:'00000000-0000-4000-8000-000000000002',name:'Shoot'});await Promise.resolve();expect(mocks.commit).not.toHaveBeenCalled();expect(screen.getByText('Retry creation')).toBeDefined();
});

it('restores an unresolved POST after reload with the same key and name',async()=>{
 mocks.request.mockRejectedValueOnce(new Error('lost response'));
 const first=render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));
 fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Reload Shoot'}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByRole('alert');
 const key=mocks.request.mock.calls[0][1].idempotencyKey;first.unmount();
 mocks.request.mockResolvedValue({id:'00000000-0000-4000-8000-000000000002',name:'Reload Shoot',role:'owner',revision:'1'});
 render(<Workspaces/>);await screen.findByText('Retry creation');await clickWhenEnabled(screen.getByText('Retry creation'));
 await waitFor(()=>expect(mocks.commit).toHaveBeenCalledTimes(1));
 const posts=mocks.request.mock.calls.filter(call=>call[1]?.method==='POST');expect(posts).toHaveLength(2);
 expect(posts[1][1].idempotencyKey).toBe(key);expect(posts[1][1].body).toEqual({name:'Reload Shoot'});
});
it('restores accepted tenant ticket through authenticated GET without another POST',async()=>{
 const tenant='00000000-0000-4000-8000-000000000002';
 const storageKey='photo.workspace-create-attempt.v1:'+JSON.stringify(['https://workspace.test','00000000-0000-4000-8000-000000000001']);
 mocks.data.set(storageKey,JSON.stringify({schema_version:1,key:'00000000-0000-4000-8000-000000000003',request:{name:'Accepted'},ticket:{tenant_id:tenant}}));
 mocks.request.mockResolvedValue({id:tenant,name:'Accepted',role:'owner',revision:'1'});
 render(<Workspaces/>);await screen.findByText('Retry creation');await clickWhenEnabled(screen.getByText('Retry creation'));
 await waitFor(()=>expect(mocks.commit).toHaveBeenCalledTimes(1));expect(mocks.request).toHaveBeenCalledTimes(1);
 expect(mocks.request.mock.calls[0][0]).toBe('/api/v1/tenants/'+tenant);expect(mocks.request.mock.calls[0][1].method).toBeUndefined();expect(mocks.data.has(storageKey)).toBe(false);
});
it('refuses overlong names before persisting an attempt and keeps the name editable',async()=>{
 render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));
 fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'x'.repeat(121)}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByRole('alert');
 expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false);expect(mocks.request).not.toHaveBeenCalled();expect(mocks.data.size).toBe(0);
});
it('cancellation during persistence prevents POST even when the write finishes later',async()=>{
 const {default:storage}=await import('@react-native-async-storage/async-storage');
 let release!:()=>void,entered!:()=>void;const gate=new Promise<void>(done=>{release=done}),started=new Promise<void>(done=>{entered=done});
 const original=storage.setItem.bind(storage);
 const write=vi.spyOn(storage,'setItem').mockImplementationOnce(async(key,value)=>{entered();await gate;await original(key,value)});
 try{
  render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));
  fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Cancelled'}});await clickWhenEnabled(screen.getByText('Create'));await started;
  fireEvent.click(screen.getByText('Cancel creation request'));await screen.findByRole('alert');release();
  await waitFor(()=>expect([...mocks.data.keys()].some(key=>key.startsWith('photo.workspace-create-attempt.v1:'))).toBe(true));
  expect(mocks.request).not.toHaveBeenCalled();expect(screen.getByText('Retry creation')).toBeDefined();
 }finally{release?.();write.mockRestore();}
});
it('cancels blocked creation-journal restoration and retries readonly without issuing a producer',async()=>{
 const {default:storage}=await import('@react-native-async-storage/async-storage');let release!:(v:string|null)=>void;
 const read=vi.spyOn(storage,'getItem').mockImplementationOnce(()=>new Promise(done=>{release=done}));
 try{render(<Workspaces/>);await waitFor(()=>expect(read).toHaveBeenCalled());fireEvent.click(screen.getByText('Cancel workspace recovery'));await screen.findByText('Workspace recovery stopped. Retry to recover the saved creation.');release(null);await Promise.resolve();expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(true);fireEvent.click(screen.getByText('Retry workspace recovery'));await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));expect(mocks.request).not.toHaveBeenCalled();}finally{read.mockRestore();}
});

it.each([{id:'not-a-uuid'},{role:'root'},{revision:'0'},{name:null}])('preserves exact pending POST when its response is malformed: %j',async change=>{
 const valid={id:'00000000-0000-4000-8000-000000000002',name:'Studio',role:'owner',revision:'1'};
 mocks.request.mockResolvedValueOnce({...valid,...change}).mockResolvedValue(valid);
 render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));
 fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Studio'}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByRole('alert');
 expect(mocks.commit).not.toHaveBeenCalled();expect(mocks.request).toHaveBeenCalledTimes(1);
 const key=mocks.request.mock.calls[0][1].idempotencyKey;
 const saved=[...mocks.data.entries()].find(([key])=>key.startsWith('photo.workspace-create-attempt.v1:'));
 expect(JSON.parse(saved![1]).ticket).toBeNull();
 await clickWhenEnabled(screen.getByText('Retry creation'));await waitFor(()=>expect(mocks.commit).toHaveBeenCalledTimes(1));
 const posts=mocks.request.mock.calls.filter(call=>call[1]?.method==='POST');expect(posts).toHaveLength(2);expect(posts[1][1].idempotencyKey).toBe(key);
});
it('retains accepted receipt across malformed GET and retries only authenticated GET',async()=>{
 const valid={id:'00000000-0000-4000-8000-000000000002',name:'Studio',role:'owner',revision:'1'};
 mocks.request.mockResolvedValueOnce(valid).mockResolvedValueOnce({...valid,role:'root'}).mockResolvedValue(valid);
 render(<Workspaces/>);await waitFor(()=>expect(screen.getByLabelText('Workspace name').hasAttribute('readonly')).toBe(false));
 fireEvent.change(screen.getByLabelText('Workspace name'),{target:{value:'Studio'}});await clickWhenEnabled(screen.getByText('Create'));await screen.findByRole('alert');
 expect(mocks.commit).not.toHaveBeenCalled();const saved=[...mocks.data.entries()].find(([key])=>key.startsWith('photo.workspace-create-attempt.v1:'));
 expect(JSON.parse(saved![1]).ticket).toEqual({tenant_id:valid.id});
 await clickWhenEnabled(screen.getByText('Retry creation'));await waitFor(()=>expect(mocks.commit).toHaveBeenCalledTimes(1));
 expect(mocks.request.mock.calls.filter(call=>call[1]?.method==='POST')).toHaveLength(1);expect(mocks.request.mock.calls[2][0]).toBe('/api/v1/tenants/'+valid.id);
});


it.each(['ru','bg'] as const)('translates an uncertain workspace creation to %s while preserving its exact request',async language=>{
 mocks.request.mockRejectedValueOnce(new Error('lost response')).mockResolvedValue({id:'00000000-0000-4000-8000-000000000002',name:'Shoot',role:'owner',revision:'1'});
 const view=render(<Workspaces/>);await waitFor(()=>expect(screen.getByRole('textbox',{name:en.workspace_name}).hasAttribute('readonly')).toBe(false));
 fireEvent.change(screen.getByRole('textbox',{name:en.workspace_name}),{target:{value:'Shoot'}});await clickWhenEnabled(screen.getByRole('button',{name:en.workspace_create}));await screen.findByRole('alert');
 const original=structuredClone(mocks.request.mock.calls[0][1].body),key=mocks.request.mock.calls[0][1].idempotencyKey;
 mocks.locale=language;view.rerender(<Workspaces/>);const text=catalogs[language];
 expect(screen.getByText(text.workspace_title)).toBeTruthy();expect(screen.getByRole('alert').textContent).toBe(text.workspace_creation_uncertain);
 expect(screen.getByRole('textbox',{name:text.workspace_name}).hasAttribute('readonly')).toBe(true);
 await clickWhenEnabled(screen.getByRole('button',{name:text.workspace_create_retry}));await waitFor(()=>expect(mocks.commit).toHaveBeenCalledOnce());
 expect(mocks.request.mock.calls[1][1].body).toEqual(original);expect(mocks.request.mock.calls[1][1].idempotencyKey).toBe(key);
});
