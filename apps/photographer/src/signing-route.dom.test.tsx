// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{} as Record<string,string>,me:vi.fn(),boundary:vi.fn(),replace:vi.fn(),client:{request:vi.fn()}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{replace:mocks.replace}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./auth-context',()=>({useAuth:()=>({origin:'https://sign.test',client:()=>mocks.client})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=mocks.me;}}));
vi.mock('./signing-screen',()=>({SigningScreen:(props:unknown)=>{mocks.boundary(props);return 'Signing boundary';}}));
import Route from '../screens/exports/sign';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{mocks.params={tenant:id(1),export:id(2)};mocks.client={request:vi.fn()};mocks.me.mockReset();mocks.boundary.mockReset();mocks.replace.mockReset();});
afterEach(()=>{cleanup();vi.useRealTimers();});

it('retries identity restoration without signing or discarding the export scope',async()=>{
 mocks.me.mockRejectedValueOnce(new Error('temporary')).mockResolvedValueOnce({id:id(3)});
 render(<Route/>);await screen.findByRole('alert');
 expect(mocks.boundary).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'retry'}));
 await screen.findByText('Signing boundary');
 const props=mocks.boundary.mock.lastCall![0];
 expect(props.journal.key).toContain(id(3));expect(props.journal.exportID).toBe(id(2));expect(props.tenant).toBe(id(1));
 expect(mocks.me).toHaveBeenCalledTimes(2);expect(mocks.client.request).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});

it('aborts stale identity and never opens the previous actor journal',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(4)});
 const view=render(<Route/>);const signal=mocks.me.mock.calls[0][0] as AbortSignal;
 mocks.client={request:vi.fn()};view.rerender(<Route/>);
 await screen.findByText('Signing boundary');expect(signal.aborted).toBe(true);
 await act(async()=>finish({id:id(3)}));
 expect(mocks.boundary.mock.calls.every(call=>call[0].journal.key.includes(id(4)))).toBe(true);
});


it('bounds stalled identity restoration and ignores its late actor after retry',async()=>{
 vi.useFakeTimers();
 let finish!:(value:unknown)=>void;
 mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(4)});
 render(<Route/>);
 const signal=mocks.me.mock.calls[0][0] as AbortSignal;
 await act(async()=>{await vi.advanceTimersByTimeAsync(8000);});
 expect(signal.aborted).toBe(true);expect(screen.getByRole('alert')).toBeTruthy();
 expect(mocks.boundary).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'retry'}));
 await act(async()=>{await Promise.resolve();await Promise.resolve();});
 expect(screen.getByText('Signing boundary')).toBeTruthy();
 await act(async()=>{finish({id:id(3)});await Promise.resolve();});
 expect(mocks.boundary.mock.calls.every(call=>call[0].journal.key.includes(id(4)))).toBe(true);
 expect(mocks.client.request).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});
