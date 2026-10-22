// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({saved:null as unknown,access:vi.fn(),preflight:vi.fn(),capabilities:vi.fn(),me:vi.fn().mockResolvedValue({id:'user'}),run:vi.fn(async(id:string)=>({id})),write:vi.fn(),params:{id:'session',tenant:'tenant'},session:vi.fn(),list:vi.fn(),collection:vi.fn(),snapshot:vi.fn(),analyze:vi.fn(),update:vi.fn(),push:vi.fn()}));
vi.mock('./session-analysis-attempt-store',()=>({SessionAnalysisAttemptStore:class {async read(){return mocks.saved}async write(record:unknown){mocks.write(record);mocks.saved=structuredClone(record)}async clearAccepted(){mocks.saved=null}async discardRejected(){mocks.saved=null}}}));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
let auth={origin:'https://api.example',client:()=>({})};vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./owner-api',()=>({OwnerAPI:class{sessionAnalysisCapabilities=mocks.preflight;projectAccess=mocks.access;capabilities=mocks.capabilities;tenant:string;constructor(_client:unknown,tenant:string){this.tenant=tenant;}me=mocks.me;run=mocks.run;shootSession=mocks.session;sessionAnalyses=mocks.list;collection=mocks.collection;selectionSnapshot=mocks.snapshot;analyzeSession=mocks.analyze;updateShootSession=mocks.update;}}));
vi.mock('./persistent-session-policy-editor',async()=>({PersistentSessionPolicyEditor:(await import('./session-policy-editor')).SessionPolicyEditor}));
import SessionPage from '../screens/sessions/[id]/index';
afterEach(()=>{cleanup();vi.clearAllMocks();lifecycle.active=true;lifecycle.generation=0;mocks.saved=null;mocks.params={id:'session',tenant:'tenant'};auth={origin:'https://api.example',client:()=>({})};});
function prepare(){mocks.preflight.mockResolvedValue({});mocks.access.mockResolvedValue({id:'project',can_edit:true});mocks.capabilities.mockResolvedValue({operations:['session.describe','session.plan'].map(operation=>({operation,resource_class:operation==='session.plan'?'cpu':'gpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready'}))});mocks.session.mockResolvedValue({id:'session',project_id:'project',name:'Shoot',state:'open',revision:'3',collection_id:'collection',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:"calibration",sha256:"a".repeat(64)}}});mocks.list.mockResolvedValue({items:[],next_cursor:null});mocks.collection.mockResolvedValue({id:'collection',revision:'4'});mocks.snapshot.mockResolvedValue({id:'selection'});}
it('starts analysis and retries with the exact key/body and one selection snapshot',async()=>{
 prepare();mocks.analyze.mockRejectedValueOnce(new Error('transport')).mockResolvedValueOnce({run_id:'run'});render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));await screen.findByText(en.scene_retry_analysis);const first=mocks.analyze.mock.calls[0];expect(first[1].manual_references).toEqual([]);expect(first[1].expected_revision).toBe('3');await clickWhenEnabled(screen.getByText(en.scene_retry_analysis));await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.analyze.mock.calls[1][1]).toEqual(first[1]);expect(mocks.analyze.mock.calls[1][2]).toBe(first[2]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});
it('does not navigate to a late result from the previous session',async()=>{
 prepare();let release!:(value:unknown)=>void;mocks.analyze.mockReturnValue(new Promise(resolve=>release=resolve));const view=render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));mocks.params={id:'other',tenant:'tenant'};view.rerender(<SessionPage/>);await act(async()=>release({run_id:'old-run'}));expect(mocks.push).not.toHaveBeenCalled();expect((mocks.analyze.mock.calls[0][3] as AbortSignal).aborted).toBe(true);
});
it('opens an analysis with the exact session and tenant scope',async()=>{
 prepare();mocks.list.mockResolvedValue({items:[{id:'analysis',createdAt:'2026-10-02T12:00:00Z'}],next_cursor:null});render(<SessionPage/>);await screen.findByText('Analysis 1');fireEvent.click(screen.getByText('Analysis 1'));expect(mocks.push).toHaveBeenCalledWith({pathname:'/sessions/[id]/scenes',params:{id:'session',tenant:'tenant',analysis:'analysis'}});
});

