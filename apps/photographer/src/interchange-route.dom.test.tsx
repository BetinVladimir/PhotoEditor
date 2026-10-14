// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{tenant:'',project:'',id:''},values:new Map<string,string>(),me:vi.fn(),boundary:vi.fn(),client:{request:vi.fn()}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.values.get(k)??null,setItem:async(k:string,v:string)=>{mocks.values.set(k,v);},removeItem:async(k:string)=>{mocks.values.delete(k);}}}));
vi.mock('./auth-context',()=>({useAuth:()=>({origin:'https://interchange-route.test',client:()=>mocks.client})}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=mocks.me;}}));
vi.mock('./interchange-job-screen',()=>({InterchangeJobScreen:(props:any)=>{
 props.journal.assertScope(props.tenant,props.project,props.id);mocks.boundary(props);return 'Current interchange screen';
}}));
import InterchangeRoute from '../app/interchange/[id]';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{mocks.values.clear();mocks.params={tenant:id(2),project:id(3),id:id(4)};mocks.client={request:vi.fn()};mocks.me.mockReset();mocks.boundary.mockReset();});
afterEach(cleanup);
it('creates principal-scoped journal only after current identity response',async()=>{
 let finish!:(value:unknown)=>void;mocks.me.mockImplementation(()=>new Promise(resolve=>{finish=resolve;}));
 render(<InterchangeRoute/>);expect(mocks.boundary).not.toHaveBeenCalled();
 await act(async()=>finish({id:id(1)}));await screen.findByText('Current interchange screen');
 const props=mocks.boundary.mock.lastCall![0];
 expect(props.journal.key).toBe('photo.interchange-review-attempt.v1:'+JSON.stringify(['https://interchange-route.test',id(1),id(2),id(3),id(4)]));
});
it('rejects invalid route before identity IO',async()=>{
 mocks.params.id='invalid';render(<InterchangeRoute/>);
 await screen.findByText('Unable to restore interchange review.');
 expect(mocks.me).not.toHaveBeenCalled();expect(mocks.boundary).not.toHaveBeenCalled();
});
it('ignores prior tenant identity after route replacement',async()=>{
 let finish!:(value:unknown)=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({id:id(90)});
 const view=render(<InterchangeRoute/>);const signal=mocks.me.mock.calls[0][0] as AbortSignal;
 mocks.params={tenant:id(20),project:id(30),id:id(40)};view.rerender(<InterchangeRoute/>);
 await screen.findByText('Current interchange screen');expect(signal.aborted).toBe(true);
 await act(async()=>finish({id:id(1)}));
 expect(mocks.boundary.mock.calls.every(call=>call[0].tenant===id(20))).toBe(true);
 expect(mocks.boundary.mock.lastCall![0].journal.key).toContain(id(90));
});
it('replacing client hides prior journal until new identity resolves',async()=>{
 mocks.me.mockResolvedValueOnce({id:id(1)});const view=render(<InterchangeRoute/>);
 await screen.findByText('Current interchange screen');
 let finish!:(value:unknown)=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;}));
 mocks.client={request:vi.fn()};view.rerender(<InterchangeRoute/>);
 expect(screen.queryByText('Current interchange screen')).toBeNull();
 await waitFor(()=>expect(mocks.me).toHaveBeenCalledTimes(2));
 await act(async()=>finish({id:id(90)}));await screen.findByText('Current interchange screen');
 expect(mocks.boundary.mock.lastCall![0].journal.key).toContain(id(90));
});
