import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {APIError} from '@photoeditor/api-client';
// @vitest-environment jsdom
import {afterEach,beforeEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{id:'00000000-0000-4000-8000-000000000004',tenant:'00000000-0000-4000-8000-000000000002'},replace:vi.fn(),values:new Map<string,string>(),failWrite:false,removeWait:null as null|(()=>Promise<void>),api:{collectionApplyCapabilities:vi.fn(),projectAccess:vi.fn(),run:vi.fn(),collection:vi.fn(),me:vi.fn(),styles:vi.fn(),styleVersions:vi.fn(),apply:vi.fn()},auth:{origin:'https://studio.test',client:()=>({})}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{replace:mocks.replace},useLocalSearchParams:()=>mocks.params}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {constructor(){return mocks.api}}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.values.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failWrite)throw new Error('disk unavailable');mocks.values.set(k,v)},removeItem:async(k:string)=>{if(mocks.removeWait)await mocks.removeWait();mocks.values.delete(k)},getAllKeys:async()=>[...mocks.values.keys()]}}));
import Apply from '../screens/collections/[id]/apply';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{
 mocks.params={id:id(4),tenant:id(2)};mocks.removeWait=null;
 Object.defineProperty(navigator,'locks',{configurable:true,value:{request:async(_key:string,_options:unknown,action:()=>Promise<unknown>)=>action()}});
 mocks.api.collectionApplyCapabilities.mockResolvedValue({available:true});
 mocks.api.projectAccess.mockResolvedValue({id:id(3),can_edit:true});mocks.api.run.mockImplementation(async(run:string)=>({id:run}));
 mocks.api.me.mockResolvedValue({id:id(1)});mocks.api.collection.mockResolvedValue({id:id(4),tenant_id:id(2),project_id:id(3),revision:'7',name:'Original'});mocks.api.styles.mockResolvedValue({items:[{id:id(10),name:'Studio'}]});mocks.api.styleVersions.mockResolvedValue({items:[{id:id(5),version_no:1,approval_status:'accepted'}]});
});
afterEach(()=>{cleanup();mocks.values.clear();mocks.failWrite=false;vi.resetAllMocks()});
async function clickReady(text:string){const label=await screen.findByText(text);const button=label.closest('[role="button"]');expect(button).not.toBeNull();await waitFor(()=>{expect(button!.hasAttribute('disabled')).toBe(false);expectEnabled(button!)});fireEvent.click(button!)}
async function submit(){await clickReady('Studio');await clickReady('Version 1');await clickReady('Start processing')}
it('restores frozen pending request after remount despite changed source revision',async()=>{
 mocks.api.apply.mockRejectedValueOnce(new Error('reply lost')).mockResolvedValueOnce({run_id:id(7),output_collection_id:id(8),status:'queued'});
 const view=render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');const first=mocks.api.apply.mock.calls[0];
 expect([...mocks.values.values()].some(value=>value.includes(first[4]))).toBe(true);view.unmount();
 mocks.api.collection.mockResolvedValue({id:id(4),tenant_id:id(2),project_id:id(3),revision:'99',name:'Changed'});
 render(<Apply/>);await screen.findByDisplayValue('Original edited');fireEvent.click(screen.getByText('Start processing'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply.mock.calls[1].slice(0,5)).toEqual(first.slice(0,5));expect(first[0].revision).toBe('7');
});
it('restores confirmed ticket without another admission and permits acknowledged-only new operation',async()=>{
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 const first=render(<Apply/>);await submit();await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));first.unmount();
 const second=render(<Apply/>);await clickReady('Open existing run');await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(2));expect(mocks.api.apply).toHaveBeenCalledTimes(1);second.unmount();
 render(<Apply/>);await clickReady('Start another operation');await screen.findByText('Start processing');expect([...mocks.values.keys()].filter(key=>key.startsWith('photo.collection-apply.v1:'))).toHaveLength(0);
});
it('refuses HTTP admission when durable pending write fails',async()=>{
 mocks.failWrite=true;render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');expect(mocks.api.apply).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});

it('recovers an accepted run for a viewer without loading styles or posting again',async()=>{
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 const view=render(<Apply/>);await submit();await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));view.unmount();
 mocks.api.projectAccess.mockResolvedValue({id:id(3),can_edit:false});mocks.api.styles.mockClear();mocks.api.styles.mockRejectedValue(new Error('catalog offline'));
 render(<Apply/>);await clickReady('Open existing run');
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(2));expect(mocks.api.styles).not.toHaveBeenCalled();expect(mocks.api.apply).toHaveBeenCalledTimes(1);expect(mocks.api.run).toHaveBeenCalledWith(id(7),expect.any(AbortSignal));
});

