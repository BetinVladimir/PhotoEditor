// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({failAccepted:false,projectCanEdit:true,params:{id:'',tenant:'',operation:'optical.panorama'},values:new Map<string,string>(),request:vi.fn(),me:vi.fn(),caps:vi.fn(),availability:vi.fn(),binding:vi.fn(),snapshot:vi.fn(),submit:vi.fn(),media:vi.fn(),push:vi.fn(),review:vi.fn()}));
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./multi-review-view',()=>({MultiReviewView:(props:unknown)=>{mocks.review(props);return 'Current multi review';}}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.values.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failAccepted&&JSON.parse(v)?.attempt?.ticket)throw new Error('accepted storage failed');mocks.values.set(k,v);},removeItem:async(k:string)=>{mocks.values.delete(k);},getAllKeys:async()=>[...mocks.values.keys()]}}));
const client={request:mocks.request},auth={origin:'https://multi-route.test',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {projectAccess=async(id:string)=>({id,can_edit:mocks.projectCanEdit});me=mocks.me;multiCapabilities=mocks.caps;capabilities=mocks.availability;v2Binding=mocks.binding;createRenderInputSnapshot=mocks.snapshot;submitMulti=mocks.submit;mediaLinks=mocks.media;}}));
import MultiRoute from '../screens/collections/[id]/multi';
import {APIError} from '@photoeditor/api-client';
import {renderInputSnapshotDigest} from './render-input-snapshot';
import {LocaleProvider} from './locale-context';
import {LocalePicker} from './locale-picker';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const frame=(n:number)=>({asset_version_id:id(n),source:{artifact_id:id(n+1),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)});
const items=()=>[4,6,8].map((n,index)=>({id:id(100+n),collection_id:mocks.params.id,asset_id:id(200+n),asset_version_id:id(n),position:index,tags:[],moderation_state:'accepted'}));
function deferred<T>(){let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>resolve=r);return {promise,resolve};}
beforeEach(()=>{
 mocks.failAccepted=false;mocks.projectCanEdit=true;
 mocks.availability.mockImplementation(async()=>({operations:['optical.panorama','optical.focus_stack'].map(operation=>({operation,available:true,status:'ready',supported_backends:['cpu'],available_backends:['cpu']}))}));
 mocks.values.clear();mocks.params={id:id(10),tenant:id(1),operation:'optical.panorama'};
 // Browser lock stub only; this is not proof of real browser lock serialization.
 Object.defineProperty(navigator,'locks',{value:{request:async(_name:string,_options:unknown,action:()=>unknown)=>action()},configurable:true});
 mocks.me.mockResolvedValue({id:id(2)});mocks.media.mockResolvedValue({items:[]});
 mocks.request.mockImplementation(async(path:string)=>path.includes('/items?')?{revision:'1',items:items()}:{id:mocks.params.id,tenant_id:id(1),project_id:id(3),name:'Source collection',revision:'1'});
 mocks.caps.mockImplementation(async(operation:string)=>({schema_version:1,tenant_id:id(1),project_id:id(3),operation,calibration:{artifact_id:id(50),sha256:'c'.repeat(64)},max_pixels:1000000,frame_limit:operation==='optical.panorama'?20:30}));
 mocks.binding.mockImplementation(async(asset:string)=>frame(Number(asset.slice(-12))-200));
 mocks.snapshot.mockImplementation(async(_project:string,body:{sources:ReturnType<typeof frame>[]})=>({id:id(60),digest:renderInputSnapshotDigest(body),item_count:body.sources.length}));
 mocks.submit.mockResolvedValue({run_id:id(70),workflow_id:id(71),state:'queued'});
});
afterEach(()=>{cleanup();vi.resetAllMocks();});
async function addTwo(){
 await screen.findByRole('button',{name:'Add photo 1'});
 await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 1'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Add photo 1'}));await screen.findByText('Ordered frames · 1 / 20');
 await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 2'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Add photo 2'}));await screen.findByText('Ordered frames · 2 / 20');
}
it.each([401,403,404,410])('withdraws original authoring authority after admission denial %s',async(status)=>{
 mocks.submit.mockRejectedValue(new APIError(status,'FORBIDDEN'));render(<MultiRoute/>);await addTwo();
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await waitFor(()=>expect(mocks.submit).toHaveBeenCalledOnce());
 await waitFor(()=>expect(screen.queryByText('Current multi review')).toBeNull());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Create multi-image result'}).hasAttribute('disabled')).toBe(true));
 expect(mocks.push).not.toHaveBeenCalled();expect([...mocks.values.keys()].some(k=>k.includes('preparation'))).toBe(true);
});
it('refuses submission after actor changes and preserves existing state until explicit reload',async()=>{
 render(<MultiRoute/>);await addTwo();mocks.me.mockResolvedValue({id:id(99)});
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Create multi-image result'}).hasAttribute('disabled')).toBe(true));
 expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.submit).not.toHaveBeenCalled();expect(mocks.push).not.toHaveBeenCalled();
});
it('retains an accepted immutable ticket but refuses navigation after actor changes',async()=>{
 mocks.submit.mockImplementation(async()=>{mocks.me.mockResolvedValue({id:id(99)});return {run_id:id(70),workflow_id:id(71),state:'queued'};});
 render(<MultiRoute/>);await addTwo();fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));
 await waitFor(()=>expect([...mocks.values.values()].some(v=>JSON.parse(v).attempt?.ticket?.run_id===id(70))).toBe(true));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Create multi-image result'}).hasAttribute('disabled')).toBe(true));expect(mocks.push).not.toHaveBeenCalled();
});
it('submits the edited order/reference and original keys, then navigates only after acknowledgement',async()=>{
 render(<MultiRoute/>);await addTwo();
 fireEvent.click(screen.getByRole('button',{name:'Use frame 2 as reference'}));fireEvent.click(screen.getByRole('button',{name:'Move frame 2 up'}));
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await waitFor(()=>expect(mocks.push).toHaveBeenCalled());
 const call=mocks.submit.mock.calls[0];expect(call[0]).toBe('optical.panorama');expect(call[1]).toBe(id(3));expect(call[2].params.frames.map((f:{asset_version_id:string})=>f.asset_version_id)).toEqual([id(6),id(4)]);expect(call[2].params.reference_ordinal).toBe(0);
 const saved=JSON.parse([...mocks.values.entries()].find(([key])=>key.includes('preparation'))![1]);expect(saved.intent_key).toBe(call[3]);expect(saved.snapshot_key).toBe(mocks.snapshot.mock.calls[0][2]);expect(saved.attempt.ticket.run_id).toBe(id(70));
});
it('remount recovers an ambiguous admission without changing its snapshot or intent',async()=>{
 mocks.submit.mockRejectedValueOnce(new Error('response lost'));
 const view=render(<MultiRoute/>);await addTwo();fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await screen.findByRole('button',{name:'Retry saved submission'});await screen.findByText(/Submission was not confirmed/);
 const first=structuredClone(mocks.submit.mock.calls[0].slice(0,4));view.unmount();render(<MultiRoute/>);
 await screen.findByRole('button',{name:'Retry saved submission'});await waitFor(()=>expect(screen.getByRole('button',{name:'Retry saved submission'}).hasAttribute('disabled')).toBe(false));
 expect(screen.queryByRole('button',{name:'Add photo 3'})).toBeNull();fireEvent.click(screen.getByRole('button',{name:'Retry saved submission'}));await waitFor(()=>expect(mocks.push).toHaveBeenCalled());
 expect(mocks.snapshot).toHaveBeenCalledTimes(1);expect(mocks.submit.mock.calls[1].slice(0,4)).toEqual(first);
});
it('does not submit a late binding after moving to a different operation',async()=>{
 const pending=deferred<ReturnType<typeof frame>>();mocks.binding.mockReturnValueOnce(pending.promise);
 const view=render(<MultiRoute/>);await screen.findByRole('button',{name:'Add photo 1'});await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 1'}).hasAttribute('disabled')).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Add photo 1'}));
 mocks.params={...mocks.params,operation:'optical.focus_stack'};view.rerender(<MultiRoute/>);await screen.findByText('Focus stack');
 await act(async()=>pending.resolve(frame(4)));await screen.findByText('Ordered frames · 0 / 30');expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.submit).not.toHaveBeenCalled();
});
it('refuses changed collection versions',async()=>{
 mocks.binding.mockResolvedValue(frame(90));render(<MultiRoute/>);await screen.findByRole('button',{name:'Add photo 1'});await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 1'}).hasAttribute('disabled')).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Add photo 1'}));
 await screen.findByText(/This photo changed/);expect(screen.getByText('Ordered frames · 0 / 20')).toBeDefined();expect(mocks.snapshot).not.toHaveBeenCalled();
});
it('keeps saved retry available when current capabilities are unavailable',async()=>{
 mocks.submit.mockRejectedValueOnce(new Error('response lost'));
 const view=render(<MultiRoute/>);await addTwo();await clickWhenEnabled(screen.getByRole('button',{name:'Create multi-image result'}));await screen.findByText(/Submission was not confirmed/);view.unmount();
 mocks.caps.mockRejectedValue(new Error('qualification unavailable'));render(<MultiRoute/>);await screen.findByText('Saved submission is available. New selections are currently unavailable.');
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry saved submission'}));await waitFor(()=>expect(mocks.push).toHaveBeenCalled());expect(mocks.snapshot).toHaveBeenCalledTimes(1);expect(mocks.submit).toHaveBeenCalledTimes(2);
});
it('does not navigate a new route after a late old admission acknowledgement',async()=>{
 const pending=deferred<{run_id:string;workflow_id:string;state:string}>();mocks.submit.mockReturnValueOnce(pending.promise);
 const view=render(<MultiRoute/>);await addTwo();fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await waitFor(()=>expect(mocks.submit).toHaveBeenCalledTimes(1));
 mocks.params={...mocks.params,operation:'optical.focus_stack'};view.rerender(<MultiRoute/>);await screen.findByText('Focus stack');
 await act(async()=>pending.resolve({run_id:id(70),workflow_id:id(71),state:'queued'}));expect(mocks.push).not.toHaveBeenCalled();
 mocks.params={...mocks.params,operation:'optical.panorama'};view.rerender(<MultiRoute/>);await screen.findByRole('button',{name:'Open acknowledged run'});
});
it('focus stack submits its explicit motion policy',async()=>{
 mocks.params={...mocks.params,operation:'optical.focus_stack'};render(<MultiRoute/>);
 await screen.findByRole('button',{name:'Add photo 1'});await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 1'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Add photo 1'}));await screen.findByText('Ordered frames · 1 / 30');
 await waitFor(()=>expect(screen.getByRole('button',{name:'Add photo 2'}).hasAttribute('disabled')).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Add photo 2'}));await screen.findByText('Ordered frames · 2 / 30');
 fireEvent.click(screen.getByRole('button',{name:'Reject moving frames'}));await act(async()=>{});fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await waitFor(()=>expect(mocks.push).toHaveBeenCalled());
 expect(mocks.submit.mock.calls[0][0]).toBe('optical.focus_stack');expect(mocks.submit.mock.calls[0][2].params.motion_policy).toBe('reject');expect(mocks.submit.mock.calls[0][2].params).not.toHaveProperty('projection');
});
it('switches RU/BG labels without resetting an unresolved submission',async()=>{
 mocks.submit.mockRejectedValueOnce(new Error('lost'));
 render(<LocaleProvider><LocalePicker/><MultiRoute/></LocaleProvider>);await addTwo();fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await screen.findByText(/Submission was not confirmed/);
 const first=structuredClone(mocks.submit.mock.calls[0].slice(0,4));
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));expect(screen.getByRole('button',{name:ru.multi_retry})).toBeDefined();expect(screen.getByRole('alert').textContent).toBe(ru.multi_unknown);
 fireEvent.click(screen.getByRole('radio',{name:'Български'}));expect(screen.getByRole('button',{name:bg.multi_retry})).toBeDefined();expect(screen.getByRole('alert').textContent).toBe(bg.multi_unknown);
 fireEvent.click(screen.getByRole('button',{name:bg.multi_retry}));await waitFor(()=>expect(mocks.push).toHaveBeenCalled());expect(mocks.submit.mock.calls[1].slice(0,4)).toEqual(first);expect(mocks.me.mock.calls.every(call=>call[0] instanceof AbortSignal)).toBe(true);
});

