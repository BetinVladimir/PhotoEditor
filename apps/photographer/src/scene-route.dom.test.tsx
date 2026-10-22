// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {SceneReferenceAttempt} from './scene-reference-attempt-store';
import type {SceneApplyAttempt} from './scene-apply-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const mocks=vi.hoisted(()=>({storage:new Map<string,string>(),auth:{origin:'https://api.example',client:()=>({})},access:vi.fn(),push:vi.fn(),run:vi.fn(),me:vi.fn(),sceneBoard:vi.fn(),analyzeSession:vi.fn(),applySceneAnalysis:vi.fn(),shootSession:vi.fn(),collection:vi.fn(),selectionSnapshot:vi.fn(),boardProps:vi.fn(),applyProps:vi.fn()}));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:mocks.push},useLocalSearchParams:()=>({id:'00000000-0000-4000-8000-000000000004',tenant:'00000000-0000-4000-8000-000000000002',analysis:'00000000-0000-4000-8000-000000000005'})}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.storage.get(k)??null,setItem:async(k:string,v:string)=>{mocks.storage.set(k,v);},removeItem:async(k:string)=>{mocks.storage.delete(k);},getAllKeys:async()=>[...mocks.storage.keys()]}}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {sessionAnalysisCapabilities=async()=>({});projectAccess=mocks.access;capabilities=async()=>({operations:['session.describe','session.plan'].map(operation=>({operation,resource_class:operation==='session.plan'?'cpu':'gpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready'}))});tenant=id(2);run=mocks.run;me=mocks.me;sceneBoard=mocks.sceneBoard;analyzeSession=mocks.analyzeSession;applySceneAnalysis=mocks.applySceneAnalysis;shootSession=mocks.shootSession;collection=mocks.collection;selectionSnapshot=mocks.selectionSnapshot;}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./scene-board-view',()=>({SceneBoardView:(props:unknown)=>{mocks.boardProps(props);return null}}));
vi.mock('./scene-apply-panel',()=>({SceneApplyPanel:(props:unknown)=>{mocks.applyProps(props);return null}}));
import SessionScenes from '../screens/sessions/[id]/scenes';
function saved(ticket:SceneReferenceAttempt['ticket']=null):SceneReferenceAttempt{return {schema_version:1,session_id:id(4),analysis_id:id(5),key:id(6),request:{expected_revision:'3',selection_snapshot_id:id(7),policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(8),sha256:'a'.repeat(64)}},manual_references:[{previous_analysis_id:id(5),group_id:id(9),asset_version_id:id(10)}]},ticket};}
function install(record:SceneReferenceAttempt){mocks.shootSession.mockResolvedValue({id:id(4),project_id:id(20)});mocks.access.mockResolvedValue({id:id(20),can_edit:true});mocks.run.mockImplementation(async(id:string)=>({id}));mocks.storage.set('photo.scene-reference-attempt.v1:'+JSON.stringify(['https://api.example',id(1),id(2),id(4),id(5)]),JSON.stringify(record));vi.stubEnv('EXPO_PUBLIC_API_ORIGIN','https://api.example');mocks.me.mockResolvedValue({id:id(1)});}
function installApply(ticket:SceneApplyAttempt['ticket']={run_id:id(17)}):SceneApplyAttempt{mocks.shootSession.mockResolvedValue({id:id(4),project_id:id(20)});mocks.access.mockResolvedValue({id:id(20),can_edit:true});mocks.run.mockImplementation(async(id:string)=>({id}));const record:SceneApplyAttempt={schema_version:1,session_id:id(4),analysis_id:id(5),key:id(15),body:{plan_sha:'a'.repeat(64),approved_groups:[id(9)],recipe:{artifact_id:id(16),sha256:'b'.repeat(64)},expected_revision:'1'},ticket};mocks.storage.set('photo.scene-apply-attempt.v1:'+JSON.stringify(['https://api.example',id(1),id(2),id(4),id(5)]),JSON.stringify(record));vi.stubEnv('EXPO_PUBLIC_API_ORIGIN','https://api.example');mocks.me.mockResolvedValue({id:id(1)});return record;}
afterEach(()=>{cleanup();vi.clearAllMocks();vi.unstubAllEnvs();mocks.storage.clear();mocks.auth={origin:'https://api.example',client:()=>({})};lifecycle.active=true;lifecycle.generation=0;});
it('opens an acknowledged run without needing the old scene board',async()=>{install(saved({run_id:id(11)}));mocks.sceneBoard.mockRejectedValue(new Error('gone'));render(<SessionScenes/>);await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(11),tenant:id(2)}}));expect(mocks.sceneBoard).not.toHaveBeenCalled();expect(mocks.analyzeSession).not.toHaveBeenCalled();});
it('opens an acknowledged Apply child without needing the old scene board',async()=>{installApply();mocks.sceneBoard.mockRejectedValue(new Error('gone'));render(<SessionScenes/>);await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(17),tenant:id(2)}}));expect(mocks.sceneBoard).not.toHaveBeenCalled();expect(mocks.analyzeSession).not.toHaveBeenCalled();});
it('retries a pending Apply with exact saved request when the board is unavailable',async()=>{const record=installApply(null);mocks.sceneBoard.mockRejectedValue(new Error('gone'));mocks.applySceneAnalysis.mockResolvedValue({run_id:id(18)});render(<SessionScenes/>);const retry=await screen.findByText('scene_apply_retry');await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.applySceneAnalysis).toHaveBeenCalledTimes(1));expect(mocks.applySceneAnalysis.mock.calls[0].slice(0,3)).toEqual([id(5),record.body,id(15)]);await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(18),tenant:id(2)}}));expect(mocks.analyzeSession).not.toHaveBeenCalled();});
it('retries a pending reference with its saved key when board loading fails',async()=>{const record=saved();install(record);mocks.sceneBoard.mockRejectedValue(new Error('gone'));mocks.analyzeSession.mockResolvedValue({run_id:id(12)});render(<SessionScenes/>);fireEvent.click(await screen.findByText('scene_retry_analysis'));await waitFor(()=>expect(mocks.analyzeSession).toHaveBeenCalledTimes(1));expect(mocks.analyzeSession.mock.calls[0].slice(0,3)).toEqual([id(4),record.request,id(6)]);await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(12),tenant:id(2)}}));});

