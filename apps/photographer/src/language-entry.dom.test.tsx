// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({storage:new Map<string,string>(),save:vi.fn(),request:vi.fn(),me:vi.fn(),update:vi.fn(),replace:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{replace:state.replace}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(key:string)=>state.storage.get(key)??null,setItem:async(key:string,value:string)=>state.save(key,value)}}));
const client={request:state.request},auth={origin:'https://language.test',clientType:'web',client:()=>client,principal:()=>null,sessionGeneration:()=>0};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=state.me;updateMe=state.update;}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {LocaleProvider} from './locale-context';
import Login from '../screens/login';
import {LocalePicker} from './locale-picker';
import Profile from '../screens/settings/profile';
const profile={id:'11111111-1111-4111-8111-111111111111',email:'language@example.test',locale:'en' as 'en'|'ru'|'bg',theme:'system',timezone:'UTC',revision:'1'};
beforeEach(()=>{state.storage.clear();vi.clearAllMocks();state.save.mockImplementation(async(key:string,value:string)=>{state.storage.set(key,value)});state.me.mockResolvedValue(profile);});
afterEach(cleanup);
it('allows language selection before login and persists the choice through remount without starting auth',async()=>{
 const view=render(<LocaleProvider><LocalePicker/><Login/></LocaleProvider>);
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));
 expect(screen.getByLabelText('Электронная почта')).toBeTruthy();expect(screen.getByRole('radio',{name:'Русский'}).getAttribute('aria-checked')).toBe('true');expect(screen.getByRole('radio',{name:'English'}).getAttribute('aria-checked')).toBe('false');
 await waitFor(()=>expect(state.storage.get('photo.locale')).toBe('ru'));
 view.unmount();render(<LocaleProvider><LocalePicker/><Login/></LocaleProvider>);
 await screen.findByLabelText('Электронная почта');expect(state.request).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('radio',{name:'Български'}));expect(screen.getByLabelText('Имейл')).toBeTruthy();
});
it('saves the profile locale using the actual CAS method and restores localized controls',async()=>{
 let current={...profile};state.me.mockImplementation(async()=>current);
 state.update.mockImplementation(async(_before:unknown,patch:{locale:'bg'})=>{current={...current,...patch,revision:'2'};return current});
 const view=render(<LocaleProvider><Profile/></LocaleProvider>);
 await screen.findByText(profile.email);fireEvent.click(screen.getByRole('radio',{name:'Български'}));
 await screen.findByText('Профил');expect(state.update).toHaveBeenCalledWith(profile,{locale:'bg'},expect.any(AbortSignal));
 await waitFor(()=>expect(state.storage.get('photo.locale')).toBe('bg'));
 view.unmount();render(<LocaleProvider><Profile/></LocaleProvider>);
 await screen.findByText('Запази часовата зона');expect(screen.getByPlaceholderText('Часова зона IANA')).toBeTruthy();
});
it('does not announce a new profile language when its uncertain write is refused',async()=>{
 state.update.mockRejectedValue(new Error('authority withdrawn'));
 render(<LocaleProvider><Profile/></LocaleProvider>);await screen.findByText(profile.email);
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));await act(async()=>{});
 expect(screen.queryByText('Профиль')).toBeNull();expect(screen.getByText('Profile')).toBeTruthy();
 expect(state.storage.get('photo.locale')).not.toBe('ru');
});
it('serializes locale persistence so a slow earlier choice cannot overwrite the final choice',async()=>{
 let finish!:()=>void;
 state.save.mockImplementationOnce((key:string,value:string)=>new Promise<void>(resolve=>{finish=()=>{state.storage.set(key,value);resolve()}}));
 render(<LocaleProvider><LocalePicker/><Login/></LocaleProvider>);
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));await act(async()=>{});
 fireEvent.click(screen.getByRole('radio',{name:'Български'}));await act(async()=>{});
 expect(state.save).toHaveBeenCalledTimes(1);expect(screen.getByLabelText('Имейл')).toBeTruthy();
 await act(async()=>finish());await waitFor(()=>expect(state.storage.get('photo.locale')).toBe('bg'));
});
