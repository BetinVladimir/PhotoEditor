// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>{
 const client={request:vi.fn()};
 return {params:{tenant:'',project:'',snapshot:''},client,auth:{origin:'https://api.test',client:()=>client},me:vi.fn(),journals:[] as unknown[][]};
});
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{replace:vi.fn()}}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=mocks.me;}}));
vi.mock('./delivery-create-attempt-store',()=>({DeliveryCreateAttemptStore:class {constructor(...args:unknown[]){mocks.journals.push(args)}}}));
vi.mock('./delivery-create-screen',()=>({DeliveryCreateScreen:()=> <div>creation-ready</div>}));
import DeliveryRoute from '../screens/deliveries/new';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{
 mocks.params={tenant:id(2),project:id(3),snapshot:id(4)};
 mocks.auth={origin:'https://api.test',client:()=>mocks.client};
 mocks.me.mockReset();mocks.journals.length=0;
 lifecycle.active=true;lifecycle.generation=0;
});
afterEach(()=>{cleanup();vi.useRealTimers()});

it('retries account loading before constructing the user-scoped creation journal',async()=>{
 mocks.me.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({id:id(10)});
 render(<DeliveryRoute/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'delivery_create_reload'}));
 await screen.findByText('creation-ready');
 expect(mocks.me).toHaveBeenCalledTimes(2);
 expect(mocks.journals).toHaveLength(1);
 expect(mocks.journals[0].slice(1,6)).toEqual(['https://api.test',id(10),id(2),id(3),id(4)]);
});

it('rejects malformed route scope before account IO or journal construction',async()=>{
 mocks.params.snapshot='invalid';
 render(<DeliveryRoute/>);
 await screen.findByRole('alert');
 expect(mocks.me).not.toHaveBeenCalled();expect(mocks.journals).toHaveLength(0);
});

it('aborts a stalled identity read and ignores its response after retry',async()=>{
 vi.useFakeTimers();
 let finish!:(user:{id:string})=>void;
 mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(11)});
 render(<DeliveryRoute/>);
 await act(async()=>{});
 const oldSignal=mocks.me.mock.calls[0][0] as AbortSignal;
 await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});
 expect(oldSignal.aborted).toBe(true);
 fireEvent.click(screen.getByText('delivery_create_reload'));
 await act(async()=>{});
 expect(screen.getByText('creation-ready')).toBeTruthy();
 await act(async()=>finish({id:id(10)}));
 expect(mocks.journals).toHaveLength(1);expect(mocks.journals[0][2]).toBe(id(11));
});

it('does not attach a late identity to a different account client',async()=>{
 let finish!:(user:{id:string})=>void;
 mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(11)});
 const view=render(<DeliveryRoute/>);
 await waitFor(()=>expect(mocks.me).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.me.mock.calls[0][0] as AbortSignal;
 const client={request:vi.fn()};mocks.auth={origin:'https://api.test',client:()=>client};
 view.rerender(<DeliveryRoute/>);
 await screen.findByText('creation-ready');
 expect(oldSignal.aborted).toBe(true);
 await act(async()=>finish({id:id(10)}));
 expect(mocks.journals).toHaveLength(1);expect(mocks.journals[0][2]).toBe(id(11));
});

it('aborts account loading in background and reauthorizes on return',async()=>{
 mocks.me.mockImplementationOnce(()=>new Promise(()=>{})).mockResolvedValueOnce({id:id(10)});
 const view=render(<DeliveryRoute/>);
 await waitFor(()=>expect(mocks.me).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.me.mock.calls[0][0] as AbortSignal;
 lifecycle.active=false;view.rerender(<DeliveryRoute/>);
 expect(oldSignal.aborted).toBe(true);expect(screen.queryByText('creation-ready')).toBeNull();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<DeliveryRoute/>);
 await screen.findByText('creation-ready');expect(mocks.me).toHaveBeenCalledTimes(2);
});
it('cancels account loading and ignores its late actor before retrying the same route',async()=>{
 let finish!:(value:unknown)=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(10)});
 render(<DeliveryRoute/>);await clickWhenEnabled(await screen.findByRole('button',{name:'read_cancel'}));await act(async()=>finish({id:id(9)}));expect(mocks.journals).toHaveLength(0);
 await clickWhenEnabled(screen.getByRole('button',{name:'delivery_create_reload'}));await screen.findByText('creation-ready');expect(mocks.journals[0][2]).toBe(id(10));
});
