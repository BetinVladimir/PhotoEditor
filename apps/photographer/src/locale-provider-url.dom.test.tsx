// @vitest-environment jsdom
import {useState} from 'react';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=> 'ru',setItem:async()=>{}}}));
import {LocaleProvider,useLocale} from './locale-context';
import {setFixtureLocale} from './test/locale-router';
function Probe({template=false}:{template?:boolean}){const {locale,t,setLocale}=useLocale();const [count,setCount]=useState(0);return <><output>{locale}</output>{template?<p>{t('ui_8cbaa55a52ef',{p0:'Photo',p1:7})}</p>:null}<button onClick={()=>setCount(count+1)}>count {count}</button><button onClick={()=>setLocale('ru')}>choose ru</button></>}
afterEach(cleanup);
it('interpolates the English default context without a provider',()=>{render(<Probe template/>);expect(screen.getByText('Photo 7')).toBeTruthy();expect(screen.queryByText('{p0} {p1}')).toBeNull()});
it('uses the web URL rather than a conflicting stored preference',()=>{setFixtureLocale('bg');render(<LocaleProvider><output><Language/></output></LocaleProvider>);expect(screen.getByText('bg')).toBeTruthy()});
function Language(){return useLocale().locale}
it('changes URL locale without remounting screen state or dropping query and fragment',()=>{window.history.replaceState(null,'','/en/projects?tenant=one&tag=a&tag=b#selection');render(<LocaleProvider><Probe/></LocaleProvider>);fireEvent.click(screen.getByText('count 0'));fireEvent.click(screen.getByText('choose ru'));expect(screen.getByText('count 1')).toBeTruthy();expect(screen.getByText('ru')).toBeTruthy();expect(window.location.pathname+window.location.search+window.location.hash).toBe('/ru/projects?tenant=one&tag=a&tag=b#selection')});
