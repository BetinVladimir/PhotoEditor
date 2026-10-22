// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({storage:new Map<string,string>(),write:null as null|((k:string,v:string)=>Promise<void>)}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({locale:'en'})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.storage.get(k)??null,setItem:async(k:string,v:string)=>{await state.write?.(k,v);state.storage.set(k,v)},removeItem:async(k:string)=>{state.storage.delete(k)},getAllKeys:async()=>[...state.storage.keys()]}}));
import {CollectionBulkControls} from './collection-bulk-controls';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(()=>{assertNoJSXWarnings();cleanup();});beforeEach(()=>{state.storage.clear();state.write=null});
function fixture(){
 const ticket={bulk_operation_id:id(8),run_id:id(9),status:'queued'};
 const snapshot={id:id(7),digest:'a'.repeat(64),item_count:2,expires_at:'2099-01-01T00:00:00Z'};
 const report={id:id(8),collection_id:id(3),snapshot_id:id(7),run_id:id(9),action:'rating',status:'completed_with_errors',cancel_requested:false,expected_next_revision:'4',counts:{total:2,succeeded:1,failed:1,skipped:0},results:{next_cursor:null,items:[{item_id:id(5),ordinal:0,status:'succeeded',result:{}},{item_id:id(6),ordinal:1,status:'failed',error_code:'REVISION_CONFLICT',result:null}]}};
 const request=vi.fn(async(path:string,options?:{method?:string;body?:unknown;idempotencyKey?:string;signal?:AbortSignal}):Promise<unknown>=>path==='/api/v1/me'?{id:id(1)}:path.endsWith('/selection-snapshots')?snapshot:options?.method==='POST'&&!path.endsWith('/cancel')?ticket:report);
 let held:symbol|null=null;const acquire=vi.fn(()=>{if(held)return null;held=Symbol();return held}),release=vi.fn((token:symbol)=>{if(held===token)held=null});
 const props={client:{request} as never,origin:'https://bulk.test',tenant:id(2),collection:{id:id(3),revision:'3',state:'draft',project_id:id(4)} as never,items:[{id:id(5),position:0,rating:0,flag:'none',tags:[]},{id:id(6),position:1,rating:5,flag:'pick',tags:['client']}] as never,canEdit:true,current:()=>true,onChanged:vi.fn(),acquire,release};
 return {request,props,ticket,snapshot,report};
}
async function start(){const select=await screen.findByRole('button',{name:'Select all'});await waitFor(()=>expect(select.getAttribute('aria-disabled')==='true'||select.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(select);const apply=screen.getByRole('button',{name:'Apply to selected'});await waitFor(()=>expect(apply.getAttribute('aria-disabled')==='true'||apply.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(apply)}
async function recover(){const button=await screen.findByRole('button',{name:'Recover / refresh'});await waitFor(()=>{expect(button.hasAttribute('disabled')).toBe(false);expectEnabled(button)});await clickWhenEnabled(button)}
it('unknown admission remount retries exact frozen snapshot/key/body and displays authoritative per-item failures',async()=>{
 const f=fixture();let fail=true;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))return f.snapshot;if(options?.method==='POST'){if(fail){fail=false;throw new Error('lost response')}return f.ticket}return f.report});
 let view=render(<CollectionBulkControls {...f.props}/>);await start();await screen.findByRole('alert');const original=f.request.mock.calls.find(([p])=>p.endsWith('/bulk-operations'))!;view.unmount();
 view=render(<CollectionBulkControls {...f.props}/>);await recover();await screen.findByText(`${id(6)} · Failed REVISION_CONFLICT`);
 const calls=f.request.mock.calls.filter(([p])=>p.endsWith('/bulk-operations'));expect(calls).toHaveLength(2);expect(calls[1][1]?.idempotencyKey).toBe(original[1]?.idempotencyKey);expect(calls[1][1]?.body).toEqual(original[1]?.body);expect(f.request.mock.calls.filter(([p])=>p.endsWith('/selection-snapshots'))).toHaveLength(1);
});
it('accepted receipt survives failed report and reload uses GET only',async()=>{
 const f=fixture();let failure=true;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))return f.snapshot;if(options?.method==='POST')return f.ticket;if(failure){failure=false;throw new Error('read unavailable')}return f.report});
 const view=render(<CollectionBulkControls {...f.props}/>);await start();await screen.findByRole('alert');view.unmount();const writes=f.request.mock.calls.filter(([,o])=>o?.method==='POST').length;
 render(<CollectionBulkControls {...f.props}/>);await recover();await screen.findByText(`${id(6)} · Failed REVISION_CONFLICT`);expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(writes);
});
it('stop during noncooperative admission preserves pending key and refuses late report publication',async()=>{
 const f=fixture();let resolve!: (v:unknown)=>void;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))return f.snapshot;if(options?.method==='POST')return new Promise(r=>{resolve=r});return f.report});
 render(<CollectionBulkControls {...f.props}/>);await start();await waitFor(()=>expect(resolve).toBeTypeOf('function'));await clickWhenEnabled(screen.getByRole('button',{name:'Stop waiting'}));await act(async()=>resolve(f.ticket));expect(screen.queryByText(`${id(6)} · Failed REVISION_CONFLICT`)).toBeNull();const raw=[...state.storage.values()][0];expect(JSON.parse(raw).ticket).toBeNull();expect(JSON.parse(raw).snapshot.id).toBe(id(7));
});
it('read-only users cannot allocate a new bulk intent',async()=>{const f=fixture();render(<CollectionBulkControls {...f.props} canEdit={false}/>);await waitFor(()=>expect(f.request).toHaveBeenCalled());fireEvent.click(screen.getByText('Select all'));fireEvent.click(screen.getByText('Apply to selected'));expect(f.request.mock.calls.some(([,o])=>o?.method==='POST')).toBe(false)});