it('retains Apply collection binding for acknowledged recovery without fresh capabilities',async()=>{
 const view=render(<MultiRoute/>);await addTwo();
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalled());
 view.unmount();mocks.review.mockClear();mocks.caps.mockRejectedValue(new Error('qualification unavailable'));
 render(<MultiRoute/>);
 await screen.findByText('Saved submission is available. New selections are currently unavailable.');
 await waitFor(()=>{
  const props=mocks.review.mock.calls.at(-1)?.[0] as {apply?:{selection?:{item:string;expectedVersion:string};disabled:boolean}}|undefined;
  expect(props?.apply?.selection?.item).toBe(id(104));
  expect(props?.apply?.selection?.expectedVersion).toBe(id(4));
  expect(props?.apply?.disabled).toBe(false);
 });
 expect(mocks.submit).toHaveBeenCalledTimes(1);
});

it('removes acknowledged review when collection route changes while next load is pending',async()=>{
 const view=render(<MultiRoute/>);await addTwo();
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalled());await screen.findByText('Current multi review');
 const pending=deferred<unknown>();mocks.request.mockReturnValue(pending.promise);
 mocks.params={...mocks.params,id:id(99)};
 view.rerender(<MultiRoute/>);
 expect(screen.queryByText('Current multi review')).toBeNull();
 expect(mocks.submit).toHaveBeenCalledTimes(1);
 view.unmount();
});

