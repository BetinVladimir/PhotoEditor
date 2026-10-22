// @vitest-environment jsdom
import {expectEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {Review} from './catalog-types';
import type {OwnerAPI} from './owner-api';
import {CullingReviewSelection} from './culling-review-selection';
import {cullingDecisionIntent,type CullingPage,type CullingDecisionRequest} from './culling-selection';
const local=vi.hoisted(()=>({data:new Map<string,string>(),stops:[] as Array<()=>void>,handlers:[] as Array<{label:string;invoke:()=>void}>,write:null as null|((key:string,value:string)=>Promise<void>)}));
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>local.data.get(k)??null,setItem:async(k:string,v:string)=>{if(local.write)return local.write(k,v);local.data.set(k,v)},removeItem:async(k:string)=>{local.data.delete(k)},getAllKeys:async()=>[...local.data.keys()]}}));
vi.mock('./scene-frame-preview',()=>({SceneFramePreview:({frame,onAccessDenied}:{frame:{versionId:string};onAccessDenied:()=>void})=><button aria-label={`Preview ${frame.versionId}`} onClick={onAccessDenied}>Preview</button>}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64),tenant=id(6),actor=id(7),origin='https://culling-dom.example';
const review={id:id(1),run_id:id(2),kind:'culling',state:'pending',revision:1,candidate_hash:sha,reasons:[],created_at:'2026-10-17T00:00:00Z'};
function projection():CullingPage{return {review_id:id(1),run_id:id(2),revision:1,state:'pending',candidate_hash:sha,snapshot_id:id(3),snapshot_digest:sha,never_delete:true,total:1,groups:{artifact_id:id(4),sha256:sha},proposal:{artifact_id:id(5),sha256:sha},selected_item_ids:null,confirmed_eyes_item_ids:null,next_cursor:null,items:[{item_id:id(10),asset_id:id(11),asset_version_id:id(12),scene_id:'scene',exact_group_id:'exact',burst_group_id:'burst',rank:1,score:.8,recommended:true,manual:false,eye_review:true,eye_state:'unknown'}]};}
function fixture(){let current:Review=review;const onDecided=vi.fn(),onAccessDenied=vi.fn(),api={me:vi.fn(async()=>({id:actor})),review:vi.fn(async()=>current),cullingItems:vi.fn(async()=>projection()),decideCulling:vi.fn(async(_review:string,request:CullingDecisionRequest,_key?:string,_signal?:AbortSignal)=>{
 const {next_cursor,...selection}=projection();const intent=cullingDecisionIntent(tenant,actor,selection,request.selected_item_ids,request.confirmed_eyes_item_ids);current={...review,state:'decided',revision:2,decision:'accept',decision_route:'selection',decision_hash:intent.decision_hash};return current;
 })};const ui=()=> <CullingReviewSelection api={api as unknown as OwnerAPI} origin={origin} tenant={tenant} review={review} onDecided={onDecided} onAccessDenied={onAccessDenied}/>;return {api,ui,onDecided,onAccessDenied};}
afterEach(()=>{cleanup();vi.useRealTimers();local.write=null;local.stops=[];local.handlers=[];});beforeEach(()=>{local.data.clear();vi.clearAllMocks()});
it('requires the photographer to confirm eye warnings and submits a nondeleting selection',async()=>{
 const f=fixture();render(f.ui());await screen.findByText(/1 of 1 selected/);
 const submit=screen.getByRole('button',{name:'Confirm photographer selection'});expect(submit.getAttribute('aria-disabled')).toBe('true');
 fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(submit);
 await waitFor(()=>expect(f.onDecided).toHaveBeenCalledTimes(1));expect(f.api.decideCulling.mock.calls[0][1]).toEqual({expected_revision:1,candidate_hash:sha,selected_item_ids:[id(10)],confirmed_eyes_item_ids:[id(10)],never_delete:true});
});
it('retries the same immutable selection after remount following an uncertain response',async()=>{
 const f=fixture();f.api.decideCulling.mockRejectedValue(new Error('network'));const view=render(f.ui());await screen.findByText(/1 of 1 selected/);
 fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));
 await screen.findByText(/Selection is not yet confirmed/);const first=f.api.decideCulling.mock.calls[0];view.unmount();render(f.ui());
 const retry=await screen.findByRole('button',{name:'Retry saved photographer selection'});fireEvent.click(retry);
 await waitFor(()=>expect(f.api.decideCulling).toHaveBeenCalledTimes(2));expect(f.api.decideCulling.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));
});
it('tears down selection authority on media ACL loss and never submits',async()=>{
 const f=fixture();render(f.ui());await screen.findByText(/1 of 1 selected/);fireEvent.click(screen.getByRole('button',{name:`Preview ${id(12)}`}));expect(f.onAccessDenied).toHaveBeenCalledTimes(1);expect(screen.queryByRole('checkbox',{name:'Select photograph 1'})).toBeNull();expect(f.api.decideCulling).not.toHaveBeenCalled();
});
it('does not apply a late target inventory after unmount',async()=>{
 const f=fixture();let release!:(value:CullingPage)=>void;f.api.cullingItems.mockImplementation(()=>new Promise(resolve=>{release=resolve}));const view=render(f.ui());await waitFor(()=>expect(f.api.cullingItems).toHaveBeenCalledTimes(1));view.unmount();await act(async()=>{release(projection())});expect(f.api.decideCulling).not.toHaveBeenCalled();expect(f.onDecided).not.toHaveBeenCalled();
});
it('preserves the pending journal on denied decision receipt and never posts',async()=>{
 const f=fixture();render(f.ui());await screen.findByText(/1 of 1 selected/);fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));f.api.review.mockRejectedValue(new APIError(403,'FORBIDDEN'));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));await waitFor(()=>expect(f.onAccessDenied).toHaveBeenCalledTimes(1));expect(f.api.decideCulling).not.toHaveBeenCalled();expect([...local.data.keys()].some(key=>key.startsWith('photo.culling-decision-attempt.v1:'))).toBe(true);
});
it('does not expose a partial page or submit before all targets arrive',async()=>{
 const f=fixture(),root=projection(),items=Array.from({length:100},(_,n)=>({...root.items[0],item_id:id(100+n),asset_id:id(1000+n),asset_version_id:id(2000+n),rank:n+1,eye_review:false,eye_state:'open' as const}));
 let finish!:(page:CullingPage)=>void;
 f.api.cullingItems.mockImplementationOnce(async()=>({...root,total:101,items,next_cursor:'next-page'})).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(f.ui());await waitFor(()=>expect(f.api.cullingItems).toHaveBeenCalledTimes(2));expect(screen.queryByRole('button',{name:'Confirm photographer selection'})).toBeNull();expect(f.api.decideCulling).not.toHaveBeenCalled();
 await act(async()=>{finish({...root,total:101,items:[{...root.items[0],item_id:id(999),eye_review:false,eye_state:'open'}]})});
 await screen.findByText(/101 of 101 selected/);expect(screen.getByRole('button',{name:'Confirm photographer selection'})).toBeTruthy();
});
it('fences a late accepted decision after unmount while retaining recovery identifiers',async()=>{
 const f=fixture();let finish!:(v:Review)=>void;f.api.decideCulling.mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(f.ui());await screen.findByText(/1 of 1 selected/);fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));await waitFor(()=>expect(f.api.decideCulling).toHaveBeenCalledTimes(1));
 view.unmount();await act(async()=>{finish({...review,state:'decided',revision:2})});expect(f.onDecided).not.toHaveBeenCalled();expect([...local.data.keys()].some(key=>key.startsWith('photo.culling-decision-attempt.v1:'))).toBe(true);
});
it('cancels the complete initial selection read and retries without submitting partial targets',async()=>{
 const f=fixture();let finish!:(value:CullingPage)=>void;f.api.cullingItems.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;}));
 render(f.ui());await waitFor(()=>expect(f.api.cullingItems).toHaveBeenCalledOnce());fireEvent.click(screen.getByRole('button',{name:'Cancel selection request'}));await act(async()=>finish(projection()));expect(screen.queryByRole('button',{name:'Confirm photographer selection'})).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Reload complete selection'}));await screen.findByText(/1 of 1 selected/);expect(f.api.decideCulling).not.toHaveBeenCalled();
});
it('preserves a cancelled selection and retries the same immutable request/key without consuming a late old receipt',async()=>{
 const f=fixture(),complete=f.api.decideCulling.getMockImplementation()!;let finish!:(value:Review)=>void;
 f.api.decideCulling.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockImplementationOnce(complete);
 render(f.ui());await screen.findByText(/1 of 1 selected/);fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));await waitFor(()=>expect(f.api.decideCulling).toHaveBeenCalledOnce());const first=f.api.decideCulling.mock.calls[0];
 fireEvent.click(screen.getByRole('button',{name:'Cancel selection request'}));await act(async()=>finish({...review,state:'decided',revision:2}));expect(f.onDecided).not.toHaveBeenCalled();expect([...local.data.keys()].some(key=>key.startsWith('photo.culling-decision-attempt.v1:'))).toBe(true);
 fireEvent.click(screen.getByRole('button',{name:'Retry saved photographer selection'}));await waitFor(()=>expect(f.onDecided).toHaveBeenCalledOnce());expect(f.api.decideCulling.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));
});


