import {APIError} from '@photoeditor/api-client';
// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {RunStatus} from './catalog-types';
const mocks=vi.hoisted(()=>({params:{id:'run',tenant:'tenant'} as {id:string;tenant:string;semanticWorkflow?:string},request:vi.fn(),stream:vi.fn(),push:vi.fn(),resolve:vi.fn()}));
const journals=vi.hoisted(()=>({retryKey:0,retryScope:'tenant:run',cancelScope:'tenant:run',retryPending:null as null|{version:1;key:string;expected_revision:string;created_at:number},cancelPending:null as null|{version:1;key:string;created_at:number},retryDiscard:vi.fn(),cancelDiscard:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
vi.mock('./run-retry-attempt-store',()=>({RunRetryAttemptStore:class{private scope:string;constructor(_storage:unknown,_origin:string,_user:string,tenant:string,run:string){this.scope=tenant+':'+run}async read(){return journals.retryScope===this.scope?journals.retryPending:null}async begin(expected_revision:string){if(journals.retryScope===this.scope&&journals.retryPending)return journals.retryPending;journals.retryKey++;journals.retryScope=this.scope;journals.retryPending={version:1,key:`retry-${journals.retryKey}`,expected_revision,created_at:1};return journals.retryPending}async acknowledge(key:string){if(journals.retryScope===this.scope&&journals.retryPending?.key===key)journals.retryPending=null}async discard(key:string){journals.retryDiscard(key);if(journals.retryScope===this.scope&&journals.retryPending?.key===key)journals.retryPending=null}}}));
vi.mock('./run-cancel-attempt-store',()=>({RunCancelAttemptStore:class{private scope:string;constructor(_storage:unknown,_origin:string,_user:string,tenant:string,run:string){this.scope=tenant+':'+run}async read(){return journals.cancelScope===this.scope?journals.cancelPending:null}async begin(){if(journals.cancelScope===this.scope&&journals.cancelPending)return journals.cancelPending;journals.cancelScope=this.scope;journals.cancelPending={version:1,key:'cancel-1',created_at:1};return journals.cancelPending}async acknowledge(key:string){if(journals.cancelScope===this.scope&&journals.cancelPending?.key===key)journals.cancelPending=null}async discard(key:string){journals.cancelDiscard(key);if(journals.cancelScope===this.scope&&journals.cancelPending?.key===key)journals.cancelPending=null}}}));
const client={request:mocks.request,eventStream:mocks.stream,semanticRunIntent:mocks.resolve};const auth={origin:'https://app.test',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
import {OwnerAPI} from './owner-api';
import RunScreen from '../app/runs/[id]';
beforeEach(()=>{vi.spyOn(OwnerAPI.prototype,'me').mockResolvedValue({id:'00000000-0000-0000-0000-000000000001',email:'test@example.com',locale:'en',theme:'light',timezone:'UTC',revision:'1'});});
it('opens semantic review using resolved intent instead of workflow route ID',async()=>{
 const workflow='22222222-2222-4222-8222-222222222222',intent='33333333-3333-4333-8333-333333333333';
 mocks.params={id:'run',tenant:'tenant',semanticWorkflow:workflow};streaming();
 mocks.request.mockResolvedValue({...value('2','waiting_review'),kind:'v2'});
 mocks.resolve.mockResolvedValue({run_id:'run',workflow_id:workflow,intent_id:intent,revision:'2',state:'waiting_review'});
 render(<RunScreen/>);fireEvent.click(await screen.findByText('Open batch review'));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/semantic/[id]',params:{tenant:'tenant',id:intent}});
});
it('discovers semantic review from a direct V2 run URL without a workflow hint',async()=>{
 const workflow='22222222-2222-4222-8222-222222222222',intent='33333333-3333-4333-8333-333333333333';
 streaming();mocks.request.mockResolvedValue({...value('2','waiting_review'),kind:'v2'});
 mocks.resolve.mockResolvedValue({run_id:'run',workflow_id:workflow,intent_id:intent,revision:'2',state:'waiting_review'});
 render(<RunScreen/>);fireEvent.click(await screen.findByText('Open batch review'));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/semantic/[id]',params:{tenant:'tenant',id:intent}});
 expect(mocks.resolve).toHaveBeenCalledWith('tenant','run',expect.any(AbortSignal));
});
it('keeps an ordinary V2 run free of semantic review errors',async()=>{
 streaming();mocks.request.mockResolvedValue({...value('2','running'),kind:'v2'});
 mocks.resolve.mockRejectedValue(new APIError(404,'NOT_FOUND'));
 render(<RunScreen/>);await waitFor(()=>expect(mocks.resolve).toHaveBeenCalledTimes(1));
 expect(screen.queryByText('Open batch review')).toBeNull();
 expect(screen.queryByText('Preparing batch review…')).toBeNull();
 expect(screen.queryByText('Unable to read processing status.')).toBeNull();
});
const value=(revision:string,status:string,id='run'):RunStatus=>({id,kind:'bulk',status,revision,stage:status,progress:status==='completed'?1:0,counts:{total:2,succeeded:status==='completed'?2:0,failed:status==='failed'?2:0,skipped:0,review:0},errors:[]});
function streaming(){mocks.stream.mockImplementation((_path,_cursor,_receive,signal)=>new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true})));}
async function clickable(label:string){const target=await screen.findByText(label);await waitFor(()=>{const button=target.closest('[role="button"],button');if(!button||button.getAttribute('aria-disabled')==='true'||button.hasAttribute('disabled'))throw new Error('run action disabled')});return target;}
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.resetAllMocks();mocks.params={id:'run',tenant:'tenant'};journals.retryKey=0;journals.retryScope='tenant:run';journals.cancelScope='tenant:run';journals.retryPending=null;journals.cancelPending=null;});
it('mounts stream refresh and preserves a newer retry response against an older GET',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','failed'));render(<RunScreen/>);
 await screen.findByText('Retry failed');const receive=mocks.stream.mock.calls[0][2];
 let release!:(run:RunStatus)=>void;mocks.request.mockReturnValueOnce(new Promise(resolve=>release=resolve));
 act(()=>receive({cursor:'2',type:'run.failed',payload:{run_id:'run'}}));
 mocks.request.mockResolvedValueOnce(value('2','running'));fireEvent.click(await clickable('Retry failed'));
 await screen.findByText('Cancel');expect(mocks.request.mock.calls[2][1]).toMatchObject({method:'POST',body:{expected_revision:'1'}});
 await act(async()=>release(value('1','failed')));expect(screen.queryByText('Retry failed')).toBeNull();
 mocks.request.mockResolvedValueOnce(value('3','completed'));act(()=>receive({cursor:'3',type:'run.completed',payload:{run_id:'run'}}));
 await screen.findByText('completed · 100%');expect(screen.queryByText('Cancel')).toBeNull();
});
it('sends a full int64 run revision as an exact decimal string on retry',async()=>{
 streaming();const revision='9007199254740993';
 mocks.request.mockResolvedValueOnce(value(revision,'failed')).mockResolvedValueOnce(value('9007199254740994','running'));
 render(<RunScreen/>);fireEvent.click(await clickable('Retry failed'));
 await waitFor(()=>expect(mocks.request).toHaveBeenCalledTimes(2));
 expect(mocks.request.mock.calls[1][1]).toMatchObject({method:'POST',body:{expected_revision:revision}});
});
it('uses a new key after a definite rejected retry',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','failed'))
  .mockRejectedValueOnce(new APIError(409,'RUN_NOT_RETRYABLE'))
  .mockResolvedValueOnce(value('2','running'));
 render(<RunScreen/>);fireEvent.click(await clickable('Retry failed'));
 await screen.findByText('Unable to retry run');
 expect(journals.retryDiscard).toHaveBeenCalledWith('retry-1');
 fireEvent.click(await clickable('Retry failed'));
 await screen.findByText('Cancel');
 expect(mocks.request.mock.calls[1][1]).toMatchObject({idempotencyKey:'retry-1'});
 expect(mocks.request.mock.calls[2][1]).toMatchObject({idempotencyKey:'retry-2'});
});
it('replays a saved retry automatically after reopening the run',async()=>{
 streaming();journals.retryPending={version:1,key:'persisted-retry',expected_revision:'1',created_at:1};
 mocks.request.mockImplementation(async(_path,options)=>options?.method==='POST'?value('2','running'):value('2','running'));
 render(<RunScreen/>);
 await waitFor(()=>expect(mocks.request.mock.calls.some(([,options])=>options?.idempotencyKey==='persisted-retry')).toBe(true));
 await clickable('Cancel');
 expect(mocks.request.mock.calls.find(([,options])=>options?.idempotencyKey==='persisted-retry')?.[1]).toMatchObject({method:'POST',body:{expected_revision:'1'}});
 expect(journals.retryPending).toBeNull();
});
it('keeps an ambiguous saved retry for the next explicit attempt',async()=>{
 streaming();journals.retryPending={version:1,key:'persisted-retry',expected_revision:'1',created_at:1};
 let posts=0;
 mocks.request.mockImplementation(async(_path,options)=>{if(options?.method==='POST'){posts++;if(posts===1)throw new Error('response lost');return value('2','running')}return value('1','failed')});
 render(<RunScreen/>);
 await screen.findByText('Unable to restore run action');
 expect(journals.retryPending?.key).toBe('persisted-retry');
 fireEvent.click(await clickable('Retry failed'));
 await clickable('Cancel');
 expect(mocks.request.mock.calls.filter(([,options])=>options?.idempotencyKey==='persisted-retry')).toHaveLength(2);
 expect(journals.retryPending).toBeNull();
});
it('aborts the old stream and ignores a late mutation after changing tenant/run scope',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));const view=render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 let release!:(run:RunStatus)=>void;mocks.request.mockReturnValueOnce(new Promise(resolve=>release=resolve));fireEvent.click(await clickable('Cancel'));
 await waitFor(()=>expect(mocks.request).toHaveBeenCalledTimes(2));
 const mutationSignal=mocks.request.mock.calls[1][1].signal as AbortSignal;
 mocks.params={id:'next',tenant:'other'};mocks.request.mockResolvedValueOnce(value('1','waiting_review','next'));view.rerender(<RunScreen/>);
 await screen.findByText('waiting_review · 0%');expect(signal.aborted).toBe(true);expect(mutationSignal.aborted).toBe(true);
 await act(async()=>release(value('2','cancelled')));expect(screen.queryByText('cancelled · 0%')).toBeNull();
 expect(journals.cancelPending?.key).toBe('cancel-1');
 expect(mocks.stream.mock.calls[1][0]).toBe('/api/v1/tenants/other/events');
 view.unmount();expect((mocks.stream.mock.calls[1][3] as AbortSignal).aborted).toBe(true);
});

it('clears displayed run metadata and stops monitoring when access is revoked',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const receive=mocks.stream.mock.calls[0][2],signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 mocks.request.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 act(()=>receive({cursor:'4',type:'run.running',payload:{run_id:'run'}}));
 await screen.findByText('Unable to load run');expect(screen.queryByText('Cancel')).toBeNull();expect(screen.queryByText('bulk')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears the run when cancellation reports revoked access',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 mocks.request.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));fireEvent.click(await clickable('Cancel'));
 await screen.findByText('Unable to cancel run');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears displayed metadata on stream access denial before a catalog retry',async()=>{
 let deny!:(error:unknown)=>void;mocks.stream.mockReturnValueOnce(new Promise((_resolve,reject)=>deny=reject));
 mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 await act(async()=>deny(new APIError(403,'FORBIDDEN')));
 await screen.findByText('Unable to load run');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);expect(mocks.request).toHaveBeenCalledOnce();
});