it('retains acknowledged snapshot and original frames when renewed admission refuses submit',async()=>{
 render(<MultiRoute/>);await addTwo();
 const capability=mocks.caps.getMockImplementation()!;
 mocks.caps.mockReset();mocks.caps.mockImplementationOnce(capability).mockRejectedValueOnce(new Error('qualification revoked')).mockImplementation(capability);
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));
 await screen.findByText(/Submission was not confirmed/);
 expect(mocks.snapshot).toHaveBeenCalledTimes(1);expect(mocks.submit).not.toHaveBeenCalled();
 const snapshot=structuredClone(mocks.snapshot.mock.calls[0].slice(0,3));
 const saved=[...mocks.values.values()].map(raw=>JSON.parse(raw)).find(value=>value.snapshot?.id);
 expect(saved.params.frames.map((value:{asset_version_id:string})=>value.asset_version_id)).toEqual([id(4),id(6)]);
 const retry=screen.getByRole('button',{name:'Retry saved submission'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));fireEvent.click(retry);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalled());
 expect(mocks.snapshot).toHaveBeenCalledTimes(1);expect(mocks.snapshot.mock.calls[0].slice(0,3)).toEqual(snapshot);
 expect(mocks.submit.mock.calls[0][3]).toBe(saved.intent_key);
});

