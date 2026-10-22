// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render as renderUI,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({data:new Map<string,string>(),analyze:vi.fn(),run:vi.fn(),snapshot:vi.fn(),push:vi.fn(),failWrite:false}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:id(4),tenant:id(2)}),router:{push:mocks.push}}));
const client={},auth={origin:'https://recovery.example',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./persistent-session-policy-editor',()=>({PersistentSessionPolicyEditor:()=>null}));
vi.mock('./session-lifecycle',()=>({SessionLifecycle:()=>null}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.data.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failWrite)throw new Error('disk unavailable');mocks.data.set(k,v)},removeItem:async(k:string)=>{mocks.data.delete(k)},getAllKeys:async()=>[...mocks.data.keys()]}}));
vi.mock('./owner-api',()=>({OwnerAPI:class {sessionAnalysisCapabilities=async()=>({});projectAccess=async()=>({id:id(6),can_edit:true});capabilities=async()=>({operations:['session.describe','session.plan'].map(operation=>({operation,resource_class:operation==='session.plan'?'cpu':'gpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready'}))});
 tenant=id(2);me=async()=>({id:id(1)});
 shootSession=async()=>({id:id(4),project_id:id(6),name:'Shoot',state:'open',revision:'3',collection_id:id(5),policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(8),sha256:'a'.repeat(64)}}});
 sessionAnalyses=async()=>({items:[],next_cursor:null});collection=async()=>({id:id(5),revision:'4'});
 selectionSnapshot=mocks.snapshot;analyzeSession=mocks.analyze;run=mocks.run;
}}));
import {WorkspaceAccessContext} from './workspace-access-context';
import type {ReactNode} from 'react';
function render(node:ReactNode){return renderUI(<WorkspaceAccessContext.Provider value={{tenant:id(2),actor:id(1),role:'editor',canEdit:true,canAdmin:false}}>{node}</WorkspaceAccessContext.Provider>)}
import SessionPage from '../screens/sessions/[id]/index';
const journalKey='photo.session-analysis-attempt.v1:'+JSON.stringify(['https://recovery.example',id(1),id(2),id(4)]);
afterEach(()=>{cleanup();mocks.data.clear();mocks.failWrite=false;vi.resetAllMocks()});
function prepare(){mocks.snapshot.mockResolvedValue({id:id(7)});mocks.run.mockImplementation(async(runID:string)=>({id:runID}))}
async function retry(){const button=await screen.findByText('scene_retry_analysis');await waitFor(()=>expectEnabled(button));await clickWhenEnabled(button)}

it('writes before POST and replays persisted exact request/key after remount',async()=>{
 prepare();mocks.analyze.mockImplementationOnce(async()=>{expect(JSON.parse(mocks.data.get(journalKey)!)).toMatchObject({ticket:null});throw new Error('offline')}).mockResolvedValueOnce({run_id:id(12)});
 const first=render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('session_analyze'));
 await screen.findByRole('alert');await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));const original=mocks.analyze.mock.calls[0];
 first.unmount();render(<SessionPage/>);await retry();
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(12),tenant:id(2)}}));
 expect(mocks.analyze.mock.calls[1].slice(0,3)).toEqual(original.slice(0,3));expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 await waitFor(()=>expect(mocks.data.has(journalKey)).toBe(false));
});

it('does not POST when durable storage fails',async()=>{
 prepare();mocks.failWrite=true;
 render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('session_analyze'));
 await screen.findByRole('alert');expect(mocks.analyze).not.toHaveBeenCalled();expect(mocks.data.has(journalKey)).toBe(false);
});

it('retains an accepted ticket across failed run verification and recovers without another POST',async()=>{
 prepare();mocks.analyze.mockResolvedValue({run_id:id(12)});mocks.run.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({id:id(12)});
 const first=render(<SessionPage/>);await screen.findByText('Shoot');fireEvent.click(screen.getByText('session_analyze'));
 await screen.findByText('scene_retry_analysis');await waitFor(()=>expect(mocks.run).toHaveBeenCalledTimes(1));
 expect(JSON.parse(mocks.data.get(journalKey)!)).toMatchObject({ticket:{run_id:id(12)}});expect(mocks.push).not.toHaveBeenCalled();
 first.unmount();render(<SessionPage/>);
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(12),tenant:id(2)}}));
 expect(mocks.analyze).toHaveBeenCalledTimes(1);expect(mocks.snapshot).toHaveBeenCalledTimes(1);
});