it('reads the reference journal from the authenticated origin instead of the build default',async()=>{
 install(saved({run_id:id(11)}));vi.stubEnv('EXPO_PUBLIC_API_ORIGIN','https://other-server.example');
 render(<SessionScenes/>);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(11),tenant:id(2)}}));
 expect(mocks.sceneBoard).not.toHaveBeenCalled();
});

it('aborts old-account reference submission and does not install its late ticket',async()=>{
 install(saved());mocks.sceneBoard.mockRejectedValue(new Error('gone'));
 let finish!:(value:{run_id:string})=>void;
 mocks.analyzeSession.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<SessionScenes/>);
 fireEvent.click(await screen.findByText('scene_retry_analysis'));
 await waitFor(()=>expect(mocks.analyzeSession).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.analyzeSession.mock.calls[0][3] as AbortSignal;
 mocks.auth={origin:'https://new-server.example',client:()=>({})};
 view.rerender(<SessionScenes/>);
 await waitFor(()=>expect(mocks.me).toHaveBeenCalledTimes(2));
 expect(oldSignal.aborted).toBe(true);
 await act(async()=>finish({run_id:id(22)}));
 await waitFor(()=>expect(screen.queryByText('scene_retry_analysis')).toBeNull());
 expect(mocks.push).not.toHaveBeenCalled();expect(mocks.analyzeSession).toHaveBeenCalledTimes(1);
 const record=JSON.parse(mocks.storage.get('photo.scene-reference-attempt.v1:'+JSON.stringify(['https://api.example',id(1),id(2),id(4),id(5)]))!);
 expect(record.ticket).toBeNull();
});

it('aborts background reference submission and restores the same durable attempt on return',async()=>{
 const record=saved();install(record);mocks.sceneBoard.mockRejectedValue(new Error('gone'));
 let finish!:(value:{run_id:string})=>void;
 mocks.analyzeSession.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({run_id:id(23)});
 const view=render(<SessionScenes/>);
 fireEvent.click(await screen.findByText('scene_retry_analysis'));
 await waitFor(()=>expect(mocks.analyzeSession).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.analyzeSession.mock.calls[0][3] as AbortSignal;
 lifecycle.active=false;view.rerender(<SessionScenes/>);
 expect(oldSignal.aborted).toBe(true);expect(screen.queryByText('scene_retry_analysis')).toBeNull();
 await act(async()=>finish({run_id:id(22)}));expect(mocks.push).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<SessionScenes/>);
 fireEvent.click(await screen.findByText('scene_retry_analysis'));
 await waitFor(()=>expect(mocks.analyzeSession).toHaveBeenCalledTimes(2));
 expect(mocks.analyzeSession.mock.calls[1].slice(0,3)).toEqual([id(4),record.request,record.key]);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(23),tenant:id(2)}}));
});