it('does not admit panorama processing for a restricted project Viewer',async()=>{
 mocks.projectCanEdit=false;render(<LocaleProvider><MultiRoute/></LocaleProvider>);
 await screen.findByText('Source collection');
 fireEvent.click(screen.getByText('Create multi-image result'));expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.submit).not.toHaveBeenCalled();
});

it('cancelled initial multi read can reload while a late old collection remains pending',async()=>{
 const old=deferred<unknown>();mocks.request.mockImplementationOnce(()=>old.promise);
 render(<MultiRoute/>);await waitFor(()=>expect(mocks.request).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Reload multi-image selection'));await screen.findByRole('button',{name:'Add photo 1'});
 await act(async()=>old.resolve({id:id(10),tenant_id:id(1),project_id:id(99),name:'Stale old collection',revision:'1'}));
 expect(screen.queryByText('Stale old collection')).toBeNull();expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.submit).not.toHaveBeenCalled();
});

it('keeps a known multi receipt across wrapper recreation after accepted persistence failure',async()=>{
 mocks.failAccepted=true;render(<MultiRoute/>);await addTwo();await clickWhenEnabled(screen.getByRole('button',{name:'Create multi-image result'}));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.submit).toHaveBeenCalledTimes(1);
 mocks.caps.mockRejectedValue(new Error('worker gone'));mocks.availability.mockRejectedValue(new Error('worker gone'));
 await clickWhenEnabled(await screen.findByRole('button',{name:'Retry saved submission'}));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(2));expect(mocks.submit).toHaveBeenCalledTimes(1);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});

it('numbers sparse multi-photo choices and selected frames without changing source versions',async()=>{
 mocks.request.mockImplementation(async(path:string)=>path.includes('/items?')?{revision:'1',items:items().map((item,index)=>({...item,position:(index+1)*1024}))}:{id:mocks.params.id,tenant_id:id(1),project_id:id(3),name:'Sparse collection',revision:'1'});
 render(<MultiRoute/>);await addTwo();
 expect(screen.queryByRole('button',{name:'Add photo 1025'})).toBeNull();expect(screen.getByText(/1\. Photo 1/)).toBeDefined();expect(screen.getByText(/2\. Photo 2/)).toBeDefined();
 expect(mocks.binding.mock.calls.map(call=>call[0])).toEqual([id(204),id(206)]);
 fireEvent.click(screen.getByRole('button',{name:'Create multi-image result'}));await waitFor(()=>expect(mocks.submit).toHaveBeenCalled());
 expect(mocks.submit.mock.calls[0][2].params.frames.map((frame:{asset_version_id:string})=>frame.asset_version_id)).toEqual([id(4),id(6)]);
});
