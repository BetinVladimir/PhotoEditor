import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {APIError} from '@photoeditor/api-client';
// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render as renderUI,screen,waitFor} from '@testing-library/react';
import type {RunStatus} from './catalog-types';
import type {ReactNode} from 'react';
import {WorkspaceAccessContext} from './workspace-access-context';
const workspaceAccess={tenant:'tenant',actor:'00000000-0000-4000-8000-000000000001',role:'editor' as const,canEdit:true,canAdmin:false};
function render(ui:ReactNode){return renderUI(ui,{wrapper:({children})=><WorkspaceAccessContext.Provider value={{...workspaceAccess,tenant:mocks.params.tenant}}>{children}</WorkspaceAccessContext.Provider>});}

const mocks=vi.hoisted(()=>({params:{id:'run',tenant:'tenant'} as {id:string;tenant:string;semanticWorkflow?:string},request:vi.fn(),stream:vi.fn(),push:vi.fn(),resolve:vi.fn()}));
const retained=vi.hoisted(()=>({stops:[] as Array<()=>void>}));
const foreground=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>foreground}));
const journals=vi.hoisted(()=>({retryKey:0,retryScope:'tenant:run',cancelScope:'tenant:run',retryPending:null as null|{version:1;key:string;expected_revision:string;created_at:number},cancelPending:null as null|{version:1;key:string;created_at:number},retryDiscard:vi.fn(),cancelDiscard:vi.fn()}));
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native};});
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
vi.mock('./run-retry-attempt-store',()=>({RunRetryAttemptStore:class{private scope:string;constructor(_storage:unknown,_origin:string,_user:string,tenant:string,run:string){this.scope=tenant+':'+run}async read(){return journals.retryScope===this.scope?journals.retryPending:null}async begin(expected_revision:string){if(journals.retryScope===this.scope&&journals.retryPending)return journals.retryPending;journals.retryKey++;journals.retryScope=this.scope;journals.retryPending={version:1,key:`retry-${journals.retryKey}`,expected_revision,created_at:1};return journals.retryPending}async acknowledge(key:string){if(journals.retryScope===this.scope&&journals.retryPending?.key===key)journals.retryPending=null}async discard(key:string){journals.retryDiscard(key);if(journals.retryScope===this.scope&&journals.retryPending?.key===key)journals.retryPending=null}}}));
vi.mock('./run-cancel-attempt-store',()=>({RunCancelAttemptStore:class{private scope:string;constructor(_storage:unknown,_origin:string,_user:string,tenant:string,run:string){this.scope=tenant+':'+run}async read(){return journals.cancelScope===this.scope?journals.cancelPending:null}async begin(){if(journals.cancelScope===this.scope&&journals.cancelPending)return journals.cancelPending;journals.cancelScope=this.scope;journals.cancelPending={version:1,key:'cancel-1',created_at:1};return journals.cancelPending}async acknowledge(key:string){if(journals.cancelScope===this.scope&&journals.cancelPending?.key===key)journals.cancelPending=null}async discard(key:string){journals.cancelDiscard(key);if(journals.cancelScope===this.scope&&journals.cancelPending?.key===key)journals.cancelPending=null}}}));
const client={request:mocks.request,eventStream:mocks.stream,semanticRunIntent:mocks.resolve};const auth={origin:'https://app.test',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
import {OwnerAPI} from './owner-api';
import RunScreen from '../screens/runs/[id]';
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
afterEach(()=>{cleanup();retained.stops=[];workspaceAccess.canEdit=true;vi.useRealTimers();foreground.active=true;foreground.generation=0;vi.restoreAllMocks();vi.resetAllMocks();mocks.params={id:'run',tenant:'tenant'};journals.retryKey=0;journals.retryScope='tenant:run';journals.cancelScope='tenant:run';journals.retryPending=null;journals.cancelPending=null;});
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
 await screen.findByText('Unable to Retry run');
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
 await screen.findByText('Unable to load this page. Retry when the connection is available.');expect(screen.queryByText('Cancel')).toBeNull();expect(screen.queryByText('bulk')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears the run when cancellation reports revoked access',async()=>{
 streaming();mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 mocks.request.mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));fireEvent.click(await clickable('Cancel'));
 await screen.findByText('Unable to load this page. Retry when the connection is available.');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);
});

