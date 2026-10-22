// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({data:new Map<string,string>(),invite:vi.fn(),remove:vi.fn(),items:[] as Array<{id:string;email:string;role:'editor'|'owner';status:string;revision:string}>}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:'00000000-0000-4000-8000-000000000002'})}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_o:string,f:()=>Promise<unknown>)=>f()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.data.get(k)??null,setItem:async(k:string,v:string)=>{state.data.set(k,v)},removeItem:async(k:string)=>{await state.remove();state.data.delete(k)}}}));
vi.mock('./auth-context',()=>{const client={};const auth={origin:'https://members.test',client:()=>client};return{useAuth:()=>auth}});
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canAdmin:true})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=async()=>({id:'00000000-0000-4000-8000-000000000001'});invite=state.invite;}}));
vi.mock('./use-read-page',()=>({useReadPage:()=>({phase:'ready',data:{items:state.items},current:()=>true,retry:vi.fn()})}));
vi.mock('./read-resource-state',()=>({ReadResourceState:()=>null}));vi.mock('./read-page-state',()=>({ReadPageState:()=>null}));
import Members from '../screens/settings/members';
import {LocaleProvider,useLocale} from './locale-context';
import {setFixtureLocale} from './test/locale-router';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
afterEach(cleanup);beforeEach(()=>{state.data.clear();state.items=[];state.invite.mockReset();state.remove.mockReset();});
const receipt={id:'00000000-0000-4000-8000-000000000003',email:'photo@example.com',role:'editor',expires_at:'2026-10-24T00:00:00Z'};
async function enter(){await waitFor(()=>expect(screen.getByPlaceholderText('Email').hasAttribute('readonly')).toBe(false));fireEvent.change(screen.getByPlaceholderText('Email'),{target:{value:'photo@example.com'}});fireEvent.click(screen.getByText('Invite editor'));}
it('replays unresolved invitation after full remount with identical body/key',async()=>{state.invite.mockRejectedValueOnce(new Error('lost response')).mockResolvedValue(receipt);const view=render(<Members/>);await enter();await screen.findByRole('alert');const first=state.invite.mock.calls[0];view.unmount();render(<Members/>);await screen.findByDisplayValue('photo@example.com');fireEvent.click(screen.getByText('Recover saved invitation'));await screen.findByText('Invitation confirmed.');expect(state.invite).toHaveBeenCalledTimes(2);expect(state.invite.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));expect(state.invite.mock.calls[1][4]).toBe(first[4]);});
it('acknowledgement failure then same-mounted retry uses persisted accepted receipt without POST',async()=>{state.invite.mockResolvedValue(receipt);state.remove.mockRejectedValueOnce(new Error('storage unavailable')).mockResolvedValue(undefined);render(<Members/>);await enter();await screen.findByRole('alert');expect(state.invite).toHaveBeenCalledTimes(1);fireEvent.click(screen.getByText('Retry saved member change'));await waitFor(()=>expect(state.remove).toHaveBeenCalledTimes(2));expect(state.invite).toHaveBeenCalledTimes(1);expect([...state.data.keys()].some(k=>k.startsWith('photo.invitation-attempt'))).toBe(false);});
it('cancelled late invitation cannot acknowledge or erase recovery and remount retains same identity',async()=>{let resolve!:(v:unknown)=>void;state.invite.mockReturnValue(new Promise(done=>{resolve=done}));const view=render(<Members/>);await enter();await waitFor(()=>expect(state.invite).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Cancel member change request'));await screen.findByRole('alert');await act(async()=>resolve(receipt));expect(screen.queryByText('Invitation confirmed.')).toBeNull();const key=state.invite.mock.calls[0][4];view.unmount();state.invite.mockResolvedValue(receipt);render(<Members/>);await screen.findByDisplayValue('photo@example.com');fireEvent.click(screen.getByText('Recover saved invitation'));await screen.findByText('Invitation confirmed.');expect(state.invite.mock.calls[1][4]).toBe(key);});
it('late completed acknowledgement after local cancel never causes another invitation POST',async()=>{let release!:()=>void;state.invite.mockResolvedValue(receipt);state.remove.mockImplementationOnce(()=>new Promise<void>(done=>{release=done})).mockResolvedValue(undefined);render(<Members/>);await enter();await waitFor(()=>expect(state.remove).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Cancel member change request'));await screen.findByRole('alert');await act(async()=>release());fireEvent.click(screen.getByText('Retry saved member change'));await waitFor(()=>expect(state.remove).toHaveBeenCalledTimes(2));expect(state.invite).toHaveBeenCalledOnce();});
it('only definitive server validation releases the exact unaccepted invitation for correction',async()=>{const {APIError}=await import('@photoeditor/api-client');state.invite.mockRejectedValueOnce(new APIError(422,'VALIDATION_FAILED'));render(<Members/>);await enter();await screen.findByText('Invitation was rejected. Correct the email and try again.');expect(screen.getByPlaceholderText('Email').hasAttribute('readonly')).toBe(false);expect([...state.data.keys()].some(k=>k.startsWith('photo.invitation-attempt'))).toBe(false);});

it.each([['ru',ru],['bg',bg]] as const)('renders %s members labels and recovers the identical invitation through translated controls',async(locale,messages)=>{
 setFixtureLocale(locale);state.data.set('photo.locale',locale);state.items=[{id:'00000000-0000-4000-8000-000000000009',email:'existing@example.test',role:'editor',status:'active',revision:'1'}];
 state.invite.mockRejectedValueOnce(new Error('lost response')).mockResolvedValue(receipt);
 render(<LocaleProvider><Members/></LocaleProvider>);
 const email=await screen.findByRole('textbox',{name:messages.settings_members_email});
 expect(await screen.findByText(messages.settings_members_title)).toBeTruthy();
 expect(screen.getByText(messages.settings_members_role_editor)).toBeTruthy();expect(screen.getByText(messages.settings_members_status_active)).toBeTruthy();
 await waitFor(()=>expect(email.hasAttribute('readonly')).toBe(false));fireEvent.change(email,{target:{value:'photo@example.com'}});fireEvent.click(screen.getByText(messages.settings_members_invite));
 expect((await screen.findByRole('alert')).textContent).toBe(messages.settings_members_uncertain);
 const first=state.invite.mock.calls[0];expect(first.slice(0,3)).toEqual(['photo@example.com','editor','en']);
 fireEvent.click(screen.getByText(messages.settings_members_retry_change));await screen.findByText(messages.settings_members_confirmed);
 expect(state.invite).toHaveBeenCalledTimes(2);expect(state.invite.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));expect(state.invite.mock.calls[1][4]).toBe(first[4]);
 expect([...state.data.keys()].some(k=>k.startsWith('photo.invitation-attempt'))).toBe(false);
});
it('renders empty members and a visible rejection in the newly selected locale without another POST',async()=>{
 setFixtureLocale('ru');state.data.set('photo.locale','ru');const {APIError}=await import('@photoeditor/api-client');state.invite.mockRejectedValueOnce(new APIError(422,'VALIDATION_FAILED'));
 function Switch(){const {setLocale}=useLocale();return <button onClick={()=>setLocale('bg')}>switch fixture locale</button>}
 render(<LocaleProvider><Switch/><Members/></LocaleProvider>);const email=await screen.findByRole('textbox',{name:ru.settings_members_email});await screen.findByText(ru.settings_members_empty);
 await waitFor(()=>expect(email.hasAttribute('readonly')).toBe(false));fireEvent.change(email,{target:{value:'photo@example.com'}});fireEvent.click(screen.getByText(ru.settings_members_invite));await screen.findByText(ru.settings_members_rejected);
 fireEvent.click(screen.getByText('switch fixture locale'));await screen.findByText(bg.settings_members_rejected);expect(screen.getByText(bg.settings_members_reload)).toBeTruthy();expect(screen.getByText(bg.settings_members_empty)).toBeTruthy();expect(state.invite).toHaveBeenCalledOnce();
});