it('bounds journal persistence, preserves the exact selection and refuses a late write POST',async()=>{
 const f=fixture();render(f.ui());await screen.findByText(/1 of 1 selected/);
 let release!:()=>void;local.write=(key,value)=>new Promise(resolve=>{release=()=>{local.data.set(key,value);resolve();};});
 vi.useFakeTimers();fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));
 await act(async()=>{for(let n=0;n<30;n++)await Promise.resolve();});expect(release).toBeTypeOf('function');
 await act(async()=>{await vi.advanceTimersByTimeAsync(15000);});
 expectEnabled(screen.getByRole('button',{name:'Retry saved photographer selection'}));expect(f.api.decideCulling).not.toHaveBeenCalled();
 await act(async()=>{release();});expect(f.api.decideCulling).not.toHaveBeenCalled();
 local.write=null;vi.useRealTimers();fireEvent.click(screen.getByRole('button',{name:'Retry saved photographer selection'}));
 await waitFor(()=>expect(f.onDecided).toHaveBeenCalledOnce());expect(f.api.decideCulling).toHaveBeenCalledOnce();
});

it('old selection Stop and deadline cannot cancel the replacement replay',async()=>{
 const f=fixture();const replies:Array<(value:Review)=>void>=[];f.api.decideCulling.mockImplementation(()=>new Promise(resolve=>replies.push(resolve)));
 render(f.ui());await screen.findByText(/1 of 1 selected/);
 vi.useFakeTimers();fireEvent.click(screen.getByRole('checkbox',{name:'Confirm eye warning photograph 1'}));fireEvent.click(screen.getByRole('button',{name:'Confirm photographer selection'}));
 await act(async()=>{for(let n=0;n<40;n++)await Promise.resolve();});expect(replies).toHaveLength(1);
 const oldStop=local.stops.at(-1)!;await act(async()=>{await vi.advanceTimersByTimeAsync(10000);oldStop();});
 fireEvent.click(screen.getByRole('button',{name:'Retry saved photographer selection'}));
 await act(async()=>{for(let n=0;n<40;n++)await Promise.resolve();});expect(replies).toHaveLength(2);
 const currentSignal=f.api.decideCulling.mock.calls[1][3] as AbortSignal;
 expect(f.api.decideCulling.mock.calls[1].slice(0,3)).toEqual(f.api.decideCulling.mock.calls[0].slice(0,3));
 await act(async()=>{oldStop();await vi.advanceTimersByTimeAsync(5001);});expect(currentSignal.aborted).toBe(false);
 await act(async()=>{await vi.advanceTimersByTimeAsync(9999);});expect(currentSignal.aborted).toBe(true);
 expect(f.onDecided).not.toHaveBeenCalled();expect(local.data.size).toBe(1);
});


