// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
// This standalone screen fixture explicitly grants current workspace edit authority.
// JSDOM has no Web Locks; the controller's own per-journal execution queue remains exercised.
vi.mock('./semantic-attempt-store',async importOriginal=>({...await importOriginal<typeof import('./semantic-attempt-store')>(),semanticJournalLock:async<T,>(_key:string,action:()=>Promise<T>)=>action()}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>({...await import('react-native-web'),Linking:{openURL:vi.fn().mockResolvedValue(undefined)}}));
import {Linking} from 'react-native';
vi.mock('./localized-router',()=>({router:{push:vi.fn()}}));
import {router} from './localized-router';
import {LocaleProvider} from './locale-context';
import {LocalePicker} from './locale-picker';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{}}}));
import {InterchangeJobScreen} from './interchange-job-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const job=(number=1)=>({id:id(number),revision:'1',document:null,data:{resource_type:'interchange_jobs',id:id(number),revision:'1',tenant_id:id(2),project_id:id(3),workflow_id:id(number),created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:01Z',direction:'import',format:'photoeditor_bundle',mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null}});
const inspection=(number=1,status='running',stage='parsing')=>({schema_version:1,workflow_id:id(number),tenant_id:id(2),project_id:id(3),run_id:id(4),revision:'1',status,stage,ingest_run_id:null,collection_id:null,review:null});
const props=(request:ReturnType<typeof vi.fn>,number=1)=>({client:{request} as never,tenant:id(2),project:id(3),id:id(number)});
async function flush(){await act(async()=>{await Promise.resolve();await Promise.resolve();});}
afterEach(()=>{cleanup();vi.useRealTimers();vi.clearAllMocks();});
it('shows loading then polls until terminal and uses server run identity',async()=>{
 vi.useFakeTimers();let terminal=false;
 const request=vi.fn((path:string,_options?:{signal?:AbortSignal})=>Promise.resolve(path.endsWith('/inspection')?inspection(1,terminal?'completed':'running',terminal?'completed':'parsing'):job()));
 render(<InterchangeJobScreen {...props(request)}/>);expect(screen.getByLabelText('Loading interchange job')).toBeTruthy();
 await flush();expect(screen.getByText('Reading package')).toBeTruthy();expect(screen.getByText('Mapping report is not available yet.')).toBeTruthy();
 fireEvent.click(screen.getByText('Open run'));expect(router.push).toHaveBeenCalledWith({pathname:'/runs/[id]',params:{id:id(4),tenant:id(2)}});
 terminal=true;await act(async()=>{await vi.advanceTimersByTimeAsync(5000);});expect(screen.getByText('Completed')).toBeTruthy();
 const count=request.mock.calls.length;await act(async()=>{await vi.advanceTimersByTimeAsync(15000);});expect(request).toHaveBeenCalledTimes(count);
});
it.each([401,403,404,410])('clears prior metadata and stops polling on unavailable status %s',async(status)=>{
 vi.useFakeTimers();let denied=false;
 const request=vi.fn((path:string,_options?:{signal?:AbortSignal})=>denied?Promise.reject(new APIError(status,'FORBIDDEN')):Promise.resolve(path.endsWith('/inspection')?inspection():job()));
 render(<InterchangeJobScreen {...props(request)}/>);await flush();expect(screen.getByText('Open run')).toBeTruthy();
 denied=true;await act(async()=>{await vi.advanceTimersByTimeAsync(5000);});
 expect(screen.getByText('This job is unavailable.')).toBeTruthy();expect(screen.queryByText('Open run')).toBeNull();
 const count=request.mock.calls.length;await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});expect(request).toHaveBeenCalledTimes(count);
});
it('aborts old scope and ignores late responses after navigation',async()=>{
 let resolveJob!:(value:unknown)=>void,resolveInspection!:(value:unknown)=>void;
 const request=vi.fn((path:string,_options?:{signal?:AbortSignal})=>path.includes(`/interchange-jobs/${id(1)}`)?new Promise(resolve=>{if(path.endsWith('/inspection'))resolveInspection=resolve;else resolveJob=resolve;}):Promise.resolve(path.endsWith('/inspection')?inspection(9,'failed','failed'):job(9)));
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flush();const signal=request.mock.calls[0][1]?.signal as AbortSignal;
 view.rerender(<InterchangeJobScreen {...props(request,9)}/>);await flush();expect(signal.aborted).toBe(true);expect(screen.getByText('Failed')).toBeTruthy();
 await act(async()=>{resolveJob(job());resolveInspection(inspection());});expect(screen.queryByText('Reading package')).toBeNull();expect(screen.getByText('Failed')).toBeTruthy();
});
it('unmount cancels polling and outstanding scope',async()=>{
 vi.useFakeTimers();const request=vi.fn((path:string,_options?:{signal?:AbortSignal})=>Promise.resolve(path.endsWith('/inspection')?inspection():job()));
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flush();const signal=request.mock.calls[0][1]?.signal as AbortSignal;
 view.unmount();expect(signal.aborted).toBe(true);const count=request.mock.calls.length;
 await act(async()=>{await vi.advanceTimersByTimeAsync(20000);});expect(request).toHaveBeenCalledTimes(count);
});
it('allows explicit retry after a transient failure',async()=>{
 const request=vi.fn().mockRejectedValue(new Error('offline'));render(<InterchangeJobScreen {...props(request)}/>);await flush();expect(screen.getByText('Unable to load interchange job.')).toBeTruthy();
 request.mockImplementation((path:string,_options?:{signal?:AbortSignal})=>Promise.resolve(path.endsWith('/inspection')?inspection(1,'cancelled','cancelled'):job()));
 fireEvent.click(screen.getByText('Refresh'));await flush();expect(screen.getByText('Cancelled')).toBeTruthy();expect(screen.queryByText('Unable to load interchange job.')).toBeNull();
});

