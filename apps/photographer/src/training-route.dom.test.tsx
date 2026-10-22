// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({data:new Map<string,string>(),request:vi.fn(),post:vi.fn(),setRead:vi.fn(),replace:vi.fn(),push:vi.fn()}));
const foreground=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>foreground}));
vi.mock('react-native',async()=>import('react-native-web'));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:id(3),tenant:id(2)}),router:{replace:mocks.replace,push:mocks.push}}));
const client={request:mocks.request},auth={origin:'https://training-route.example',client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(key:string)=>mocks.data.get(key)??null,setItem:async(key:string,value:string)=>{mocks.data.set(key,value);},removeItem:async(key:string)=>{mocks.data.delete(key);},getAllKeys:async()=>[...mocks.data.keys()]}}));
import Training from '../screens/styles/[id]/training';
function prepare(){mocks.setRead.mockResolvedValue({id:id(4),style_id:id(3),revision:'1',state:'draft',pairs:{items:[],revision:'1'}});mocks.request.mockImplementation(async(path:string,options?:any)=>{
 if(options?.method==='POST')return mocks.post(path,options);
 if(path==='/api/v1/me')return{id:id(1),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};
 if(path.includes('/training-sets/'))return mocks.setRead(path,options);
 if(path.includes('/styles/'))return{id:id(3),tenant_id:id(2),project_id:id(5),name:'Style',description:'',archived:false,revision:'1',created_at:'2026-10-17T00:00:00Z'};
 if(path.includes('/projects/'))return{id:id(5),tenant_id:id(2),can_edit:true};
 throw new Error('unexpected read '+path);
});}
afterEach(()=>{cleanup();mocks.data.clear();foreground.active=true;foreground.generation=0;vi.resetAllMocks();});
it('retries initial readonly failure without creating a training set',async()=>{
 prepare();mocks.request.mockRejectedValueOnce(new Error('offline'));render(<Training/>);fireEvent.click(await screen.findByText('read_retry'));await screen.findByText('Create training set');expect(mocks.post).not.toHaveBeenCalled();
});
it('recovers an uncertain creation on the same mounted screen with exactly the saved command/key',async()=>{
 prepare();mocks.post.mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce({id:id(4)});render(<Training/>);fireEvent.click(await screen.findByText('Create training set'));await screen.findByRole('alert');
 const first=mocks.post.mock.calls[0];fireEvent.click(screen.getByText('training_recover'));await screen.findByText('0 pairs · revision 1');
 expect(mocks.post).toHaveBeenCalledTimes(2);expect(mocks.post.mock.calls[1][0]).toBe(first[0]);expect(mocks.post.mock.calls[1][1].idempotencyKey).toBe(first[1].idempotencyKey);expect(mocks.post.mock.calls[1][1].body).toEqual(first[1].body);
});
it('recovers an accepted creation with GET only after a failed materialization read',async()=>{
 prepare();mocks.post.mockResolvedValue({id:id(4)});mocks.setRead.mockRejectedValueOnce(new Error('read lost')).mockResolvedValueOnce({id:id(4),style_id:id(3),revision:'1',state:'draft',pairs:{items:[],revision:'1'}});
 render(<Training/>);fireEvent.click(await screen.findByText('Create training set'));await screen.findByRole('alert');fireEvent.click(screen.getByText('training_recover'));await screen.findByText('0 pairs · revision 1');expect(mocks.post).toHaveBeenCalledOnce();
});
it('does not navigate or open a set from a late mutation response after cancellation',async()=>{
 prepare();let finish!:(value:unknown)=>void;mocks.post.mockImplementation(()=>new Promise(resolve=>{finish=resolve;}));render(<Training/>);fireEvent.click(await screen.findByText('Create training set'));await waitFor(()=>expect(mocks.post).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('read_cancel'));await screen.findByText('training_recover');await act(async()=>finish({id:id(4)}));expect(screen.queryByText('0 pairs · revision 1')).toBeNull();expect(mocks.replace).not.toHaveBeenCalled();expect(mocks.setRead).not.toHaveBeenCalled();
});

