// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{} as Record<string,string>,me:vi.fn(),boundary:vi.fn(),replace:vi.fn(),client:{request:vi.fn()}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{replace:mocks.replace}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./auth-context',()=>({useAuth:()=>({origin:'https://create.test',client:()=>mocks.client})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=mocks.me;}}));
vi.mock('./interchange-create-screen',()=>({InterchangeCreateScreen:(props:any)=>{props.journal.assertScope(props.tenant,props.request);mocks.boundary(props);return 'Create boundary';}}));
import Route,{interchangeCreateRouteRequest} from '../screens/interchange/new';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{mocks.params={tenant:id(1),project:id(2),direction:'export',format:'photoeditor_bundle',snapshot:id(3)};mocks.client={request:vi.fn()};mocks.me.mockReset();mocks.boundary.mockReset();mocks.replace.mockReset();});
afterEach(cleanup);
it('waits for identity, scopes journal and opens confirmed workflow',async()=>{
 let finish!:(value:unknown)=>void;mocks.me.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));render(<Route/>);expect(mocks.boundary).not.toHaveBeenCalled();
 await act(async()=>finish({id:id(6)}));await screen.findByText('Create boundary');const props=mocks.boundary.mock.lastCall![0];
 expect(props.journal.key).toContain(id(6));props.onCreated({run_id:id(7),workflow_id:id(8),state:'queued'});
 expect(mocks.replace).toHaveBeenCalledWith({pathname:'/interchange/[id]',params:{id:id(8),tenant:id(1),project:id(2)}});
});
it('rejects mixed import/export sources before identity IO',async()=>{
 mocks.params.bundle=id(9);render(<Route/>);await screen.findByRole('alert');expect(mocks.me).not.toHaveBeenCalled();
});
it('requires explicit import SHA and format',()=>{
 const source={project:id(2),direction:'import',format:'photoeditor_bundle',bundle:id(3),sha256:'a'.repeat(64)};
 expect(interchangeCreateRouteRequest(source)).toMatchObject({bundle:{artifact_id:id(3),sha256:'a'.repeat(64)}});
 expect(()=>interchangeCreateRouteRequest({...source,sha256:'invalid'})).toThrow();expect(()=>interchangeCreateRouteRequest({...source,format:''})).toThrow();
});
it('aborts stale identity when connection changes',async()=>{
 let finish!:(value:unknown)=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:id(10)});
 const view=render(<Route/>);const signal=mocks.me.mock.calls[0][0] as AbortSignal;mocks.client={request:vi.fn()};view.rerender(<Route/>);
 await screen.findByText('Create boundary');expect(signal.aborted).toBe(true);await act(async()=>finish({id:id(6)}));
 expect(mocks.boundary.mock.calls.every(call=>call[0].journal.key.includes(id(10)))).toBe(true);
});

it('retries failed identity restoration without creating an interchange job',async()=>{
 mocks.me.mockRejectedValueOnce(new Error('temporary failure')).mockResolvedValueOnce({id:id(6)});
 render(<Route/>);await screen.findByRole('alert');
 expect(mocks.boundary).not.toHaveBeenCalled();expect(mocks.client.request).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'retry'}));
 await screen.findByText('Create boundary');expect(mocks.me).toHaveBeenCalledTimes(2);
 expect(mocks.client.request).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
 expect(mocks.boundary.mock.lastCall![0].request).toMatchObject({project_id:id(2),snapshot_id:id(3)});
});

it('cancelled identity can retry before a late prior principal resolves',async()=>{
 let finish!:(value:{id:string})=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:'00000000-0000-4000-8000-000000000009'});
 render(<Route/>);fireEvent.click(screen.getByText('read_cancel'));await screen.findByRole('alert');fireEvent.click(screen.getByText('retry'));await screen.findByText('Create boundary');
 await act(async()=>finish({id:'00000000-0000-4000-8000-000000000008'}));expect(mocks.me).toHaveBeenCalledTimes(2);expect(mocks.replace).not.toHaveBeenCalled();
});