it('clears displayed metadata on stream access denial before a catalog retry',async()=>{
 let deny!:(error:unknown)=>void;mocks.stream.mockReturnValueOnce(new Promise((_resolve,reject)=>deny=reject));
 mocks.request.mockResolvedValueOnce(value('1','running'));render(<RunScreen/>);
 await screen.findByText('Cancel');const signal=mocks.stream.mock.calls[0][3] as AbortSignal;
 await act(async()=>deny(new APIError(403,'FORBIDDEN')));
 await screen.findByText('Unable to load this page. Retry when the connection is available.');expect(screen.queryByText('bulk')).toBeNull();expect(screen.queryByText('Cancel')).toBeNull();expect(signal.aborted).toBe(true);expect(mocks.request).toHaveBeenCalledOnce();
});

it('does no run or output reads while initially backgrounded',()=>{
 foreground.active=false;render(<RunScreen/>);expect(mocks.request).not.toHaveBeenCalled();expect(mocks.stream).not.toHaveBeenCalled();
});

it('opens an Apply review from the processing screen',async()=>{
 const run='22222222-2222-4222-8222-222222222222',review='33333333-3333-4333-8333-333333333333';
 mocks.params={id:run,tenant:'tenant'};streaming();
 mocks.request.mockResolvedValue({...value('2','waiting_review',run),kind:'apply',counts:{total:1,succeeded:0,failed:0,skipped:0,review:1}});
 vi.spyOn(OwnerAPI.prototype,'reviewsForRun').mockResolvedValue({items:[{id:review,run_id:run,kind:'composition',state:'pending'}] as never[],next_cursor:null});
 render(<RunScreen/>);fireEvent.click(await screen.findByText('composition · Pending'));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/reviews/[id]',params:{tenant:'tenant',id:review}});
 expect(mocks.resolve).not.toHaveBeenCalled();
});