it('releases a definitively rejected admission and authors a new key',async()=>{
 mocks.api.apply.mockRejectedValueOnce(new APIError(409,'REVISION_CONFLICT')).mockResolvedValueOnce({run_id:id(7),output_collection_id:id(8),status:'queued'});
 render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.styles).toHaveBeenCalledTimes(2));
 await submit();await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));
 expect(mocks.api.apply.mock.calls[1][4]).not.toBe(mocks.api.apply.mock.calls[0][4]);
});

it('does not allow a rejected style version to start Apply',async()=>{
 mocks.api.styleVersions.mockResolvedValue({items:[{id:id(5),version_no:1,approval_status:'rejected'}]});
 render(<Apply/>);fireEvent.click(await screen.findByText('Studio'));const version=await screen.findByText('Version 1');
 expect(version.closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');fireEvent.click(version);fireEvent.click(screen.getByText('Start processing'));
 expect(mocks.api.apply).not.toHaveBeenCalled();
});
it('clears the style catalogue and disables Apply after version access is revoked',async()=>{
 mocks.api.styleVersions.mockRejectedValue(new APIError(403,'FORBIDDEN'));
 render(<Apply/>);fireEvent.click(await screen.findByText('Studio'));
 await waitFor(()=>expect(screen.queryByText('Studio')).toBeNull());
 expect(screen.getByText('Start processing').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');expect(mocks.api.apply).not.toHaveBeenCalled();
});

function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it('cancels initial options loading and retries without accepting a late old collection',async()=>{
 const old=deferred<{id:string;tenant_id:string;project_id:string;revision:string;name:string}>();
 mocks.api.collection.mockReturnValueOnce(old.promise).mockResolvedValue({id:id(4),tenant_id:id(2),project_id:id(3),revision:'9',name:'Current'});
 render(<Apply/>);
 fireEvent.click(await screen.findByText('Stop loading'));
 const signal=mocks.api.collection.mock.calls[0][1] as AbortSignal;
 expect(signal.aborted).toBe(true);expect(mocks.api.projectAccess).not.toHaveBeenCalled();
 fireEvent.click(await screen.findByText('Retry loading'));
 await screen.findByDisplayValue('Current edited');
 await act(async()=>{old.resolve({id:id(4),tenant_id:id(2),project_id:id(3),revision:'7',name:'Abandoned'});await old.promise});
 expect(screen.queryByDisplayValue('Abandoned edited')).toBeNull();
 expect(mocks.api.projectAccess).toHaveBeenCalledTimes(1);expect(mocks.api.apply).not.toHaveBeenCalled();
});
it('bounds an unresponsive initial options read and exposes retry',async()=>{
 vi.useFakeTimers();
 try{
  mocks.api.collection.mockReturnValueOnce(new Promise(()=>{}));
  render(<Apply/>);
  await act(async()=>{await Promise.resolve();});
  const signal=mocks.api.collection.mock.calls[0][1] as AbortSignal;
  await act(async()=>{await vi.advanceTimersByTimeAsync(8001);});
  expect(signal.aborted).toBe(true);
  expect(screen.getByText('Retry loading')).toBeTruthy();
  expect(mocks.api.projectAccess).not.toHaveBeenCalled();expect(mocks.api.apply).not.toHaveBeenCalled();
 }finally{cleanup();vi.useRealTimers();}
});
it('cancels a style-version page and retries the chosen profile without publishing its late result',async()=>{
 const old=deferred<{items:unknown[]}>();
 mocks.api.styleVersions.mockReturnValueOnce(old.promise).mockResolvedValue({items:[{id:id(6),version_no:2,approval_status:'accepted'}]});
 render(<Apply/>);fireEvent.click(await screen.findByText('Studio'));
 fireEvent.click(await screen.findByText('Stop loading'));
 expect((mocks.api.styleVersions.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
 fireEvent.click(await screen.findByText('Retry loading'));
 await screen.findByText('Version 2');
 await act(async()=>{old.resolve({items:[{id:id(5),version_no:1,approval_status:'accepted'}]});await old.promise});
 expect(screen.queryByText('Version 1')).toBeNull();
 expect(mocks.api.styleVersions.mock.calls.map(call=>call[0])).toEqual([id(10),id(10)]);
 expect(mocks.api.apply).not.toHaveBeenCalled();
});
it('retains a late accepted admission after unmount without navigating the abandoned view',async()=>{
 const admission=deferred<{run_id:string;output_collection_id:string;status:string}>();
 mocks.api.apply.mockReturnValue(admission.promise);
 const view=render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.apply).toHaveBeenCalledTimes(1));
 const signal=mocks.api.apply.mock.calls[0][5] as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);
 await act(async()=>{admission.resolve({run_id:id(7),output_collection_id:id(8),status:'queued'});await admission.promise});
 expect(mocks.replace).not.toHaveBeenCalled();expect(mocks.api.run).not.toHaveBeenCalled();
 render(<Apply/>);await clickReady('Open existing run');
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply).toHaveBeenCalledTimes(1);
});
it('stops waiting without discarding a late accepted receipt and opens it through readonly recovery',async()=>{
 const admission=deferred<{run_id:string;output_collection_id:string;status:string}>();
 mocks.api.apply.mockReturnValueOnce(admission.promise);
 render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.apply).toHaveBeenCalledTimes(1));
 const signal=mocks.api.apply.mock.calls[0][5] as AbortSignal;
 fireEvent.click(await screen.findByText('Stop loading'));
 expect(signal.aborted).toBe(true);
 await act(async()=>{admission.resolve({run_id:id(7),output_collection_id:id(8),status:'queued'});await admission.promise});
 expect(mocks.replace).not.toHaveBeenCalled();expect(mocks.api.run).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Start processing'));
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));
 expect(mocks.api.apply).toHaveBeenCalledTimes(1);expect(mocks.api.run).toHaveBeenCalledWith(id(7),expect.any(AbortSignal));
});
it('cancels accepted-run verification and retries its GET without another Apply',async()=>{
 const old=deferred<{id:string}>();
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 mocks.api.run.mockReturnValueOnce(old.promise).mockResolvedValue({id:id(7)});
 render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledTimes(1));
 fireEvent.click(await screen.findByText('Stop loading'));
 expect((mocks.api.run.mock.calls[0][1] as AbortSignal).aborted).toBe(true);
 fireEvent.click(screen.getByText('Open existing run'));
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));
 await act(async()=>{old.resolve({id:id(7)});await old.promise});
 expect(mocks.replace).toHaveBeenCalledTimes(1);expect(mocks.api.apply).toHaveBeenCalledTimes(1);expect(mocks.api.run).toHaveBeenCalledTimes(2);
});
it('ignores a late collection response after the collection scope changes',async()=>{
 const oldCollection=deferred<{id:string;tenant_id:string;project_id:string;revision:string;name:string}>();
 mocks.api.collection.mockImplementation((collection:string)=>collection===id(4)?oldCollection.promise:Promise.resolve({id:id(9),tenant_id:id(2),project_id:id(3),revision:'8',name:'Current'}));
 const view=render(<Apply/>);await waitFor(()=>expect(mocks.api.collection).toHaveBeenCalledTimes(1));
 const oldSignal=mocks.api.collection.mock.calls[0][1] as AbortSignal;
 mocks.params={id:id(9),tenant:id(2)};view.rerender(<Apply/>);
 await screen.findByDisplayValue('Current edited');expect(oldSignal.aborted).toBe(true);
 await act(async()=>{oldCollection.resolve({id:id(4),tenant_id:id(2),project_id:id(3),revision:'7',name:'Abandoned'});await oldCollection.promise});
 expect(screen.queryByDisplayValue('Abandoned edited')).toBeNull();expect(screen.getByDisplayValue('Current edited')).toBeTruthy();
 expect(mocks.api.projectAccess).toHaveBeenCalledTimes(1);expect(mocks.api.apply).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});