const reviewInspection=(revision='1')=>({...inspection(1,'waiting_review','review'),revision,review:{report:{artifact_id:id(12),sha256:'a'.repeat(64)},proposed_count:1,entry_count:1,can_close:false,can_decide:true,targets:[{entry_id:id(10),external_id:'photo-1',asset_version_id:id(11),state:'proposed',fields:[{name:'recipe',status:'exact',reason:null}]}]}});
it('saves the selected decision and refreshes the authoritative review',async()=>{
 let saved=false;
 const request=vi.fn((path:string,options?:any)=>{
  if(options?.method==='POST'){saved=true;return Promise.resolve({workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null});}
  if(path.endsWith('/inspection'))return Promise.resolve(saved?{...reviewInspection('2'),review:{...reviewInspection().review,proposed_count:0,targets:[],can_close:true}}:reviewInspection());
  const value=job();if(saved){value.revision='2';value.data.revision='2';}return Promise.resolve(value);
 });
 render(<InterchangeJobScreen {...props(request)}/>);await flush();
 expect(screen.getByText('recipe: exact')).toBeTruthy();
 fireEvent.click(screen.getByRole('checkbox',{name:'Reject photo-1'}));
 fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 const post=request.mock.calls.find(call=>call[1]?.method==='POST');
 expect(post?.[1]).toMatchObject({body:{expected_revision:'1',report_sha:'a'.repeat(64),decisions:[{entry_id:id(10),decision:'reject'}]}});
 expect(screen.getByText('Close review')).toBeTruthy();expect(screen.queryByRole('checkbox')).toBeNull();
});
it('retains the same idempotency key when retrying an uncertain decision',async()=>{
 const request=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.reject(new Error('offline')):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(request)}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));
 fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 expect(screen.getByText('Unable to save decisions. You can retry.')).toBeTruthy();
 fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 const posts=request.mock.calls.filter(call=>call[1]?.method==='POST');
 expect(posts).toHaveLength(2);expect(posts[0][1].idempotencyKey).toBe(posts[1][1].idempotencyKey);
});
it('clears stale choices when polling observes a new review revision',async()=>{
 vi.useFakeTimers();let revision='1';
 const request=vi.fn((path:string)=>{if(path.endsWith('/inspection'))return Promise.resolve(reviewInspection(revision));const value=job();value.revision=revision;value.data.revision=revision;return Promise.resolve(value);});
 render(<InterchangeJobScreen {...props(request)}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));revision='2';
 await act(async()=>{await vi.advanceTimersByTimeAsync(5000);});
 expect(screen.getByText('Review changed. Select decisions again.')).toBeTruthy();
 expect(screen.getByText('Save decisions (0)')).toBeTruthy();
 expect(screen.getByRole('checkbox',{name:'Approve photo-1'}).getAttribute('aria-checked')).not.toBe('true');
});
it('guards double submission and aborts the decision when leaving the scope',async()=>{
 const request=vi.fn((path:string,options?:any)=>options?.method==='POST'?new Promise(()=>{}):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Reject photo-1'}));
 const save=screen.getByText('Save decisions (1)');fireEvent.click(save);fireEvent.click(save);await flush();
 const posts=request.mock.calls.filter(call=>call[1]?.method==='POST');expect(posts).toHaveLength(1);
 view.unmount();expect(posts[0][1].signal.aborted).toBe(true);
});

const exportJob=()=>{const value=job();return {...value,data:{...value.data,direction:'export',format:'xmp',report_artifact_id:id(5),bundle_artifact_id:id(6)}};};
const archiveOutput=()=>({items:[{artifact_id:id(6),role:'bundle',sha256:'a'.repeat(64),bytes:4096,media_type:'application/zip',url:'https://objects.example/archive?signature=opaque&versionId=archive-v1',expires_at:new Date(Date.now()+60000).toISOString()}]});
async function flushDownload(){await act(async()=>{for(let i=0;i<16;i++)await Promise.resolve();});}
it('opens only the archive from a fresh completed export snapshot',async()=>{
 const request=vi.fn((path:string):Promise<unknown>=>Promise.resolve(path.endsWith('/outputs')?archiveOutput():path.endsWith('/inspection')?inspection(1,'completed','completed'):exportJob()));
 render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();
 fireEvent.click(screen.getByText('Download export archive'));await flushDownload();
 expect(Linking.openURL).toHaveBeenCalledTimes(1);expect(Linking.openURL).toHaveBeenCalledWith('https://objects.example/archive?signature=opaque&versionId=archive-v1');
 expect(request.mock.calls.filter(call=>call[0].endsWith('/outputs'))).toHaveLength(1);
 expect(request.mock.calls.filter(call=>call[0].endsWith('/inspection'))).toHaveLength(3);
});
it('does not show download for pending exports or imports',async()=>{
 const request=vi.fn((path:string):Promise<unknown>=>Promise.resolve(path.endsWith('/inspection')?inspection(1,'running','finalizing'):exportJob()));
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();expect(screen.queryByText('Download export archive')).toBeNull();
 view.unmount();request.mockImplementation((path:string)=>Promise.resolve(path.endsWith('/inspection')?inspection(1,'completed','completed'):job()));
 render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();expect(screen.queryByText('Download export archive')).toBeNull();
});
it.each([401,403,404,410])('clears archive controls on unavailable status %s and permits explicit refresh',async(status)=>{
 let denied=true;
 const request=vi.fn((path:string)=>path.endsWith('/outputs')&&denied?Promise.reject(new APIError(status,'FORBIDDEN')):Promise.resolve(path.endsWith('/outputs')?archiveOutput():path.endsWith('/inspection')?inspection(1,'completed','completed'):exportJob()));
 render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();
 expect(screen.getByText('This archive is unavailable.')).toBeTruthy();expect(screen.queryByText('Download export archive')).toBeNull();expect(Linking.openURL).not.toHaveBeenCalled();
 denied=false;fireEvent.click(screen.getByText('Refresh'));await flushDownload();expect(screen.queryByText('This archive is unavailable.')).toBeNull();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();expect(Linking.openURL).toHaveBeenCalledTimes(1);
});
it('does not open stale archive URLs when bundle visibility is revoked during download',async()=>{
 let outputsRead=false;
 const request=vi.fn((path:string)=>{
  if(path.endsWith('/outputs')){outputsRead=true;return Promise.resolve(archiveOutput());}
  if(path.endsWith('/inspection'))return Promise.resolve(inspection(1,'completed','completed'));
  const value=exportJob();return Promise.resolve(outputsRead?{...value,data:{...value.data,report_artifact_id:null,bundle_artifact_id:null}}:value);
 });
 render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();
 expect(Linking.openURL).not.toHaveBeenCalled();expect(screen.queryByText('Download export archive')).toBeNull();expect(screen.getByText('Unable to download archive. Refresh and try again.')).toBeTruthy();
});
it('suppresses duplicate download and aborts the pending request on unmount',async()=>{
 let resolveOutputs!:(value:unknown)=>void,outputSignal:AbortSignal|undefined;
 const request=vi.fn((path:string,options?:{signal?:AbortSignal})=>{
  if(path.endsWith('/outputs')){outputSignal=options?.signal;return new Promise(resolve=>{resolveOutputs=resolve;});}
  return Promise.resolve(path.endsWith('/inspection')?inspection(1,'completed','completed'):exportJob());
 });
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();const button=screen.getByText('Download export archive');fireEvent.click(button);fireEvent.click(button);await flushDownload();
 expect(request.mock.calls.filter(call=>call[0].endsWith('/outputs'))).toHaveLength(1);expect(screen.getByText('Preparing download…')).toBeTruthy();
 view.unmount();expect(outputSignal?.aborted).toBe(true);await act(async()=>resolveOutputs(archiveOutput()));await flushDownload();expect(Linking.openURL).not.toHaveBeenCalled();
});

it('aborts an archive request when navigating to another job and ignores its late result',async()=>{
 let resolveOutputs!:(value:unknown)=>void,signal:AbortSignal|undefined;
 const request=vi.fn((path:string,options?:{signal?:AbortSignal})=>{
  if(path.endsWith('/outputs')){signal=options?.signal;return new Promise(resolve=>{resolveOutputs=resolve;});}
  if(path.includes(id(9)))return Promise.resolve(path.endsWith('/inspection')?inspection(9,'failed','failed'):job(9));
  return Promise.resolve(path.endsWith('/inspection')?inspection(1,'completed','completed'):exportJob());
 });
 const view=render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();
 view.rerender(<InterchangeJobScreen {...props(request,9)}/>);await flushDownload();expect(signal?.aborted).toBe(true);expect(screen.getByText('Failed')).toBeTruthy();
 await act(async()=>resolveOutputs(archiveOutput()));await flushDownload();expect(Linking.openURL).not.toHaveBeenCalled();expect(screen.queryByText('Download export archive')).toBeNull();expect(screen.getByText('Failed')).toBeTruthy();
});
it('reports an external opener failure and releases the download lock for retry',async()=>{
 vi.mocked(Linking.openURL).mockRejectedValueOnce(new Error('opener unavailable'));
 const request=vi.fn((path:string):Promise<unknown>=>Promise.resolve(path.endsWith('/outputs')?archiveOutput():path.endsWith('/inspection')?inspection(1,'completed','completed'):exportJob()));
 render(<InterchangeJobScreen {...props(request)}/>);await flushDownload();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();
 expect(screen.getByText('Unable to download archive. Refresh and try again.')).toBeTruthy();
 fireEvent.click(screen.getByText('Refresh'));await flushDownload();fireEvent.click(screen.getByText('Download export archive'));await flushDownload();expect(Linking.openURL).toHaveBeenCalledTimes(2);
});


it('replacing API client cancels old mutation and resets pending review choices',async()=>{
 let finish!:(value:unknown)=>void;
 const oldRequest=vi.fn((path:string,options?:any)=>options?.method==='POST'?
  new Promise(resolve=>{finish=resolve;}):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 const nextRequest=vi.fn((path:string,_options?:any)=>Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 const view=render(<InterchangeJobScreen {...props(oldRequest)}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));
 fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 const post=oldRequest.mock.calls.find(call=>call[1]?.method==='POST')!;
 view.rerender(<InterchangeJobScreen {...props(nextRequest)}/>);await flush();
 expect(post[1].signal.aborted).toBe(true);
 expect(screen.getByText('Save decisions (0)')).toBeTruthy();
 expect(screen.getByRole('checkbox',{name:'Approve photo-1'}).getAttribute('aria-checked')).not.toBe('true');
 const before=nextRequest.mock.calls.length;
 await act(async()=>finish({workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null}));
 await flush();
 expect(nextRequest).toHaveBeenCalledTimes(before);
 expect(screen.getByText('Save decisions (0)')).toBeTruthy();
 expect(nextRequest.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
});


it('restores uncertain decisions after remount and reuses exact key and body',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'durable-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const firstRequest=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.reject(new Error('lost')):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 const first=render(<InterchangeJobScreen {...props(firstRequest)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Reject photo-1'}));fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 expect(screen.getByText('Retry saved review action')).toBeTruthy();
 const oldPost=firstRequest.mock.calls.find(call=>call[1]?.method==='POST')!;first.unmount();
 const secondRequest=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.resolve({workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null}):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(secondRequest)} journal={journal}/>);await flush();
 expect(secondRequest.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 const replay=secondRequest.mock.calls.find(call=>call[1]?.method==='POST')!;
 expect(replay[1].idempotencyKey).toBe(oldPost[1].idempotencyKey);expect(replay[1].body).toEqual(oldPost[1].body);
 expect(await journal.read()).toBeNull();
});