it('retains a validated accepted ticket in memory when journal persistence fails and retries GET only',async()=>{
 const f=fixture();let failed=false;state.write=async(_key,raw)=>{if(JSON.parse(raw).ticket&&!failed){failed=true;throw new Error('disk unavailable')}};
 render(<CollectionBulkControls {...f.props}/>);await start();await screen.findByRole('alert');expect(failed).toBe(true);const posts=f.request.mock.calls.filter(([,o])=>o?.method==='POST').length;
 await clickWhenEnabled(screen.getByRole('button',{name:'Recover / refresh'}));await screen.findByText(`${id(6)} · Failed REVISION_CONFLICT`);expect(f.request.mock.calls.filter(([,o])=>o?.method==='POST')).toHaveLength(posts);expect(JSON.parse([...state.storage.values()][0]).ticket).toEqual(f.ticket);
});
it('role withdrawal during pending journal write prevents snapshot and admission POST',async()=>{
 const f=fixture();let release!:()=>void;state.write=()=>new Promise<void>(resolve=>{release=resolve});const view=render(<CollectionBulkControls {...f.props}/>);await start();await waitFor(()=>expect(release).toBeTypeOf('function'));
 view.rerender(<CollectionBulkControls {...f.props} canEdit={false}/>);await act(async()=>release());await screen.findByRole('alert');expect(f.request.mock.calls.some(([,o])=>o?.method==='POST')).toBe(false);expect(JSON.parse([...state.storage.values()][0]).ticket).toBeNull();
});
it('unknown snapshot can be abandoned with both original keys without creating another snapshot or run',async()=>{
 const f=fixture();f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))throw new Error('lost snapshot response');if(path.endsWith('/bulk-operations/abandon'))return{status:'abandoned',key:options!.idempotencyKey,collection_id:id(3)};throw new Error('unexpected network action')});
 render(<CollectionBulkControls {...f.props}/>);await start();await screen.findByRole('alert');const pending=JSON.parse([...state.storage.entries()].find(([key])=>key.startsWith('photo.collection-bulk.v1:'))![1]);
 await clickWhenEnabled(screen.getByRole('button',{name:'Abandon this pending action'}));await waitFor(()=>expect(f.props.onChanged).toHaveBeenCalledTimes(1));
 const call=f.request.mock.calls.find(([path])=>path.endsWith('/bulk-operations/abandon'))!;expect(call[1]?.idempotencyKey).toBe(pending.key);expect(call[1]?.body).toEqual({selection_key:pending.snapshot_key,selection:{collection_revision:'3',scope:'selected',item_ids:[id(5),id(6)]},action:'rating',params:{rating:5}});
 expect(f.request.mock.calls.filter(([path])=>path.endsWith('/selection-snapshots'))).toHaveLength(1);expect(f.request.mock.calls.some(([path])=>path.endsWith('/bulk-operations'))).toBe(false);expect([...state.storage.keys()].some(key=>key.startsWith('photo.collection-bulk.v1:'))).toBe(false);
});
it('accepted abandon resolution remembers snapshot and ticket before failed persistence, then uses GET only',async()=>{
 const f=fixture();let failSave=true;state.write=async(key,raw)=>{if(key.startsWith('photo.collection-bulk.v1:')&&JSON.parse(raw).ticket&&failSave){failSave=false;throw new Error('disk unavailable')}};
 f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))throw new Error('lost snapshot response');if(path.endsWith('/bulk-operations/abandon'))return{status:'accepted',snapshot:f.snapshot,ticket:f.ticket};if(options?.method==='POST')throw new Error('unexpected POST');return f.report});
 render(<CollectionBulkControls {...f.props}/>);await start();await screen.findByRole('alert');await clickWhenEnabled(screen.getByRole('button',{name:'Abandon this pending action'}));await waitFor(()=>expect(failSave).toBe(false));await screen.findByRole('alert');const posts=f.request.mock.calls.filter(([,options])=>options?.method==='POST').length;
 await clickWhenEnabled(screen.getByRole('button',{name:'Recover / refresh'}));await screen.findByText(`${id(6)} · Failed REVISION_CONFLICT`);expect(f.request.mock.calls.filter(([,options])=>options?.method==='POST')).toHaveLength(posts);
});
it('same actor changing collection cannot inherit a late old accepted receipt',async()=>{
 const f=fixture();let resolve!:(value:unknown)=>void;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))return f.snapshot;if(options?.method==='POST')return new Promise(r=>{resolve=r});return f.report});
 const view=render(<CollectionBulkControls {...f.props}/>);await start();await waitFor(()=>expect(resolve).toBeTypeOf('function'));
 view.rerender(<CollectionBulkControls {...f.props} collection={{id:id(30),revision:'1',state:'draft',project_id:id(4)} as never}/>);await waitFor(()=>expect(screen.getByText('Apply to selected')).toBeTruthy());await act(async()=>resolve(f.ticket));expect(screen.queryByText('Recover / refresh')).toBeNull();expect(screen.queryByText(`${id(6)} · Failed REVISION_CONFLICT`)).toBeNull();
});
it('preserves a validated snapshot across role withdrawal without admitting the subsequent bulk run',async()=>{
 const f=fixture();let finish!:(value:unknown)=>void;f.request.mockImplementation(async(path,options)=>{if(path==='/api/v1/me')return{id:id(1)};if(path.endsWith('/selection-snapshots'))return new Promise(resolve=>{finish=resolve});if(options?.method==='POST')return f.ticket;return f.report});
 const view=render(<CollectionBulkControls {...f.props}/>);await start();await waitFor(()=>expect(finish).toBeTypeOf('function'));
 view.rerender(<CollectionBulkControls {...f.props} canEdit={false}/>);await act(async()=>finish(f.snapshot));await screen.findByRole('alert');
 const pending=JSON.parse([...state.storage.entries()].find(([key])=>key.startsWith('photo.collection-bulk.v1:'))![1]);expect(pending.snapshot).toEqual(f.snapshot);expect(pending.ticket).toBeNull();expect(f.request.mock.calls.some(([path])=>path.endsWith('/bulk-operations'))).toBe(false);
 view.rerender(<CollectionBulkControls {...f.props}/>);await clickWhenEnabled(screen.getByRole('button',{name:'Recover / refresh'}));await screen.findByText(`${id(6)} · Failed REVISION_CONFLICT`);expect(f.request.mock.calls.filter(([path])=>path.endsWith('/selection-snapshots'))).toHaveLength(1);
});

let jsxWarnings:ReturnType<typeof vi.spyOn>;
beforeEach(()=>{jsxWarnings=vi.spyOn(console,'error');});
function assertNoJSXWarnings(){const messages=jsxWarnings.mock.calls.map((args:unknown[])=>args.map(String).join(' '));jsxWarnings.mockRestore();expect(messages.filter((message:string)=>message.includes('Unexpected text node')||message.includes('unique \"key\"'))).toEqual([]);}