it.each([401,403])('preserves the accepted receipt when run verification returns %s and never repeats admission',async(status)=>{
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 mocks.api.run.mockRejectedValueOnce(new APIError(status,status===401?'UNAUTHORIZED':'FORBIDDEN'));
 const view=render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');
 expect(mocks.replace).not.toHaveBeenCalled();expect(mocks.api.apply).toHaveBeenCalledTimes(1);
 expect([...mocks.values.entries()].some(([key,value])=>key.startsWith('photo.collection-apply.v1:')&&value.includes(id(7)))).toBe(true);
 expect(screen.queryByText('Studio')).toBeNull();expect(screen.getByText('Start processing').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 view.unmount();render(<Apply/>);await clickReady('Open existing run');
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply).toHaveBeenCalledTimes(1);expect(mocks.api.run).toHaveBeenCalledTimes(2);
});

it('does not navigate when accepted-run verification resolves after unmount',async()=>{
 const verifiedRun=deferred<{id:string}>();mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});mocks.api.run.mockReturnValueOnce(verifiedRun.promise);
 const view=render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledTimes(1));
 const signal=mocks.api.run.mock.calls[0][1] as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);
 await act(async()=>{verifiedRun.resolve({id:id(7)});await verifiedRun.promise});
 expect(mocks.replace).not.toHaveBeenCalled();expect(mocks.api.apply).toHaveBeenCalledTimes(1);
 render(<Apply/>);await clickReady('Open existing run');
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply).toHaveBeenCalledTimes(1);
});