it('acknowledged recovery only clears journal and never repeats POST',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'acknowledged-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await journal.write({version:1,kind:'decide',key:'accepted-original',inspection:reviewInspection() as any,decisions:[{entry_id:id(10),decision:'approve'}],receipt:{workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null}});
 const request=vi.fn((path:string,_options?:any)=>Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(await journal.read()).toBeNull();expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
});


it('preserves unresolved close request across remount and retries exact CAS/key',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'close-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const closable={...reviewInspection(),review:{...reviewInspection().review,targets:[],proposed_count:0,can_close:true}};
 const firstRequest=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.reject(new Error('lost')):Promise.resolve(path.endsWith('/inspection')?closable:job()));
 const first=render(<InterchangeJobScreen {...props(firstRequest)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByText('Close review'));await flush();
 const oldPost=firstRequest.mock.calls.find(call=>call[1]?.method==='POST')!;first.unmount();
 const request=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.resolve({workflow_id:id(1),run_id:id(4),revision:'2',state:'completed',outputs:[],error_code:null}):Promise.resolve(path.endsWith('/inspection')?closable:job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 const replay=request.mock.calls.find(call=>call[1]?.method==='POST')!;
 expect(replay[0]).toBe(oldPost[0]);expect(replay[1].idempotencyKey).toBe(oldPost[1].idempotencyKey);expect(replay[1].body).toEqual(oldPost[1].body);
 expect(await journal.read()).toBeNull();
});