it('saves grouping policy with CAS and exact retry key while blocking analysis for unsaved edits',async()=>{
 prepare();mocks.update.mockRejectedValueOnce(new Error('lost response')).mockImplementationOnce(async(_id,request)=>({...request,id:'session',collection_id:'collection',revision:'4'}));
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.change(screen.getByLabelText(en.session_time_gap),{target:{value:'120'}});
 await waitFor(()=>expect(screen.getByText(en.session_analyze).closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true'));
 fireEvent.click(screen.getByText(en.session_policy_save));await screen.findByText(en.session_policy_retry);const first=mocks.update.mock.calls[0];expect(first[1].expected_revision).toBe('3');expect(first[1].policy.time_gap_seconds).toBe(120);expect(first[1].policy.protect_manual_picks).toBe(true);
 await waitFor(()=>expectEnabled(screen.getByText(en.session_policy_retry)));await clickWhenEnabled(screen.getByText(en.session_policy_retry));await waitFor(()=>expect(mocks.update).toHaveBeenCalledTimes(2));await waitFor(()=>expectEnabled(screen.getByText(en.session_analyze)));
 expect(mocks.update.mock.calls[1][1]).toEqual(first[1]);expect(mocks.update.mock.calls[1][2]).toBe(first[2]);
});

it('shows local/server grouping after conflict and saves only after explicit resolution',async()=>{
 prepare();const initial=await mocks.session();mocks.session.mockResolvedValueOnce(initial).mockResolvedValueOnce({...initial,revision:'5',name:'Server name',policy:{...initial.policy,time_gap_seconds:90,neighbor_count:8,camera_offsets:[{camera_hash:'b'.repeat(64),seconds:10}]}});
 mocks.update.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockImplementationOnce(async(_id,request)=>({...request,id:'session',collection_id:'collection',revision:'6'}));
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.change(screen.getByLabelText(en.session_time_gap),{target:{value:'120'}});fireEvent.click(screen.getByText(en.session_policy_save));
 await screen.findByText(en.session_policy_server+': 90 / 8');expect(screen.getByText(en.session_policy_local+': 120 / 4')).toBeDefined();expect(mocks.update).toHaveBeenCalledTimes(1);
 await waitFor(()=>expectEnabled(screen.getByText(en.session_policy_resolve)));await clickWhenEnabled(screen.getByText(en.session_policy_resolve));
 await screen.findByText('Server name');const request=mocks.update.mock.calls[1][1];expect(request.expected_revision).toBe('5');expect(request.policy.time_gap_seconds).toBe(120);expect(request.policy.neighbor_count).toBe(4);expect(request.policy.camera_offsets).toEqual([{camera_hash:'b'.repeat(64),seconds:10}]);expect(mocks.update.mock.calls[1][2]).not.toBe(mocks.update.mock.calls[0][2]);
});
it('does not replace a newer authorized session with an older idempotent response',async()=>{prepare();const current={...(await mocks.session()),revision:'5',name:'Current shoot'};mocks.session.mockResolvedValue(current);mocks.update.mockResolvedValue({...current,revision:'4',name:'Historical shoot'});render(<SessionPage/>);await screen.findByText('Current shoot');fireEvent.change(screen.getByLabelText(en.session_time_gap),{target:{value:'120'}});fireEvent.click(screen.getByRole('button',{name:en.session_policy_save}));await waitFor(()=>expect(mocks.update).toHaveBeenCalledTimes(1));await waitFor(()=>expect(screen.getByRole('button',{name:en.scene_reload}).hasAttribute('disabled')).toBe(false));expect(screen.getByRole('heading').textContent).toBe('Current shoot');expect(screen.queryByText('Historical shoot')).toBeNull();});

it('aborts an old-account analysis even when the session route remains unchanged',async()=>{
 prepare();let finish!:(value:{run_id:string})=>void;
 mocks.analyze.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<SessionPage/>);await screen.findByText('Shoot');
 fireEvent.click(screen.getByText(en.session_analyze));await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.analyze.mock.calls[0][3] as AbortSignal;
 auth={origin:'https://api.example',client:()=>({})};view.rerender(<SessionPage/>);
 await waitFor(()=>expect(mocks.list).toHaveBeenCalledTimes(2));
 expect(oldSignal.aborted).toBe(true);
 await act(async()=>finish({run_id:'old-account-run'}));expect(mocks.push).not.toHaveBeenCalled();
 // A new client for the same actor recovers the durable exact request, never the late response.
 expect(screen.getByText(en.scene_retry_analysis)).toBeTruthy();expect(mocks.analyze).toHaveBeenCalledTimes(1);
});

it('uses the session lifecycle signal for collection and snapshot preparation',async()=>{
 prepare();mocks.analyze.mockResolvedValue({run_id:'run'});
 render(<SessionPage/>);await screen.findByText('Shoot');
 fireEvent.click(screen.getByText(en.session_analyze));await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));
 const signal=mocks.analyze.mock.calls[0][3] as AbortSignal;
 expect(mocks.collection.mock.calls[0][1]).toBe(signal);
 expect(mocks.snapshot.mock.calls[0][1]).toBeUndefined();expect(mocks.snapshot.mock.calls[0][2]).toBe(signal);
});

