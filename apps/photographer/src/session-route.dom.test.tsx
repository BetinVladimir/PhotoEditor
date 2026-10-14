// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({params:{id:'session',tenant:'tenant'},session:vi.fn(),list:vi.fn(),collection:vi.fn(),snapshot:vi.fn(),analyze:vi.fn(),update:vi.fn(),push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
const auth={client:()=>({})};vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./owner-api',()=>({OwnerAPI:class{tenant:string;constructor(_client:unknown,tenant:string){this.tenant=tenant;}shootSession=mocks.session;sessionAnalyses=mocks.list;collection=mocks.collection;selectionSnapshot=mocks.snapshot;analyzeSession=mocks.analyze;updateShootSession=mocks.update;}}));
vi.mock('./persistent-session-policy-editor',async()=>({PersistentSessionPolicyEditor:(await import('./session-policy-editor')).SessionPolicyEditor}));
import SessionPage from '../app/sessions/[id]/index';
afterEach(()=>{cleanup();vi.clearAllMocks();mocks.params={id:'session',tenant:'tenant'};});
function prepare(){mocks.session.mockResolvedValue({id:'session',name:'Shoot',state:'open',revision:'3',collection_id:'collection',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:"calibration",sha256:"a".repeat(64)}}});mocks.list.mockResolvedValue({items:[],next_cursor:null});mocks.collection.mockResolvedValue({id:'collection',revision:'4'});mocks.snapshot.mockResolvedValue({id:'selection'});}
it('starts analysis and retries with the exact key/body and one selection snapshot',async()=>{
 prepare();mocks.analyze.mockRejectedValueOnce(new Error('transport')).mockResolvedValueOnce({run_id:'run'});render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText(en.session_analyze));await screen.findByText(en.scene_retry_analysis);const first=mocks.analyze.mock.calls[0];expect(first[1].manual_references).toEqual([]);expect(first[1].expected_revision).toBe('3');fireEvent.click(screen.getByText(en.scene_retry_analysis));await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));expect(mocks.analyze.mock.calls[1][1]).toEqual(first[1]);expect(mocks.analyze.mock.calls[1][2]).toBe(first[2]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
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
 await waitFor(()=>expect(screen.getByText(en.session_policy_retry).closest('[role="button"]')?.getAttribute('aria-disabled')).not.toBe('true'));fireEvent.click(screen.getByText(en.session_policy_retry));await waitFor(()=>expect(mocks.update).toHaveBeenCalledTimes(2));await waitFor(()=>expect(screen.getByText(en.session_analyze).closest('[role="button"]')?.getAttribute('aria-disabled')).not.toBe('true'));
 expect(mocks.update.mock.calls[1][1]).toEqual(first[1]);expect(mocks.update.mock.calls[1][2]).toBe(first[2]);
});

it('shows local/server grouping after conflict and saves only after explicit resolution',async()=>{
 prepare();const initial=await mocks.session();mocks.session.mockResolvedValueOnce(initial).mockResolvedValueOnce({...initial,revision:'5',name:'Server name',policy:{...initial.policy,time_gap_seconds:90,neighbor_count:8,camera_offsets:[{camera_hash:'b'.repeat(64),seconds:10}]}});
 mocks.update.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockImplementationOnce(async(_id,request)=>({...request,id:'session',collection_id:'collection',revision:'6'}));
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.change(screen.getByLabelText(en.session_time_gap),{target:{value:'120'}});fireEvent.click(screen.getByText(en.session_policy_save));
 await screen.findByText(en.session_policy_server+': 90 / 8');expect(screen.getByText(en.session_policy_local+': 120 / 4')).toBeDefined();expect(mocks.update).toHaveBeenCalledTimes(1);
 await waitFor(()=>expect(screen.getByText(en.session_policy_resolve).closest('[role="button"]')?.getAttribute('aria-disabled')).not.toBe('true'));fireEvent.click(screen.getByText(en.session_policy_resolve));
 await screen.findByText('Server name');const request=mocks.update.mock.calls[1][1];expect(request.expected_revision).toBe('5');expect(request.policy.time_gap_seconds).toBe(120);expect(request.policy.neighbor_count).toBe(4);expect(request.policy.camera_offsets).toEqual([{camera_hash:'b'.repeat(64),seconds:10}]);expect(mocks.update.mock.calls[1][2]).not.toBe(mocks.update.mock.calls[0][2]);
});
it('does not replace a newer authorized session with an older idempotent response',async()=>{prepare();const current={...(await mocks.session()),revision:'5',name:'Current shoot'};mocks.session.mockResolvedValue(current);mocks.update.mockResolvedValue({...current,revision:'4',name:'Historical shoot'});render(<SessionPage/>);await screen.findByText('Current shoot');fireEvent.change(screen.getByLabelText(en.session_time_gap),{target:{value:'120'}});fireEvent.click(screen.getByRole('button',{name:en.session_policy_save}));await waitFor(()=>expect(mocks.update).toHaveBeenCalledTimes(1));await waitFor(()=>expect(screen.getByRole('button',{name:en.scene_reload}).hasAttribute('disabled')).toBe(false));expect(screen.getByRole('heading').textContent).toBe('Current shoot');expect(screen.queryByText('Historical shoot')).toBeNull();});
