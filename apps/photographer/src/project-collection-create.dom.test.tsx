// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({data:new Map<string,string>(),request:vi.fn(),post:vi.fn(),get:vi.fn(),failWrite:false,failAccepted:false,roleCanEdit:true,canEdit:true,project:3,creates:[] as Array<()=>void>,names:[] as Array<(v:string)=>void>}));
const foreground=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>foreground}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:mocks.roleCanEdit})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');return {...paper,Button:(props:any)=>{if(props.children==='Create')mocks.creates.push(props.onPress);return React.createElement(paper.Button,props);},TextInput:(props:any)=>{if(props.accessibilityLabel==='Collection name')mocks.names.push(props.onChangeText);return React.createElement(paper.TextInput,props);}};});
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:id(mocks.project),tenant:id(2)}),router:{push:vi.fn()}}));
const client={request:mocks.request},auth={origin:'https://project-create.example',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.data.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failWrite)throw new Error('disk');if(mocks.failAccepted&&v.startsWith('{')&&JSON.parse(v).ticket){mocks.failAccepted=false;throw new Error('accepted disk');}mocks.data.set(k,v)},removeItem:async(k:string)=>{mocks.data.delete(k)},getAllKeys:async()=>[...mocks.data.keys()]}}));
import ProjectDetail from '../screens/projects/[id]/index';
const key='photo.collection-create-attempt.v1:'+JSON.stringify([auth.origin,id(1),id(2),id(3)]);
const collection=()=>({id:id(4),tenant_id:id(2),project_id:id(3),name:'Wedding',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'});
function prepare(){
 mocks.get.mockResolvedValue(collection());mocks.post.mockResolvedValue({collection:collection()});
 mocks.request.mockImplementation(async(path:string,options?:any)=>{
  if(path==='/api/v1/me')return {id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};
  if(path.includes('/projects/'))return {id:id(mocks.project),tenant_id:id(2),name:'Project '+mocks.project,description:'',visibility:'workspace',download_allowed:true,revision:'1',created_at:'2026-10-17T10:00:00Z',can_edit:mocks.canEdit};
  if(options?.method==='POST')return mocks.post(path,options);
  if(path.includes('/collections?'))return {items:[]};
  return mocks.get(path,options);
 });
}
async function create(){await screen.findByText('Project 3');fireEvent.change(await screen.findByLabelText('Collection name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByRole('button',{name:'Create'}));}
async function retry(){const button=await screen.findByRole('button',{name:'Retry collection creation'});await waitFor(()=>expectEnabled(button));await clickWhenEnabled(button);}
afterEach(()=>{assertNoJSXWarnings();cleanup();mocks.data.clear();mocks.failWrite=false;mocks.failAccepted=false;mocks.roleCanEdit=true;mocks.canEdit=true;mocks.project=3;mocks.creates=[];mocks.names=[];foreground.active=true;foreground.generation=0;vi.resetAllMocks();});
it('persists before POST and repeats exact body/key after uncertain remount',async()=>{
 prepare();mocks.post.mockImplementationOnce(async(_path,options)=>{expect(JSON.parse(mocks.data.get(key)!)).toMatchObject({key:options.idempotencyKey,request:options.body,ticket:null});throw new Error('offline');}).mockResolvedValueOnce({collection:collection()});
 const view=render(<ProjectDetail/>);await create();await screen.findByRole('alert');const original=mocks.post.mock.calls[0][1];view.unmount();
 render(<ProjectDetail/>);await retry();await screen.findByText('Wedding');
 expect(mocks.post.mock.calls[1][1].body).toEqual(original.body);expect(mocks.post.mock.calls[1][1].idempotencyKey).toBe(original.idempotencyKey);
 expect(JSON.parse(mocks.data.get(key)!).ticket).toEqual({collection_id:id(4)});await clickWhenEnabled(await screen.findByRole('button',{name:'Start another collection'}));await waitFor(()=>expect(mocks.data.has(key)).toBe(false));
});
it('recovers accepted collection with GET only',async()=>{
 prepare();mocks.get.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(collection());
 const view=render(<ProjectDetail/>);await create();await screen.findByRole('alert');
 expect(JSON.parse(mocks.data.get(key)!).ticket).toEqual({collection_id:id(4)});view.unmount();
 render(<ProjectDetail/>);await retry();await screen.findByText('Wedding');expect(mocks.post).toHaveBeenCalledTimes(1);
});
it('does not POST when pre-send persistence fails',async()=>{
 prepare();mocks.failWrite=true;render(<ProjectDetail/>);await create();await screen.findByRole('alert');expect(mocks.post).not.toHaveBeenCalled();
});
it('disables creation for current project viewers',async()=>{
 prepare();mocks.canEdit=false;render(<ProjectDetail/>);await screen.findByText('Project 3');fireEvent.change(await screen.findByLabelText('Collection name'),{target:{value:'Wedding'}});
 expect(screen.getByRole('button',{name:'Create'}).getAttribute('aria-disabled')).toBe('true');expect(mocks.post).not.toHaveBeenCalled();
});
it('ignores a late accepted ticket after switching project',async()=>{
 prepare();let resolve!:(value:unknown)=>void;mocks.post.mockImplementation(()=>new Promise(done=>resolve=done));
 const view=render(<ProjectDetail/>);await create();await waitFor(()=>expect(mocks.post).toHaveBeenCalledTimes(1));const signal=mocks.post.mock.calls[0][1].signal;
 mocks.project=8;view.rerender(<ProjectDetail/>);await screen.findByText('Project 8');expect(signal.aborted).toBe(true);
 await act(async()=>resolve({collection:collection()}));expect(screen.queryByText('Wedding')).toBeNull();expect(mocks.get).not.toHaveBeenCalled();
});

it('does no project reads while backgrounded',()=>{
 prepare();foreground.active=false;render(<ProjectDetail/>);expect(mocks.request).not.toHaveBeenCalled();
});

it('lets a viewer confirm an already accepted collection without another POST',async()=>{
 prepare();mocks.canEdit=false;
 mocks.data.set(key,JSON.stringify({schema_version:1,project_id:id(3),key:id(9),request:{project_id:id(3),name:'Wedding',description:''},ticket:{collection_id:id(4)}}));
 render(<ProjectDetail/>);await retry();await screen.findByText('Wedding');
 expect(mocks.post).not.toHaveBeenCalled();expect(JSON.parse(mocks.data.get(key)!).ticket).toEqual({collection_id:id(4)});await clickWhenEnabled(await screen.findByRole('button',{name:'Start another collection'}));await waitFor(()=>expect(mocks.data.has(key)).toBe(false));
});
it('retries only initial reads after project load failure without creating a collection',async()=>{
 prepare();mocks.request.mockRejectedValueOnce(new Error('offline'));render(<ProjectDetail/>);
 const retry=await screen.findByText('read_retry');await clickWhenEnabled(retry);
 await screen.findByText('Project 3');expect(mocks.post).not.toHaveBeenCalled();expect(mocks.data.size).toBe(0);
});
it('cancels initial project read and rejects its late result before explicit retry',async()=>{
 prepare();let resolve!:(value:unknown)=>void;mocks.request.mockReturnValueOnce(new Promise(done=>{resolve=done;}));render(<ProjectDetail/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'read_cancel'}));
 await act(async()=>{resolve({id:id(3),tenant_id:id(2),name:'Late project',can_edit:true});});
 expect(screen.queryByText('Late project')).toBeNull();await clickWhenEnabled(screen.getByRole('button',{name:'read_retry'}));
 await screen.findByText('Project 3');expect(mocks.post).not.toHaveBeenCalled();
});

it('does not elevate tenant Viewer using a project can_edit=true projection',async()=>{
 prepare();mocks.roleCanEdit=false;render(<ProjectDetail/>);await screen.findByText('Project 3');
 fireEvent.change(await screen.findByLabelText('Collection name'),{target:{value:'Wedding'}});fireEvent.click(screen.getByRole('button',{name:'Create'}));
 fireEvent.click(screen.getByRole('button',{name:'Import photos'}));expect(mocks.post).not.toHaveBeenCalled();
});
it('aborts an unfinished collection admission on current workspace edit withdrawal',async()=>{
 prepare();let complete!:(value:unknown)=>void;mocks.post.mockReturnValue(new Promise(resolve=>{complete=resolve}));
 const rendered=render(<ProjectDetail/>);await create();await waitFor(()=>expect(mocks.post).toHaveBeenCalledTimes(1));
 const options=mocks.post.mock.calls[0][1];mocks.roleCanEdit=false;rendered.rerender(<ProjectDetail/>);await act(async()=>{complete({collection:collection()})});expect(options.signal.aborted).toBe(true);expect(mocks.data.has(key)).toBe(true);
});

it('creates an empty collection and its initial private link atomically without persisting the secret',async()=>{
 prepare();mocks.post.mockImplementation(async(_path,options)=>({collection:{...collection(),state:'published',revision:'2'},initial_share:{id:id(7),collection_id:id(4),policy_version:'1',created_at:'2026-10-17T10:00:00Z',expires_at:'2026-11-17T10:00:00Z',...options.body.initial_share,secret:'S'.repeat(43)}}));mocks.get.mockResolvedValue({...collection(),state:'published',revision:'2'});
 render(<ProjectDetail/>);await screen.findByText('Project 3');await screen.findByText('Create an empty collection');fireEvent.change(await screen.findByLabelText('Collection name'),{target:{value:'Wedding'}});await clickWhenEnabled(screen.getByRole('button',{name:'Create and share in one step'}));await clickWhenEnabled(screen.getByRole('button',{name:'Create'}));await screen.findByText('The collection and link were created together. Copy the private link now.');expect(mocks.post).toHaveBeenCalledTimes(1);const sent=mocks.post.mock.calls[0][1];expect(sent.body.initial_share.permissions).toEqual({view:true,download:false,upload:false});expect(JSON.parse(mocks.data.get(key)!).request).toEqual(sent.body);expect([...mocks.data.values()].join('')).not.toContain('S'.repeat(43));
});
it('Stops waiting on a hanging create and retains the exact key when a late response arrives',async()=>{
 prepare();let complete!:(value:unknown)=>void;mocks.post.mockReturnValue(new Promise(resolve=>complete=resolve));render(<ProjectDetail/>);await create();await waitFor(()=>expect(mocks.post).toHaveBeenCalledTimes(1));const sent=mocks.post.mock.calls[0][1];await clickWhenEnabled(await screen.findByRole('button',{name:'Stop waiting'}));expect(sent.signal.aborted).toBe(true);await act(async()=>complete({collection:collection()}));expect(JSON.parse(mocks.data.get(key)!).key).toBe(sent.idempotencyKey);expect(JSON.parse(mocks.data.get(key)!).ticket).toBeNull();expect(mocks.get).not.toHaveBeenCalled();
});

it('keeps an accepted collection identity in memory after receipt persistence failure and never POSTs it again',async()=>{
 prepare();mocks.failAccepted=true;render(<ProjectDetail/>);await create();await screen.findByRole('alert');expect(JSON.parse(mocks.data.get(key)!).ticket).toBeNull();await retry();await screen.findByText('Wedding');expect(mocks.post).toHaveBeenCalledTimes(1);expect(JSON.parse(mocks.data.get(key)!).ticket).toEqual({collection_id:id(4)});
});
it('abandons the original uncertain collection request with its saved key before allowing another creation',async()=>{
 prepare();const saved={schema_version:1,project_id:id(3),key:id(8),request:{project_id:id(3),name:'Wedding',description:''},ticket:null};mocks.data.set(key,JSON.stringify(saved));mocks.post.mockResolvedValue({status:'abandoned',key:id(8),project_id:id(3)});render(<ProjectDetail/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Abandon saved creation'}));await waitFor(()=>expect(mocks.data.has(key)).toBe(false));expect(mocks.post.mock.calls[0][0]).toMatch(/collections\/abandon$/);expect(mocks.post.mock.calls[0][1]).toMatchObject({idempotencyKey:id(8),body:saved.request});await screen.findByText('Create');
});

it('a retained Create reads the latest draft, and a retained form edit cannot alter a pending immutable request',async()=>{
 prepare();let resolve!:(v:unknown)=>void;mocks.post.mockImplementation(()=>new Promise(done=>resolve=done));render(<ProjectDetail/>);await screen.findByText('Create an empty collection');await screen.findByRole('button',{name:'Create'});await act(async()=>{});const oldCreate=mocks.creates.at(-1)!,oldName=mocks.names.at(-1)!;fireEvent.change(await screen.findByLabelText('Collection name'),{target:{value:'Latest draft'}});act(()=>oldCreate());await waitFor(()=>expect(mocks.post).toHaveBeenCalledTimes(1));expect(mocks.post.mock.calls[0][1].body.name).toBe('Latest draft');act(()=>oldName('Forged old edit'));expect((await screen.findByLabelText('Collection name') as HTMLInputElement).value).toBe('Latest draft');await act(async()=>resolve({collection:{...collection(),name:'Latest draft'}}));
});
it('refuses an accepted collection response whose description differs from the immutable request',async()=>{
 prepare();mocks.post.mockResolvedValue({collection:{...collection(),description:'Unexpected'}});render(<ProjectDetail/>);await create();await screen.findByText('Unable to confirm creation. Retry uses the same saved request.');expect(JSON.parse(mocks.data.get(key)!).ticket).toBeNull();
});

let jsxWarnings:ReturnType<typeof vi.spyOn>;
beforeEach(()=>{jsxWarnings=vi.spyOn(console,'error');});
function assertNoJSXWarnings(){const messages=jsxWarnings.mock.calls.map((args:unknown[])=>args.map(String).join(' '));jsxWarnings.mockRestore();expect(messages.filter((message:string)=>message.includes('Unexpected text node')||message.includes('unique \"key\"'))).toEqual([]);}