it('blocks recovered mutation until native access read completes and after revocation',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'revoked-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await journal.write({version:1,kind:'decide',key:'original-key',inspection:reviewInspection() as any,decisions:[{entry_id:id(10),decision:'reject'}],receipt:null});
 let finish!:(value:unknown)=>void;
 const request=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.reject(new APIError(403,'FORBIDDEN')):path.endsWith('/inspection')?new Promise(resolve=>{finish=resolve;}):Promise.resolve(job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
 await act(async()=>finish(reviewInspection()));await flush();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(screen.getByText('This review is unavailable.')).toBeTruthy();expect(screen.queryByText('photo-1')).toBeNull();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(request.mock.calls.filter(call=>call[1]?.method==='POST')).toHaveLength(1);
 expect((await journal.read())?.key).toBe('original-key');
});


it('storage failure prevents decision POST before durable admission',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const journal=new InterchangeReviewAttemptStore({getItem:async()=>null,setItem:async()=>{throw new Error('disk')},removeItem:async()=>{}},'storage-failed',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const request=vi.fn((path:string,_options?:any)=>Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 expect(screen.getByText('Retry saved review action')).toBeTruthy();
 expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
});

it('refresh adopts another tab receipt and only cleans the acknowledged journal',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'other-tab-receipt',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const pending={version:1 as const,kind:'decide' as const,key:'original-key',inspection:reviewInspection() as any,decisions:[{entry_id:id(10),decision:'approve' as const}],receipt:null};
 await journal.write(pending);
 const request=vi.fn((path:string,_options?:any)=>Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 const another=new InterchangeReviewAttemptStore(storage,'other-tab-receipt',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 await another.write({...pending,receipt:{workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null}});
 fireEvent.click(screen.getByText('Refresh'));await flush();
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
 expect(await journal.read()).toBeNull();
});

it('cleanup failure retains acknowledged receipt and retry does not POST again',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>();let removable=false;
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{if(!removable)throw new Error('disk');values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'cleanup-receipt',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const request=vi.fn((path:string,options?:any)=>options?.method==='POST'?Promise.resolve({workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null}):Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));fireEvent.click(screen.getByText('Save decisions (1)'));await flush();
 expect((await journal.read())?.receipt).not.toBeNull();removable=true;
 fireEvent.click(screen.getByText('Retry saved review action'));await flush();
 expect(request.mock.calls.filter(call=>call[1]?.method==='POST')).toHaveLength(1);
 expect(await journal.read()).toBeNull();
});


it('switches recovery language without replacing durable intent or submitting it',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'locale-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 const pending={version:1 as const,kind:'decide' as const,key:'original-key',inspection:reviewInspection() as any,decisions:[{entry_id:id(10),decision:'reject' as const}],receipt:null};await journal.write(pending);
 const request=vi.fn((path:string,_options?:any)=>Promise.resolve(path.endsWith('/inspection')?reviewInspection():job()));
 render(<LocaleProvider><LocalePicker/><InterchangeJobScreen {...props(request)} journal={journal}/></LocaleProvider>);await flush();
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));
 expect(screen.getByRole('button',{name:ru.interchange_recovery_retry})).toBeTruthy();
 fireEvent.click(screen.getByRole('radio',{name:'Български'}));
 expect(screen.getByRole('button',{name:bg.interchange_recovery_retry})).toBeTruthy();
 expect((await journal.read())?.key).toBe('original-key');expect(request.mock.calls.every(call=>call[1]?.method!=='POST')).toBe(true);
});