it.each([401,403])('revokes Apply run controls when review access fails with %s',async(status)=>{
 const run='22222222-2222-4222-8222-222222222222';
 mocks.params={id:run,tenant:'tenant'};streaming();
 mocks.request.mockResolvedValue({...value('2','waiting_review',run),kind:'apply',output_collection_id:'33333333-3333-4333-8333-333333333333',counts:{total:1,succeeded:0,failed:0,skipped:0,review:1}});
 vi.spyOn(OwnerAPI.prototype,'reviewsForRun').mockRejectedValue(new APIError(status,'FORBIDDEN'));
 render(<RunScreen/>);
 await screen.findByText('Unable to load this page. Retry when the connection is available.');
 expect(screen.queryByText('Cancel')).toBeNull();
 expect(screen.queryByText('Open output collection')).toBeNull();
 expect(mocks.push).not.toHaveBeenCalled();
 await waitFor(()=>expect(mocks.stream.mock.calls[0][3].aborted).toBe(true));
});
it('offers an explicit initial read retry after access refusal without issuing a new mutation',async()=>{
 streaming();mocks.request.mockRejectedValueOnce(new APIError(410,'GONE')).mockResolvedValueOnce(value('1','running'));
 render(<RunScreen/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Retry loading'}));
 await screen.findByText('running · 0%');expect(mocks.request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
});
it('stops initial loading and refuses a noncooperative late run response',async()=>{
 streaming();let resolve!:(run:RunStatus)=>void;mocks.request.mockReturnValueOnce(new Promise(done=>{resolve=done;}));render(<RunScreen/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Stop loading'}));await act(async()=>{resolve(value('1','running'));});
 expect(screen.queryByText('running · 0%')).toBeNull();expect(screen.getByRole('button',{name:'Retry loading'})).toBeTruthy();
});


it('releases the initial action gate when saved-action identity lookup ignores abort',async()=>{
 streaming();mocks.request.mockResolvedValue(value('1','failed'));let release!:(value:any)=>void;
 vi.spyOn(OwnerAPI.prototype,'me').mockReturnValue(new Promise(resolve=>release=resolve));
 vi.useFakeTimers();render(<RunScreen/>);
 await act(async()=>{for(let n=0;n<30;n++)await Promise.resolve();});
 expect(screen.getByText('Retry failed').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 await act(async()=>{await vi.advanceTimersByTimeAsync(10001);});
 expect(screen.getByRole('alert').textContent).toContain('Unable to check saved run actions');
 expectEnabled(screen.getByText('Retry failed'));
 await act(async()=>release({id:'00000000-0000-0000-0000-000000000001'}));
 expect(mocks.request.mock.calls.filter(call=>call[1]?.method==='POST')).toHaveLength(0);
});

it('retains a saved action after recovery timeout and ignores the late replay response',async()=>{
 streaming();journals.retryPending={version:1,key:'persisted-retry',expected_revision:'1',created_at:1};
 let release!:(value:RunStatus)=>void;
 mocks.request.mockImplementation((_path,options)=>options?.method==='POST'?new Promise(resolve=>release=resolve):Promise.resolve(value('1','failed')));
 vi.useFakeTimers();render(<RunScreen/>);
 await act(async()=>{for(let n=0;n<40;n++)await Promise.resolve();});
 const post=mocks.request.mock.calls.find(call=>call[1]?.method==='POST');expect(post).toBeDefined();
 await act(async()=>{await vi.advanceTimersByTimeAsync(10001);});
 expect(post![1].signal.aborted).toBe(true);expect(journals.retryPending?.key).toBe('persisted-retry');
 expect(screen.getByRole('alert').textContent).toContain('Unable to restore run action');
 await act(async()=>release(value('2','running')));
 expect(journals.retryPending?.key).toBe('persisted-retry');expect(screen.getByText('Retry failed')).toBeDefined();
});

it('cancels retry confirmation, preserves the key and suppresses its late result',async()=>{
 streaming();let release!:(value:RunStatus)=>void;let posts=0;
 mocks.request.mockImplementation((_path,options)=>options?.method==='POST'?(++posts===1?new Promise(resolve=>release=resolve):Promise.resolve(value('2','running'))):Promise.resolve(value('1','failed')));
 render(<RunScreen/>);fireEvent.click(await clickable('Retry failed'));
 await waitFor(()=>expect(posts).toBe(1));
 const first=mocks.request.mock.calls.find(call=>call[1]?.method==='POST')!;
 fireEvent.click(screen.getByRole('button',{name:'Cancel action confirmation'}));
 await screen.findByText('Unable to Retry run');expect(first[1].signal.aborted).toBe(true);
 expect(journals.retryPending?.key).toBe('retry-1');
 fireEvent.click(await clickable('Retry failed'));await screen.findByText('Cancel');
 const requests=mocks.request.mock.calls.filter(call=>call[1]?.method==='POST');
 expect(requests).toHaveLength(2);expect(requests[1][1].idempotencyKey).toBe(requests[0][1].idempotencyKey);
 expect(requests[1][1].body).toEqual(requests[0][1].body);expect(journals.retryPending).toBeNull();
 await act(async()=>release(value('3','failed')));
 expect(screen.queryByText('Retry failed')).toBeNull();expect(screen.getByText('Cancel')).toBeDefined();
});

it('times out cancel confirmation while preserving its durable request',async()=>{
 streaming();let release!:(value:RunStatus)=>void;
 mocks.request.mockImplementation((_path,options)=>options?.method==='POST'?new Promise(resolve=>release=resolve):Promise.resolve(value('1','running')));
 render(<RunScreen/>);await clickable('Cancel');vi.useFakeTimers();fireEvent.click(screen.getByText('Cancel'));
 await act(async()=>{for(let n=0;n<30;n++)await Promise.resolve();});
 const post=mocks.request.mock.calls.find(call=>call[1]?.method==='POST');expect(post).toBeDefined();
 await act(async()=>{await vi.advanceTimersByTimeAsync(15001);});
 expect(post![1].signal.aborted).toBe(true);expect(journals.cancelPending?.key).toBe('cancel-1');
 expect(screen.queryByRole('button',{name:'Cancel action confirmation'})).toBeNull();
 await act(async()=>release(value('2','cancelled')));
 expect(journals.cancelPending?.key).toBe('cancel-1');expect(screen.getByText('Cancel')).toBeDefined();
});

it('does not acknowledge a retry receipt for another run',async()=>{
 streaming();mocks.request.mockImplementation((_path,options)=>Promise.resolve(options?.method==='POST'?value('2','running','other-run'):value('1','failed')));
 render(<RunScreen/>);fireEvent.click(await clickable('Retry failed'));
 await screen.findByText('Unable to Retry run');
 expect(journals.retryPending?.key).toBe('retry-1');expect(journals.retryDiscard).not.toHaveBeenCalled();
 expect(screen.getByText('Retry failed')).toBeDefined();
});

it('retains a recovered action if its replay response has a malformed revision',async()=>{
 streaming();journals.retryPending={version:1,key:'saved-key',expected_revision:'1',created_at:1};
 mocks.request.mockImplementation((_path,options)=>Promise.resolve(options?.method==='POST'?{...value('2','running'),revision:2}:value('1','failed')));
 render(<RunScreen/>);await screen.findByText('Unable to restore run action');
 expect(journals.retryPending?.key).toBe('saved-key');expect(journals.retryDiscard).not.toHaveBeenCalled();
});


it('keeps a readonly run visible without replaying saved mutations or allowing cancellation',async()=>{
 workspaceAccess.canEdit=false;
 journals.retryPending={version:1,key:'saved-retry',expected_revision:'1',created_at:1};
 streaming();mocks.request.mockResolvedValue(value('2','running'));
 render(<RunScreen/>);
 const cancel=await screen.findByRole('button',{name:'Cancel'});
 expect(cancel.getAttribute('aria-disabled')).toBe('true');
 fireEvent.click(cancel);
 await act(async()=>{await Promise.resolve();});
 expect(mocks.request.mock.calls.some(call=>call[1]?.method==='POST')).toBe(false);
 expect(journals.retryPending?.key).toBe('saved-retry');
});


it('a retained confirmation Stop cannot abort the next exact-key replay',async()=>{
 streaming();const replies:Array<(run:RunStatus)=>void>=[];
 mocks.request.mockImplementation((_path,options)=>options?.method==='POST'?new Promise(resolve=>replies.push(resolve)):Promise.resolve(value('1','failed')));
 render(<RunScreen/>);fireEvent.click(await clickable('Retry failed'));
 await waitFor(()=>expect(replies).toHaveLength(1));
 const oldStop=retained.stops.at(-1)!;expect(oldStop).toBeTypeOf('function');
 await act(async()=>{oldStop();});await screen.findByText('Unable to Retry run');
 fireEvent.click(await clickable('Retry failed'));await waitFor(()=>expect(replies).toHaveLength(2));
 const posts=mocks.request.mock.calls.filter(call=>call[1]?.method==='POST');
 expect(posts[1][1].idempotencyKey).toBe(posts[0][1].idempotencyKey);
 expect(posts[1][1].body).toEqual(posts[0][1].body);
 await act(async()=>{oldStop();});expect(posts[1][1].signal.aborted).toBe(false);
 await act(async()=>{replies[0](value('9','failed'));});
 expect(posts[1][1].signal.aborted).toBe(false);
 await act(async()=>{replies[1](value('2','running'));});await screen.findByText('Cancel');
 expect(journals.retryPending).toBeNull();
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);if(label==='Cancel action confirmation')retained.stops.push(props.onPress);};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