it('retained Confirm freezes current choices before actor IO and old checkbox handlers cannot edit a pending or accepted selection',async()=>{
 const f=fixture(),page=projection();f.api.cullingItems.mockResolvedValue({...page,total:2,items:[page.items[0],{...page.items[0],item_id:id(20),asset_id:id(21),asset_version_id:id(22),rank:2,eye_review:false,eye_state:'open'}]});
 render(f.ui());await screen.findByText(/2 of 2 selected/);
 const handler=(label:string)=>local.handlers.filter(item=>item.label===label).at(-1)!.invoke;
 const oldConfirm=handler('Confirm photographer selection'),oldEye=handler('Confirm eye warning photograph 1'),oldFirst=handler('Select photograph 1'),oldSecond=handler('Select photograph 2');
 let release!:(value:{id:string})=>void;f.api.me.mockImplementationOnce(()=>new Promise(resolve=>{release=resolve;}));
 await act(async()=>{oldEye();oldSecond();oldConfirm();});
 expect(release).toBeTypeOf('function');expect(f.api.decideCulling).not.toHaveBeenCalled();
 await act(async()=>{oldFirst();oldEye();oldSecond();});
 expect(screen.getByText(/1 of 2 selected/)).toBeTruthy();
 await act(async()=>{release({id:actor});});await waitFor(()=>expect(f.onDecided).toHaveBeenCalledOnce());
 expect(f.api.decideCulling.mock.calls[0][1]).toMatchObject({selected_item_ids:[id(10)],confirmed_eyes_item_ids:[id(10)]});
 const reads=f.api.me.mock.calls.length;await act(async()=>{oldFirst();oldEye();oldConfirm();});
 expect(f.api.decideCulling).toHaveBeenCalledOnce();expect(f.api.me).toHaveBeenCalledTimes(reads);
 expect(screen.getByText(/1 of 2 selected/)).toBeTruthy();
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const childLabel=unwrap(props.children);if(childLabel==='Cancel selection request')local.stops.push(props.onPress);
const label=props.accessibilityLabel??childLabel;
if(typeof label==='string')local.handlers.push({label,invoke:props.onPress});};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
