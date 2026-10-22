// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const state=vi.hoisted(()=>({os:'ios',read:vi.fn(),write:vi.fn(),values:new Map<string,string>()}));
vi.mock('react-native',async()=>{const web=await import('react-native-web');return {...web,Platform:{get OS(){return state.os}}}});
vi.mock('expo-router',()=>({usePathname:()=>'/bg/projects',useNavigationContainerRef:()=>({current:null}),router:{replace:vi.fn()}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:state.read,setItem:state.write}}));
import {LocaleProvider,useLocale} from './locale-context';
function Choice(){const {locale,setLocale}=useLocale();return <><output>{locale}</output><button onClick={()=>setLocale('ru')}>choose ru</button><button onClick={()=>setLocale('bg')}>choose bg</button></>}
function mount(){return render(<LocaleProvider><Choice/></LocaleProvider>)}
beforeEach(()=>{state.os='ios';state.values.clear();vi.resetAllMocks();state.read.mockImplementation(async(key:string)=>state.values.get(key)??null);state.write.mockImplementation(async(key:string,value:string)=>{state.values.set(key,value)})});
afterEach(cleanup);
it.each(['ios','android'])('restores the native preference on %s independently of URL locale',async(os)=>{state.os=os;state.values.set('photo.locale','ru');mount();await screen.findByText('ru');expect(state.read).toHaveBeenCalledWith('photo.locale')});
it('does not let a slow restoration overwrite an explicit newer choice',async()=>{let finish!:(value:string)=>void;state.read.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));mount();fireEvent.click(screen.getByText('choose bg'));await screen.findByText('bg');await act(async()=>finish('ru'));expect(screen.getByText('bg')).toBeTruthy();await waitFor(()=>expect(state.values.get('photo.locale')).toBe('bg'))});
it('serializes writes and continues after a failed earlier persistence',async()=>{let reject!:(error:Error)=>void;state.write.mockImplementationOnce(()=>new Promise((_resolve,rejectWrite)=>{reject=rejectWrite}));mount();fireEvent.click(screen.getByText('choose ru'));await waitFor(()=>expect(state.write).toHaveBeenCalledTimes(1));fireEvent.click(screen.getByText('choose bg'));expect(screen.getByText('bg')).toBeTruthy();expect(state.write).toHaveBeenCalledTimes(1);await act(async()=>reject(new Error('device storage unavailable')));await waitFor(()=>expect(state.values.get('photo.locale')).toBe('bg'));expect(state.write.mock.calls).toEqual([['photo.locale','ru'],['photo.locale','bg']])});
it.each(['de','constructor',''])('ignores unsupported stored preference %s',async(value)=>{state.values.set('photo.locale',value);mount();await act(async()=>{});expect(screen.getByText('en')).toBeTruthy();expect(state.write).not.toHaveBeenCalled()});
it('keeps language controls usable after a storage read failure',async()=>{state.read.mockRejectedValueOnce(new Error('storage unavailable'));mount();await act(async()=>{});expect(screen.getByText('en')).toBeTruthy();fireEvent.click(screen.getByText('choose ru'));expect(screen.getByText('ru')).toBeTruthy();await waitFor(()=>expect(state.values.get('photo.locale')).toBe('ru'))});
it('restores the last persisted choice after provider remount',async()=>{const view=mount();fireEvent.click(screen.getByText('choose bg'));await waitFor(()=>expect(state.values.get('photo.locale')).toBe('bg'));view.unmount();mount();await screen.findByText('bg')});