it('refuses a new admission when the authoritative style capability is unavailable without freezing a journal',async()=>{
 mocks.api.collectionApplyCapabilities.mockResolvedValue({available:false});render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.collectionApplyCapabilities).toHaveBeenCalledOnce());expect(mocks.api.apply).not.toHaveBeenCalled();expect(mocks.values.size).toBe(0);
});
it('recovers an ambiguous frozen Apply without querying live capability again',async()=>{
 mocks.api.apply.mockRejectedValueOnce(new Error('reply lost')).mockResolvedValueOnce({run_id:id(7),output_collection_id:id(8),status:'queued'});const view=render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');view.unmount();mocks.api.collectionApplyCapabilities.mockClear();mocks.api.collectionApplyCapabilities.mockRejectedValue(new Error('worker gone'));
 render(<Apply/>);await screen.findByDisplayValue('Original edited');await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Start processing'})));await clickWhenEnabled(screen.getByRole('button',{name:'Start processing'}));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledOnce());expect(mocks.api.collectionApplyCapabilities).not.toHaveBeenCalled();expect(mocks.api.apply.mock.calls[1][4]).toBe(mocks.api.apply.mock.calls[0][4]);
});
it('ignores a cancelled late capability result before freezing or admitting a new request',async()=>{
 let finish!:(value:unknown)=>void;mocks.api.collectionApplyCapabilities.mockImplementation(()=>new Promise(done=>{finish=done}));render(<Apply/>);await submit();await waitFor(()=>expect(mocks.api.collectionApplyCapabilities).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Stop loading'));await act(async()=>finish({available:true}));expect(mocks.api.apply).not.toHaveBeenCalled();expect(mocks.values.size).toBe(0);
});

it('can stop an acknowledged journal clear without letting its late completion unlock a replacement run read',async()=>{
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 render(<Apply/>);await submit();await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));
 const clearing=deferred<void>(),runRead=deferred<{id:string}>();
 const remove=vi.fn(()=>clearing.promise);mocks.removeWait=remove;
 fireEvent.click(screen.getByText('Start another operation'));await waitFor(()=>expect(remove).toHaveBeenCalledTimes(1));
 fireEvent.click(await screen.findByText('Stop loading'));
 mocks.api.run.mockReturnValueOnce(runRead.promise);
 fireEvent.click(screen.getByText('Open existing run'));
 await act(async()=>{clearing.resolve();await clearing.promise;});
 await waitFor(()=>expect(mocks.api.run).toHaveBeenCalledTimes(2));
 expect(screen.getByText('Start another operation').closest('[role="button"]')?.getAttribute('aria-disabled')).toBe('true');
 expect(screen.getByText('Stop loading')).toBeTruthy();expect(mocks.api.apply).toHaveBeenCalledTimes(1);
 await act(async()=>{runRead.resolve({id:id(7)});await runRead.promise;});
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(2));
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