it('restores an uncertain analysis after remount without creating another snapshot',async()=>{
 prepare();mocks.analyze.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({run_id:'recovered-run'});
 const first=render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));
 await screen.findByText(en.scene_retry_analysis);
 const original=mocks.analyze.mock.calls[0];expect(mocks.saved).not.toBeNull();first.unmount();
 render(<SessionPage/>);const retry=await screen.findByText(en.scene_retry_analysis);await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:'recovered-run',tenant:'tenant'}}));
 expect(mocks.analyze.mock.calls[1][1]).toEqual(original[1]);expect(mocks.analyze.mock.calls[1][2]).toBe(original[2]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});
it('recovers a saved accepted run without resubmitting analysis',async()=>{
 prepare();mocks.saved={schema_version:1,session_id:'session',key:'key',request:{expected_revision:'3',selection_snapshot_id:'snapshot',policy:{},manual_references:[]},ticket:{run_id:'saved-run'}};
 render(<SessionPage/>);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:'saved-run',tenant:'tenant'}}));
 expect(mocks.run).toHaveBeenCalledWith('saved-run',expect.any(AbortSignal));expect(mocks.analyze).not.toHaveBeenCalled();expect(mocks.snapshot).not.toHaveBeenCalled();
});
it('saves the analysis request before posting and the ticket before run verification',async()=>{
 prepare();mocks.analyze.mockImplementationOnce(async()=>{expect(mocks.saved).toMatchObject({ticket:null});return {run_id:'run'}});
 mocks.run.mockImplementationOnce(async(id:string)=>{expect(mocks.saved).toMatchObject({ticket:{run_id:id}});return {id}});
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.write).toHaveBeenCalledTimes(2);
});

it('restores pending initial analysis after background without making a second snapshot',async()=>{
 prepare();let finish!:(value:{run_id:string})=>void;
 mocks.analyze.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({run_id:'resumed-run'});
 const view=render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));
 await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));
 const first=mocks.analyze.mock.calls[0];
 lifecycle.active=false;view.rerender(<SessionPage/>);
 expect((first[3] as AbortSignal).aborted).toBe(true);
 await act(async()=>finish({run_id:'late-run'}));expect(mocks.push).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<SessionPage/>);
 const retry=await screen.findByText(en.scene_retry_analysis);
 await waitFor(()=>expectEnabled(retry));
 await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:'resumed-run',tenant:'tenant'}}));
 expect(mocks.analyze.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3));expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});