it('does not load account or scene metadata while initially backgrounded',async()=>{
 lifecycle.active=false;install(saved());mocks.sceneBoard.mockRejectedValue(new Error('gone'));
 const view=render(<SessionScenes/>);
 expect(mocks.me).not.toHaveBeenCalled();expect(mocks.sceneBoard).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<SessionScenes/>);
 await screen.findByText('scene_retry_analysis');expect(mocks.me).toHaveBeenCalledTimes(1);
});

it('retains an accepted Apply when verification fails and retries only GET run',async()=>{
 installApply();mocks.run.mockRejectedValueOnce(new Error('offline'));
 render(<SessionScenes/>);await screen.findByRole('alert');
 expect(mocks.push).not.toHaveBeenCalled();
 const retry=await screen.findByText('scene_apply_retry');
 await waitFor(()=>expectEnabled(retry));
 await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.applySceneAnalysis).not.toHaveBeenCalled();expect(mocks.run).toHaveBeenCalledTimes(2);
});
it('retains an accepted reference analysis through verification failure without resubmission',async()=>{
 install(saved({run_id:id(11)}));mocks.run.mockRejectedValueOnce(new Error('offline'));
 render(<SessionScenes/>);await screen.findByRole('alert');
 fireEvent.click(await screen.findByText('scene_retry_analysis'));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.analyzeSession).not.toHaveBeenCalled();expect(mocks.run).toHaveBeenCalledTimes(2);
});

it('hides accepted Apply recovery after current access is revoked while preserving the journal',async()=>{
 installApply();const before=[...mocks.storage.entries()];
 mocks.run.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<SessionScenes/>);await screen.findByText('scene_access_denied');
 expect(screen.queryByText('scene_apply_retry')).toBeNull();
 expect(mocks.push).not.toHaveBeenCalled();expect(mocks.applySceneAnalysis).not.toHaveBeenCalled();
 expect([...mocks.storage.entries()]).toEqual(before);
});
it('keeps an accepted Apply retryable when GET run reports a temporary conflict',async()=>{
 installApply();mocks.run.mockRejectedValueOnce(new APIError(409,'RUN_CONFLICT'));
 render(<SessionScenes/>);await screen.findByRole('alert');
 const retry=await screen.findByText('scene_apply_retry');
 await waitFor(()=>expectEnabled(retry));
 await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.applySceneAnalysis).not.toHaveBeenCalled();
});

it('keeps the scene board read-only for an explicit project viewer',async()=>{
 install(saved());mocks.access.mockResolvedValue({id:id(20),can_edit:false});
 mocks.sceneBoard.mockResolvedValue({planSha:'a'.repeat(64),groups:[]});
 render(<SessionScenes/>);await waitFor(()=>expect(mocks.boardProps).toHaveBeenCalled());
 const props=mocks.boardProps.mock.calls.at(-1)![0];expect(props.onReference).toBeUndefined();
 expect(mocks.applyProps).not.toHaveBeenCalled();expect(screen.queryByText('scene_retry_analysis')).toBeNull();
 expect(mocks.analyzeSession).not.toHaveBeenCalled();expect(mocks.applySceneAnalysis).not.toHaveBeenCalled();
});

it('cancels initial identity and allows readonly reload without accepting its late old result',async()=>{
 install(saved());mocks.sceneBoard.mockResolvedValue({planSha:'a'.repeat(64),groups:[]});
 let finish!:(value:unknown)=>void;mocks.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<SessionScenes/>);await waitFor(()=>expect(mocks.me).toHaveBeenCalled());const old=mocks.me.mock.calls[0][0] as AbortSignal;
 fireEvent.click(screen.getByText('read_cancel'));expect(old.aborted).toBe(true);fireEvent.click(screen.getByText('scene_reload'));await waitFor(()=>expect(mocks.boardProps).toHaveBeenCalled());
 await act(async()=>{finish({id:id(9)})});expect(mocks.analyzeSession).not.toHaveBeenCalled();expect(mocks.applySceneAnalysis).not.toHaveBeenCalled();
});