it('switches a displayed job error language without another request',async()=>{
 const request=vi.fn().mockRejectedValue(new Error('offline'));
 render(<LocaleProvider><LocalePicker/><InterchangeJobScreen {...props(request)}/></LocaleProvider>);await flush();
 const calls=request.mock.calls.length;
 fireEvent.click(screen.getByRole('radio',{name:'Русский'}));
 expect(screen.getByRole('alert').textContent).toBe(ru.interchange_error_job_load);
 fireEvent.click(screen.getByRole('radio',{name:'Български'}));
 expect(screen.getByRole('alert').textContent).toBe(bg.interchange_error_job_load);
 expect(request).toHaveBeenCalledTimes(calls);
});


it('stops a durable review request then retries the same selection and key',async()=>{
 const {InterchangeReviewAttemptStore}=await import('./interchange-review-attempt-store');
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const journal=new InterchangeReviewAttemptStore(storage,'stop-review',id(20),id(2),id(3),id(1),async(_origin,action)=>action());
 let firstSignal:AbortSignal|undefined,posts=0;
 const request=vi.fn((path:string,options?:any)=>{
  if(options?.method==='POST'){
   posts++;
   if(posts===1){firstSignal=options.signal;return new Promise((_resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true}));}
   return Promise.resolve({workflow_id:id(1),revision:'2',remaining:0,batch_id:id(15),child:null});
  }
  return Promise.resolve(path.endsWith('/inspection')?reviewInspection():job());
 });
 render(<InterchangeJobScreen {...props(request)} journal={journal}/>);await flush();
 fireEvent.click(screen.getByRole('checkbox',{name:'Approve photo-1'}));fireEvent.click(screen.getByText('Save decisions (1)'));
 await waitFor(()=>expect(firstSignal).toBeDefined());const pending=await journal.read();
 fireEvent.click(screen.getByText('Stop waiting'));
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Retry saved review action'})));
 expect(firstSignal!.aborted).toBe(true);expect((await journal.read())?.key).toBe(pending?.key);
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry saved review action'}));
 await waitFor(()=>expect(posts).toBe(2));await waitFor(async()=>expect(await journal.read()).toBeNull());
 const sent=request.mock.calls.filter(call=>call[1]?.method==='POST');
 expect(sent[1][1].idempotencyKey).toBe(sent[0][1].idempotencyKey);expect(sent[1][1].body).toEqual(sent[0][1].body);
});