it('does not read session metadata while initially in background',async()=>{
 prepare();lifecycle.active=false;
 const view=render(<SessionPage/>);expect(mocks.me).not.toHaveBeenCalled();expect(mocks.session).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<SessionPage/>);
 await screen.findByText('Shoot');expect(mocks.me).toHaveBeenCalledTimes(1);
});

it('does not prepare a snapshot or send analysis when deployment operations are unavailable',async()=>{
 prepare();mocks.capabilities.mockResolvedValue({operations:[]});
 render(<SessionPage/>);await screen.findByText('Shoot');
 fireEvent.click(screen.getByText(en.session_analyze));
 await screen.findByText(en.scene_reference_unavailable);
 expect(mocks.snapshot).not.toHaveBeenCalled();expect(mocks.collection).not.toHaveBeenCalled();
 expect(mocks.analyze).not.toHaveBeenCalled();expect(mocks.saved).toBeNull();
});
it('replays a persisted uncertain analysis without requiring new-operation capabilities',async()=>{
 prepare();mocks.analyze.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({run_id:'run'});
 const view=render(<SessionPage/>);await screen.findByText('Shoot');
 fireEvent.click(screen.getByText(en.session_analyze));await screen.findByRole('alert');
 await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));const original=mocks.analyze.mock.calls[0];
 view.unmount();mocks.preflight.mockClear();mocks.capabilities.mockClear();mocks.capabilities.mockResolvedValue({operations:[]});
 render(<SessionPage/>);const retry=await screen.findByText(en.scene_retry_analysis);
 await waitFor(()=>expectEnabled(retry));
 await clickWhenEnabled(retry);await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 expect(mocks.preflight).not.toHaveBeenCalled();expect(mocks.capabilities).not.toHaveBeenCalled();expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 expect(mocks.analyze.mock.calls[1].slice(0,3)).toEqual(original.slice(0,3));
});

it('shows analysis history but no mutation controls for a project viewer',async()=>{
 prepare();mocks.access.mockResolvedValue({id:'project',can_edit:false});
 mocks.list.mockResolvedValue({items:[{id:'analysis',createdAt:'2026-10-17T00:00:00Z'}],next_cursor:null});
 render(<SessionPage/>);await screen.findByText('Analysis 1');
 expect(screen.queryByText(en.session_analyze)).toBeNull();
 expect(screen.queryByText(en.session_close)).toBeNull();
 expect(screen.queryByText(en.session_policy_save)).toBeNull();
 expect(mocks.analyze).not.toHaveBeenCalled();expect(mocks.update).not.toHaveBeenCalled();
 expect(mocks.access).toHaveBeenCalledWith('project',expect.any(AbortSignal));
});

vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));

it('cancels initial storage/identity waiting and refuses a late old session after readonly reload',async()=>{
 prepare();let finish!:(value:unknown)=>void;mocks.session.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 render(<SessionPage/>);await waitFor(()=>expect(mocks.session).toHaveBeenCalledTimes(1));const old=mocks.session.mock.calls[0][1] as AbortSignal;
 fireEvent.click(screen.getByText(en.read_cancel));expect(old.aborted).toBe(true);fireEvent.click(screen.getByText(en.scene_reload));await screen.findByText('Shoot');
 await act(async()=>{finish({id:'session',name:'Stale shoot',project_id:'project'})});expect(screen.queryByText('Stale shoot')).toBeNull();expect(mocks.analyze).not.toHaveBeenCalled();
});

it('refuses a new analysis when exact calibrated selection preflight fails before journal or POST',async()=>{
 prepare();mocks.preflight.mockRejectedValue(new APIError(503,'CALIBRATION_UNAVAILABLE'));
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));
 await screen.findByText(en.scene_reference_unavailable);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 expect(mocks.analyze).not.toHaveBeenCalled();expect(mocks.write).not.toHaveBeenCalled();expect(mocks.saved).toBeNull();
});
