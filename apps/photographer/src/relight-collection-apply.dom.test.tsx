// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {clickWhenEnabled} from './test/ui-readiness';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
const memory=vi.hoisted(()=>({values:new Map<string,string>(),failWrite:false}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>memory.values.get(k)??null,setItem:async(k:string,v:string)=>{if(memory.failWrite)throw new Error('storage');memory.values.set(k,v)},removeItem:async(k:string)=>{memory.values.delete(k)},getAllKeys:async()=>[...memory.values.keys()]}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async<T,>(_origin:string,action:()=>Promise<T>)=>action()}));
import {RelightCollectionApply} from './relight-collection-apply';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const review:Review={id:id(1),run_id:id(2),kind:'image.relight',state:'decided',revision:2,decision:'accept',decision_hash:'a'.repeat(64),candidate_hash:'b'.repeat(64),reasons:[],created_at:'2026-10-16T00:00:00Z'};
const target={collection:id(3),item:id(4),asset:id(5),revision:'7',expectedVersion:id(6),name:'Portraits'},version=id(7),origin='https://collection.test';
function fixture(){let applied=false;const api={me:vi.fn().mockResolvedValue({id:id(10)}),relightCollectionTargets:vi.fn().mockResolvedValue({items:[target],next_cursor:null}),applyRelightSelected:vi.fn().mockResolvedValue({asset_id:target.asset,asset_version_id:version}),reconcileCollectionVersion:vi.fn(async():Promise<'applied'|'unchanged'|'conflict'>=>applied?'applied' as const:'unchanged' as const),replaceCollectionVersion:vi.fn(async(_selection?:unknown,_signal?:AbortSignal)=>{applied=true;return {id:target.item,collection_id:target.collection,asset_id:target.asset,asset_version_id:version}})};return {api,onOpen:vi.fn()};}
function show(f:ReturnType<typeof fixture>){return render(<RelightCollectionApply api={f.api as unknown as OwnerAPI} origin={origin} tenant={id(11)} review={review} onOpen={f.onOpen}/>)}
beforeEach(()=>{memory.values.clear();memory.failWrite=false});afterEach(()=>{cleanup();vi.useRealTimers();});
it('persists the immutable choice before CAS and opens the editor with collection scope',async()=>{
 const f=fixture();show(f);fireEvent.click(await screen.findByText('Use in Portraits'));
 await waitFor(()=>expect(f.onOpen).toHaveBeenCalledWith({collection:target.collection,item:target.item,asset:target.asset,revision:'7',expectedVersion:target.expectedVersion,version}));
 expect(f.api.replaceCollectionVersion).toHaveBeenCalledTimes(1);expect(memory.values.size).toBe(0);
});
it('recovers a persisted intent after remount without another Apply request',async()=>{
 const f=fixture();f.api.replaceCollectionVersion.mockRejectedValueOnce(new Error('lost'));
 const view=show(f);await clickWhenEnabled(await screen.findByRole('button',{name:'Use in Portraits'}));await screen.findByRole('alert');
 expect(memory.values.size).toBe(1);view.unmount();
 f.api.reconcileCollectionVersion.mockResolvedValue('applied');show(f);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Recover saved collection choice'}));
 await waitFor(()=>expect(f.onOpen).toHaveBeenCalledTimes(1));expect(f.api.applyRelightSelected).toHaveBeenCalledTimes(1);expect(f.api.replaceCollectionVersion).toHaveBeenCalledTimes(1);
});
it('never changes the collection when durable intent storage fails',async()=>{
 const f=fixture();show(f);await screen.findByText('Use in Portraits');memory.failWrite=true;
 fireEvent.click(screen.getByText('Use in Portraits'));await screen.findByRole('alert');
 expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();expect(f.onOpen).not.toHaveBeenCalled();
});
it('offers explicit conflict abandonment without overwriting or refreshing the saved CAS',async()=>{
 const f=fixture();f.api.reconcileCollectionVersion.mockResolvedValue('conflict');show(f);
 fireEvent.click(await screen.findByText('Use in Portraits'));await screen.findByRole('alert');
 expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Discard conflicted collection choice'));
 await waitFor(()=>expect(memory.values.size).toBe(0));await screen.findByText('Use in Portraits');
 expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();expect(f.onOpen).not.toHaveBeenCalled();
});
it('does not navigate or persist after unmount during materialization',async()=>{
 const f=fixture();let resolve!:(v:{asset_id:string;asset_version_id:string})=>void;
 f.api.applyRelightSelected.mockImplementation(()=>new Promise(done=>{resolve=done}));
 const view=show(f);fireEvent.click(await screen.findByText('Use in Portraits'));
 await waitFor(()=>expect(f.api.applyRelightSelected).toHaveBeenCalledTimes(1));view.unmount();await act(async()=>{resolve({asset_id:target.asset,asset_version_id:version});});
 await waitFor(()=>expect(f.api.me).toHaveBeenCalled());expect(f.onOpen).not.toHaveBeenCalled();expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();
});

it('retries a failed initial readonly inventory without producing or changing a version',async()=>{
 const f=fixture();f.api.relightCollectionTargets.mockRejectedValueOnce(new Error('offline'));show(f);fireEvent.click(await screen.findByText('read_retry'));await screen.findByText('Use in Portraits');expect(f.api.applyRelightSelected).not.toHaveBeenCalled();expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();
});
it('retains a locally frozen choice after storage failure and retries persistence without another Apply',async()=>{
 const f=fixture();show(f);await screen.findByText('Use in Portraits');memory.failWrite=true;fireEvent.click(screen.getByText('Use in Portraits'));await screen.findByRole('alert');expect(f.api.replaceCollectionVersion).not.toHaveBeenCalled();
 memory.failWrite=false;fireEvent.click(screen.getByText('Recover saved collection choice'));await waitFor(()=>expect(f.onOpen).toHaveBeenCalledOnce());expect(f.api.applyRelightSelected).toHaveBeenCalledOnce();expect(f.api.replaceCollectionVersion).toHaveBeenCalledOnce();
});
it('cancels a pending collection CAS and recovers its frozen revision and target without another Apply',async()=>{
 const f=fixture(),complete=f.api.replaceCollectionVersion.getMockImplementation()!;let finish!:(value:Awaited<ReturnType<typeof complete>>)=>void;
 f.api.replaceCollectionVersion.mockImplementationOnce(()=>new Promise<Awaited<ReturnType<typeof complete>>>(resolve=>{finish=resolve;})).mockImplementationOnce(complete);
 show(f);fireEvent.click(await screen.findByText('Use in Portraits'));await waitFor(()=>expect(f.api.replaceCollectionVersion).toHaveBeenCalledOnce());const first=f.api.replaceCollectionVersion.mock.calls[0];
 fireEvent.click(screen.getByText('Cancel collection request'));await act(async()=>finish({id:target.item,collection_id:target.collection,asset_id:target.asset,asset_version_id:version}));expect(f.onOpen).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Recover saved collection choice'));await waitFor(()=>expect(f.onOpen).toHaveBeenCalledOnce());expect(f.api.applyRelightSelected).toHaveBeenCalledOnce();expect(f.api.replaceCollectionVersion.mock.calls[1][0]).toEqual(first[0]);
});