it('keeps pair draft after cancellation and retries the frozen pair without adopting a late receipt',async()=>{
 prepare();mocks.post.mockResolvedValueOnce({id:id(4)});
 render(<Training/>);fireEvent.click(await screen.findByText('Create training set'));await screen.findByText('0 pairs · revision 1');
 fireEvent.change(screen.getByLabelText('Before asset version UUID'),{target:{value:id(6)}});
 fireEvent.change(screen.getByLabelText('After asset version UUID'),{target:{value:id(7)}});
 fireEvent.change(screen.getByLabelText('Session ID'),{target:{value:'client session'}});
 let finish!:(value:unknown)=>void;
 mocks.post.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockRejectedValueOnce(new Error('still uncertain'));
 fireEvent.click(screen.getByText('Add confirmed pair'));await waitFor(()=>expect(finish).toBeDefined());
 const frozen=mocks.post.mock.calls[1];
 fireEvent.click(screen.getByText('read_cancel'));await screen.findByText('training_recover');
 expect((screen.getByLabelText('Before asset version UUID') as HTMLInputElement).value).toBe(id(6));
 expect((screen.getByLabelText('After asset version UUID') as HTMLInputElement).value).toBe(id(7));
 expect((screen.getByLabelText('Session ID') as HTMLInputElement).value).toBe('client session');
 await act(async()=>{finish({id:id(8)});});
 expect(mocks.setRead).toHaveBeenCalledTimes(2);
 mocks.setRead.mockResolvedValue({id:id(4),style_id:id(3),revision:'2',state:'draft',pairs:{revision:'2',items:[{id:id(8),training_set_id:id(4),before_asset_version_id:id(6),after_asset_version_id:id(7),session_id:'client session',confirmed:true,included:true,validation_status:'valid',revision:'1',created_at:'2026-10-17T00:00:00Z'}]}});
 fireEvent.click(screen.getByText('training_recover'));await screen.findByText('1 pairs · revision 2');
 expect(mocks.post).toHaveBeenCalledTimes(2);expect(mocks.post.mock.calls[1]).toEqual(frozen);
});
function includedSet(){return{id:id(4),style_id:id(3),revision:'1',state:'draft',pairs:{revision:'1',items:[{id:id(6),training_set_id:id(4),before_asset_version_id:id(7),after_asset_version_id:id(8),session_id:'session',confirmed:true,included:true,validation_status:'valid',revision:'1',created_at:'2026-10-17T00:00:00Z'}]}};}
async function existingTrainingSet(){const{TrainingAttemptStore}=await import('./training-attempt-store'),storage=(await import('@react-native-async-storage/async-storage')).default;const store=new TrainingAttemptStore(storage,auth.origin,id(1),id(2),id(3));const state=await store.begin({kind:'create',body:{}});await store.receipt(state.pending!.key,id(4));await store.finish(state.pending!.key);return store;}
it('does not freeze a new analysis when the actual combined runtime is unavailable',async()=>{
 prepare();mocks.setRead.mockResolvedValue(includedSet());await existingTrainingSet();const base=mocks.request.getMockImplementation()!;mocks.request.mockImplementation(async(path:string,options?:any)=>path.includes('/analysis-capabilities')?{schema_version:1,tenant_id:id(2),project_id:id(5),style_id:id(3),geometry_enabled:true,available:false,reason:'STYLE_AGGREGATE_CAPABILITY_UNAVAILABLE',backends:[],expires_at:new Date(Date.now()+15000).toISOString()}:base(path,options));
 render(<Training/>);fireEvent.click(await screen.findByText('Analyze style'));await screen.findByRole('alert');expect(mocks.post).not.toHaveBeenCalled();expect(screen.queryByText('training_recover')).toBeNull();
});
it('freezes supported crop-aware analysis and retries an uncertain receipt without a live capability read',async()=>{
 prepare();mocks.setRead.mockResolvedValue(includedSet());await existingTrainingSet();const base=mocks.request.getMockImplementation()!;let reads=0;mocks.request.mockImplementation(async(path:string,options?:any)=>{if(path.includes('/analysis-capabilities')){reads++;return{schema_version:1,tenant_id:id(2),project_id:id(5),style_id:id(3),geometry_enabled:true,available:true,reason:null,backends:['mps'],expires_at:new Date(Date.now()+15000).toISOString()};}return base(path,options);});mocks.post.mockRejectedValueOnce(new Error('reply lost')).mockResolvedValueOnce({run_id:id(9)});
 render(<Training/>);fireEvent.click(await screen.findByText('Analyze style'));await screen.findByRole('alert');const first=mocks.post.mock.calls[0][1];expect(first.body.analysis_options.alignment_mode).toBe('crop_aware');fireEvent.click(screen.getByText('training_recover'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalled());expect(reads).toBe(1);expect(mocks.post.mock.calls[1][1].idempotencyKey).toBe(first.idempotencyKey);expect(mocks.post.mock.calls[1][1].body).toEqual(first.body);
});
it('bounds a stalled completed-set reset and keeps the existing run recoverable',async()=>{
 prepare();mocks.setRead.mockResolvedValue(includedSet());const store=await existingTrainingSet(),pending=await store.begin({kind:'analyze',set_id:id(4),body:{training_set_id:id(4),training_set_revision:1,analysis_options:{alignment_mode:'crop_aware',geometry_enabled:true,seed:0}}});await store.receipt(pending.pending!.key,id(9));await store.finish(pending.pending!.key);
 const {TrainingAttemptStore}=await import('./training-attempt-store'),reset=vi.spyOn(TrainingAttemptStore.prototype,'reset').mockImplementation(()=>new Promise(()=>{}));
 try{render(<Training/>);const action=await screen.findByText('training_new_set');vi.useFakeTimers();fireEvent.click(action);await act(async()=>{await vi.advanceTimersByTimeAsync(8001)});expect(screen.getByRole('alert')).toBeTruthy();expect(screen.getByText('training_open_run')).toBeTruthy();expect(mocks.post).not.toHaveBeenCalled();}finally{vi.useRealTimers();reset.mockRestore();}
});
