// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import React from 'react';
import {AppState,type AppStateStatus} from 'react-native';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
import {APIError,semanticPreviewQuery,type SemanticPreviewQuery,type SemanticTargetResource} from '@photoeditor/api-client';
// JSDOM does not perform RN Web's detached ImageLoader requests. Expose the
// native image callback boundary explicitly; tests still deliver each role's
// load/error separately and cannot grant shown proof by mounting an image.
vi.mock('react-native',async()=>{
 const web=await import('react-native-web');
 return {...web,Image:({accessibilityLabel,source,onLoad,onError}:any)=><img aria-label={accessibilityLabel} src={source?.uri} onLoad={onLoad} onError={onError}/>};
});
import {SemanticReviewDecisions} from './semantic-review-decisions';
import {SemanticCandidatePreview} from './semantic-candidate-preview';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const item:SemanticTargetResource={id,revision:'1',document:null,data:{resource_type:'semantic_targets',id,tenant_id:id,project_id:id,created_at:'2026-10-05T12:00:00Z',revision:'1',updated_at:'2026-10-05T12:00:00Z',intent_id:id,child_run_id:null,batch_id:null,asset_version_id:id,candidate_artifact_id:null,review_id:null,override_id:null,state:'proposed'}};
const status={run_id:id,workflow_id:id,revision:'9',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:2,can_close:false};
const result={workflow_id:id,revision:'11',remaining:1,batch_id:id,child:{run_id:other,state:'queued' as const}};
afterEach(()=>{cleanup();vi.restoreAllMocks()});
it('uses refreshed parent revision and only explicit selected targets',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,revision:'10'}),applySemanticReview=vi.fn().mockResolvedValue(result),onSubmitted=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item,{...item,id:other,data:{...item.data,id:other}}]} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expect(screen.getByLabelText('reject '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'10',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'reject'}]});
});
it('retains the batch and key after an uncertain response',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({...result,revision:'10',child:null});
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(screen.getByLabelText('reject '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
 // Pressable updates its responder configuration in a passive effect after DOM commit.
 await act(async()=>{});
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry decisions'}));
 await waitFor(()=>expect(applySemanticReview).toHaveBeenCalledTimes(2));expect(applySemanticReview.mock.calls[0].slice(0,4)).toEqual(applySemanticReview.mock.calls[1].slice(0,4));expect(semanticReviewStatus).toHaveBeenCalledTimes(2);
});
it('removes stale authority and decisions after conflict',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockRejectedValue(new APIError(409,'REVISION_CONFLICT'));
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(screen.getByLabelText('reject '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());expect(screen.queryByText('Submit decisions')).toBeNull();expect(screen.queryByText('Retry decisions')).toBeNull();
});

it('clears uncertain decisions when authenticated client is replaced in the same tenant/intent',async()=>{const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockRejectedValue(new Error('lost'))},submitted=vi.fn();const view=render(<SemanticReviewDecisions client={first} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));const oldKey=first.applySemanticReview.mock.calls[0][3];const next={semanticReviewStatus:vi.fn().mockResolvedValue({...status,workflow_id:other,revision:'20',plan_sha:'b'.repeat(64)}),applySemanticReview:vi.fn().mockResolvedValue({...result,workflow_id:other})};view.rerender(<SemanticReviewDecisions client={next} tenant={id} intent={id} items={[{...item,id:other,data:{...item.data,id:other}}]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('reject '+other));expect(screen.queryByText('Retry decisions')).toBeNull();fireEvent.click(screen.getByText('Submit decisions'));await waitFor(()=>expect(next.applySemanticReview).toHaveBeenCalledTimes(1));expect(next.applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'20',plan_sha:'b'.repeat(64),target_decisions:[{target_id:other,decision:'reject'}]});expect(next.applySemanticReview.mock.calls[0][3]).not.toBe(oldKey);});
it('aborts pending old-client admission and ignores its late success',async()=>{let finish!:(value:unknown)=>void;const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))},submitted=vi.fn();const view=render(<SemanticReviewDecisions client={first} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));await waitFor(()=>expect(first.applySemanticReview).toHaveBeenCalledTimes(1));const signal=first.applySemanticReview.mock.calls[0][4] as AbortSignal,next={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};view.rerender(<SemanticReviewDecisions client={next} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);await screen.findByLabelText('reject '+id);await act(async()=>finish(result));expect(signal.aborted).toBe(true);expect(submitted).not.toHaveBeenCalled();expect(next.applySemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Submit decisions')).toBeNull();});

it('mounts a bounded initial decision window for5000 proposed targets',async()=>{const items=Array.from({length:5000},(_,i)=>{const target=`00000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`;return {...item,id:target,data:{...item.data,id:target,asset_version_id:target}};});const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={items} onSubmitted={()=>{}}/>);await screen.findByLabelText('approve '+items[0].id);expect(screen.getAllByLabelText(/^approve /).length).toBeLessThanOrEqual(50);expect(screen.queryByLabelText('approve '+items[4999].id)).toBeNull();expect(client.applySemanticReview).not.toHaveBeenCalled();});
it.each([401,403,404])('drops uncertain batch after authority refresh refuses HTTP%s',async(code)=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce(status).mockRejectedValueOnce(new APIError(code,'DENIED')),applySemanticReview:vi.fn().mockRejectedValue(new Error('lost'))};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('reject '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByRole('button',{name:'Retry decisions'});await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Refresh decision authority'}));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(3));
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Retry decisions'})).toBeNull());expect(screen.queryByText('Submit decisions')).toBeNull();expect(client.applySemanticReview).toHaveBeenCalledTimes(1);
});
it('retains exact uncertain batch through a transient authority-read failure',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce(status).mockRejectedValueOnce(new Error('offline')),applySemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result)};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('reject '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');await act(async()=>{});const original=client.applySemanticReview.mock.calls[0].slice(0,4);
 fireEvent.click(screen.getByText('Refresh decision authority'));await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(3));await act(async()=>{});
 fireEvent.click(screen.getByText('Retry decisions'));await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledTimes(2));expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});
it('does not submit targets removed while fresh authority lookup is pending',async()=>{
 let finish!:(value:typeof status)=>void;const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})),applySemanticReview:vi.fn()};
 const view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('reject '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 view.rerender(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[{...item,data:{...item.data,state:'applied'}}]} onSubmitted={()=>{}}/>);
 await act(async()=>finish(status));expect(client.applySemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Submit decisions')).toBeNull();
});
it('retains immutable uncertain batch when refreshed targets remove its selections',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result)};
 const submitted=vi.fn(),view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);
 await screen.findByLabelText('reject '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');await act(async()=>{});const original=client.applySemanticReview.mock.calls[0].slice(0,4);
 view.rerender(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} onSubmitted={submitted}/>);await act(async()=>{});
 fireEvent.click(screen.getByText('Retry decisions'));await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
});

it('blocks approval without a displayed current candidate while keeping rejection available',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 const approve=await screen.findByLabelText('approve '+id);
 expect(approve.hasAttribute('disabled')).toBe(true);
 fireEvent.click(approve);expect(screen.queryByText('Submit decisions')).toBeNull();
 fireEvent.click(screen.getByLabelText('reject '+id));expect(screen.getByText('Submit decisions')).toBeTruthy();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});

function previewClient(nextStatus:typeof status=status){
 const reviewedItem:SemanticTargetResource={...item,document:{artifact_id:other,sha256:'b'.repeat(64)},data:{...item.data,candidate_artifact_id:other}};
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValue(nextStatus),applySemanticReview:vi.fn().mockResolvedValue(result),semanticReviewPreview:vi.fn().mockImplementation(async (query:SemanticPreviewQuery,_signal?:AbortSignal)=>{
  const media=(n:number)=>({artifact_id:`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha256:'c'.repeat(64),url:`https://media.example/${n}`,expires_at:new Date(Date.now()+60000).toISOString(),width:1024,height:768,media_type:'image/png'});
  return {...query,schema_version:1,render:{artifact_id:'33333333-3333-4333-8333-333333333333',sha256:'d'.repeat(64)},evidence:{artifact_id:'44444444-4444-4444-8444-444444444444',sha256:'e'.repeat(64)},quality_status:'review',limitations:[],protected_pixels_unchanged:true,before:media(10),after:media(11),affected:media(12),protected:media(13)};
 })};
 return {client,reviewedItem};
}
async function showCandidate(){
 fireEvent.click(await screen.findByLabelText('View candidate before approval '+id));
 await screen.findByLabelText('Candidate '+id);
 for(const label of ['Before','Candidate','Affected area','Protected area'])fireEvent.load(screen.getByLabelText(label+' '+id));
 await act(async()=>{});
}
it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s requires all four current preview images to display before approving the exact target',async(kind)=>{
 const {client,reviewedItem}=previewClient(),submitted=vi.fn();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={submitted}/>);
 const approve=await screen.findByLabelText('approve '+id);expect(approve.hasAttribute('disabled')).toBe(true);
 fireEvent.click(screen.getByLabelText('View candidate before approval '+id));await screen.findByLabelText('Candidate '+id);
 for(const label of ['Before','Candidate','Affected area'])fireEvent.load(screen.getByLabelText(label+' '+id));
 await act(async()=>{});expect(approve.hasAttribute('disabled')).toBe(true);
 fireEvent.load(screen.getByLabelText('Protected area '+id));await waitFor(()=>expect(approve.hasAttribute('disabled')).toBe(false));
 fireEvent.click(approve);fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 expect(client.applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'9',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'approve',preview:{render:{artifact_id:'33333333-3333-4333-8333-333333333333',sha256:'d'.repeat(64)},evidence:{artifact_id:'44444444-4444-4444-8444-444444444444',sha256:'e'.repeat(64)}}}]});
});
it('rejects approval when the parent revision changed after preview display',async()=>{
 const {client,reviewedItem}=previewClient({...status,revision:'10'});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByRole('alert');expect(client.applySemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Submit decisions')).toBeNull();
});
it('invalidates displayed approval when an image fails and keeps rejection available',async()=>{
 const {client,reviewedItem}=previewClient();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));expect(screen.getByText('Submit decisions')).toBeTruthy();
 fireEvent.error(screen.getByLabelText('Candidate '+id));await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByText('Submit decisions')).toBeNull();fireEvent.click(screen.getByLabelText('reject '+id));expect(screen.getByText('Submit decisions')).toBeTruthy();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});
it('drops approval if the target candidate changes while the target ID stays the same',async()=>{
 const {client,reviewedItem}=previewClient();
 const view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));
 view.rerender(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[{...reviewedItem,document:{...reviewedItem.document!,sha256:'f'.repeat(64)}}]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByLabelText('Candidate '+id)).toBeNull();expect(screen.queryByText('Submit decisions')).toBeNull();
});

it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s invalidates every approval in background and requires a fresh preview after return',async(kind)=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>{listeners.delete(listener)}}});
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));expect(screen.getByText('Submit decisions')).toBeTruthy();
 act(()=>listeners.forEach(listener=>listener('background')));
 expect(screen.queryByLabelText('approve '+id)).toBeNull();expect(screen.queryByText('Submit decisions')).toBeNull();expect(screen.queryByLabelText('Candidate '+id)).toBeNull();
 act(()=>listeners.forEach(listener=>listener('active')));
 expect(client.semanticReviewPreview).toHaveBeenCalledTimes(1);expect(client.applySemanticReview).not.toHaveBeenCalled();
 expect((await screen.findByLabelText('approve '+id)).hasAttribute('disabled')).toBe(true);
});

it('retries the exact shown selection after uncertain Apply without rereading preview',async()=>{
 const {client,reviewedItem}=previewClient();
 client.applySemanticReview.mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');await act(async()=>{});
 const original=structuredClone(client.applySemanticReview.mock.calls[0].slice(0,4));
 fireEvent.click(screen.getByText('Retry decisions'));
 await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledTimes(2));
 expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
 expect(client.semanticReviewPreview).toHaveBeenCalledTimes(1);
});

it('recovers persisted Apply without rebuilding decisions from current proposals',async()=>{
 const body={expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject' as const}]};
 const saved={version:1 as const,key:other,workflow:id,body};
 const read=vi.fn().mockResolvedValue(saved),write=vi.fn().mockResolvedValue(undefined),clear=vi.fn().mockResolvedValue(undefined);
 const store={assertScope:vi.fn(),read,write,clear} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue({...status,state:'completed',plan_sha:null}),applySemanticReview:vi.fn().mockResolvedValue({...result,revision:'10',child:null})};
 const submitted=vi.fn();render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledTimes(1));
 expect(client.applySemanticReview.mock.calls[0].slice(0,4)).toEqual([id,id,body,other]);
 expect(write).toHaveBeenCalledWith(saved);expect(clear).toHaveBeenCalledWith(other,{...result,revision:'10',child:null});
 expect(write.mock.invocationCallOrder[0]).toBeLessThan(client.applySemanticReview.mock.invocationCallOrder[0]);
});

it('does not send recovered Apply when durable write fails',async()=>{
 const saved={version:1 as const,key:other,workflow:id,body:{expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject' as const}]}};
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockRejectedValue(new Error('disk full')),clear:vi.fn()} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={()=>{}}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await screen.findByRole('alert');expect(client.applySemanticReview).not.toHaveBeenCalled();expect(store.clear).not.toHaveBeenCalled();
});

it('does not send a recovered Apply after background during durable write',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>{listeners.delete(listener)}}});
 const body={expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject' as const}]},saved={version:1 as const,key:other,workflow:id,body};
 let finish!:()=>void;const write=vi.fn(()=>new Promise<void>(done=>{finish=done})),clear=vi.fn();
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write,clear} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);await waitFor(()=>expect(write).toHaveBeenCalled());
 act(()=>listeners.forEach(listener=>listener('background')));await act(async()=>finish());
 expect(client.applySemanticReview).not.toHaveBeenCalled();expect(clear).not.toHaveBeenCalled();expect(submitted).not.toHaveBeenCalled();
 act(()=>listeners.forEach(listener=>listener('active')));await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
});

it('reloads a failed durable journal read and preserves its original request',async()=>{
 const body={expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject' as const}]},saved={version:1 as const,key:other,workflow:id,body};
 const read=vi.fn().mockRejectedValueOnce(new Error('storage unavailable')).mockResolvedValueOnce(saved),write=vi.fn(),clear=vi.fn();
 const store={assertScope:vi.fn(),read,write,clear} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const factory=vi.fn(async()=>store),client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={factory} onSubmitted={submitted}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Reload saved decisions'}));
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 expect(read).toHaveBeenCalledTimes(2);expect(write).not.toHaveBeenCalled();expect(clear).not.toHaveBeenCalled();expect(client.applySemanticReview).not.toHaveBeenCalled();
});

it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s does not count repeated load events for one image as four displayed roles',async(kind)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 fireEvent.click(await screen.findByLabelText('View candidate before approval '+id));
 const candidate=await screen.findByLabelText('Candidate '+id);
 for(let i=0;i<4;i++)fireEvent.load(candidate);
 await act(async()=>{});
 expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true);
 expect(client.applySemanticReview).not.toHaveBeenCalled();
 for(const label of ['Before','Affected area','Protected area'])fireEvent.load(screen.getByLabelText(label+' '+id));
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(false));
});

it('revokes the previous shown approval immediately while replacement preview is pending',async()=>{
 const {client,reviewedItem}=previewClient();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();
 fireEvent.click(screen.getByLabelText('approve '+id));
 expect(screen.getByText('Submit decisions')).toBeTruthy();
 // Pending promise is deliberately resolved only by cleanup cancellation; it
 // cannot grant a new proof while the prior preview is being replaced.
 client.semanticReviewPreview.mockImplementationOnce((_query,signal)=>new Promise<Awaited<ReturnType<typeof client.semanticReviewPreview>>>((_resolve,reject)=>{
  signal?.addEventListener('abort',()=>reject(new Error('aborted')),{once:true});
 }));
 fireEvent.click(screen.getByLabelText('View candidate before approval '+id));
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByText('Submit decisions')).toBeNull();
 expect(screen.queryByLabelText('Candidate '+id)).toBeNull();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});


it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s reads a ready candidate without preview admission and permits rejection when Apply is unavailable',async(kind)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 const semanticPreviewAdmission=vi.fn(),submitted=vi.fn();
 render(<SemanticReviewDecisions client={{...client,semanticPreviewAdmission} as typeof client} tenant={id} intent={id}
  items={[reviewedItem]} actionAvailable={()=>false} onSubmitted={submitted}/>);
 await showCandidate();
 expect(semanticPreviewAdmission).not.toHaveBeenCalled();
 expect(client.semanticReviewPreview).toHaveBeenCalledTimes(1);
 expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true);
 fireEvent.click(screen.getByLabelText('reject '+id));
 fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(submitted).toHaveBeenCalledTimes(1));
 expect(client.applySemanticReview.mock.calls[0][2].target_decisions).toEqual([{target_id:id,decision:'reject'}]);
});


it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s retries the immutable Apply request after deployment availability disappears',async(kind)=>{
 const {client,reviewedItem}=previewClient();const submitted=vi.fn();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 client.applySemanticReview.mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result);
 const props={client,tenant:id,intent:id,items:[reviewedItem],onSubmitted:submitted};
 const view=render(<SemanticReviewDecisions {...props} actionAvailable={()=>true}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');
 const original=structuredClone(client.applySemanticReview.mock.calls[0].slice(0,4));
 view.rerender(<SemanticReviewDecisions {...props} actionAvailable={()=>false}/>);
 const retry=screen.getByRole('button',{name:'Retry decisions'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledTimes(1));
 expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
 expect(client.semanticReviewPreview).toHaveBeenCalledTimes(1);
});


it.each(['background_cleanup','retouch_region','remove_object'] as const)('%s stops a fresh approved Apply after journal IO when deployment disappears and keeps its keys until recovery',async(kind)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 let release!:()=>void;
 const pending=new Promise<void>(resolve=>{release=resolve});
 const write=vi.fn().mockImplementationOnce(()=>pending).mockResolvedValue(undefined);
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(null),write,clear:vi.fn().mockResolvedValue(undefined)} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const journalFactory=async()=>store,submitted=vi.fn();
 const props={client,tenant:id,intent:id,items:[reviewedItem],journalFactory,onSubmitted:submitted};
 const view=render(<SemanticReviewDecisions {...props} actionAvailable={()=>true}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(write).toHaveBeenCalledTimes(1));
 const original=structuredClone(write.mock.calls[0][0]);
 view.rerender(<SemanticReviewDecisions {...props} actionAvailable={()=>false}/>);
 release();await screen.findByRole('alert');
 expect(client.applySemanticReview).not.toHaveBeenCalled();
 view.rerender(<SemanticReviewDecisions {...props} actionAvailable={()=>true}/>);
 const retry=screen.getByRole('button',{name:'Retry decisions'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledTimes(1));
 expect(client.applySemanticReview.mock.calls[0].slice(0,4)).toEqual([id,id,original.body,original.key]);
 expect(client.semanticReviewPreview).toHaveBeenCalledTimes(1);
});


it.each(['Before','Candidate','Affected area','Protected area'])('cleanup drops approval after %s image failure',async(role)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'background_cleanup'});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();
 fireEvent.click(screen.getByLabelText('approve '+id));
 expect(screen.getByText('Submit decisions')).toBeTruthy();
 fireEvent.error(screen.getByLabelText(role+' '+id));
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByText('Submit decisions')).toBeNull();
 fireEvent.click(screen.getByLabelText('reject '+id));
 expect(screen.getByText('Submit decisions')).toBeTruthy();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});

it('cleanup rechecks preview expiry at submit even before the expiry timer runs',async()=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'background_cleanup'});
 const now=Date.now();
 const clock=vi.spyOn(Date,'now').mockReturnValue(now);
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();
 fireEvent.click(screen.getByLabelText('approve '+id));
 expect(screen.getByText('Submit decisions')).toBeTruthy();
 // No fake-timer execution: submit itself must reject expired proof.
 clock.mockReturnValue(now+61000);
 fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByRole('alert');
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});


it.each(['Before','Candidate','Affected area','Protected area'])('retouch requires %s to load before accepting the shown result',async(missing)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'retouch_region'});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 fireEvent.click(await screen.findByLabelText('View candidate before approval '+id));
 await screen.findByLabelText('Candidate '+id);
 for(const role of ['Before','Candidate','Affected area','Protected area'].filter(role=>role!==missing))fireEvent.load(screen.getByLabelText(role+' '+id));
 await act(async()=>{});
 expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true);
 expect(client.applySemanticReview).not.toHaveBeenCalled();
 fireEvent.load(screen.getByLabelText(missing+' '+id));
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(false));
});

it.each(['Before','Candidate','Affected area','Protected area'])('retouch revokes selected approval when %s fails',async(role)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'retouch_region'});
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));
 fireEvent.error(screen.getByLabelText(role+' '+id));
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByText('Submit decisions')).toBeNull();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});

it.each(['revision','asset','candidate'])('retouch invalidates shown ownership after target %s changes',async(field)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'retouch_region'});
 const props={client,tenant:id,intent:id,onSubmitted:()=>{}};
 const view=render(<SemanticReviewDecisions {...props} items={[reviewedItem]}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));
 const changed=structuredClone(reviewedItem);
 if(field==='revision'){changed.revision='2';changed.data.revision='2'}
 if(field==='asset')changed.data.asset_version_id=other;
 if(field==='candidate')changed.document!.sha256='f'.repeat(64);
 view.rerender(<SemanticReviewDecisions {...props} items={[changed]}/>);
 await waitFor(()=>expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true));
 expect(screen.queryByLabelText('Candidate '+id)).toBeNull();
 expect(screen.queryByText('Submit decisions')).toBeNull();
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});

it.each(['retouch_region','remove_object'] as const)('%s checks expiry again when submitting the exact shown selection',async(kind)=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind});
 const now=Date.now(),clock=vi.spyOn(Date,'now').mockReturnValue(now);
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={()=>{}}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));
 clock.mockReturnValue(now+61000);
 fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByRole('alert');
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});


it('keeps the in-flight preview fence independent of caller mutation',async()=>{
 const {client,reviewedItem}=previewClient();
 const query=semanticPreviewQuery(id,id,reviewedItem,{...status,kind:'retouch_region'} as Parameters<typeof semanticPreviewQuery>[3]);
 const original=client.semanticReviewPreview.getMockImplementation()!;
 let release!:()=>void;
 const gate=new Promise<void>(resolve=>{release=resolve});
 client.semanticReviewPreview.mockImplementationOnce(async(bound,signal)=>{await gate;return original(bound,signal)});
 const ready=vi.fn();
 render(<SemanticCandidatePreview client={client} query={query} onReady={ready}/>);
 fireEvent.click(await screen.findByLabelText('View candidate before approval '+id));
 await waitFor(()=>expect(client.semanticReviewPreview).toHaveBeenCalledOnce());
 query.candidate.sha256='f'.repeat(64);
 release();await screen.findByLabelText('Candidate '+id);
 for(const role of ['Before','Candidate','Affected area','Protected area'])fireEvent.load(screen.getByLabelText(role+' '+id));
 await waitFor(()=>expect(ready.mock.calls.some(([value])=>value?.candidate.sha256==='b'.repeat(64))).toBe(true));
 expect(client.applySemanticReview).not.toHaveBeenCalled();
});


it('remove_object refuses a fresh approval expiring during durable journal IO',async()=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'remove_object'});
 let release!:()=>void;
 const write=vi.fn(()=>new Promise<void>(resolve=>{release=resolve}));
 const clear=vi.fn(),submitted=vi.fn();
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(null),write,clear} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 await showCandidate();
 fireEvent.click(screen.getByLabelText('approve '+id));
 fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(write).toHaveBeenCalledOnce());
 const afterExpiry=Date.now()+61000;
 vi.spyOn(Date,'now').mockReturnValue(afterExpiry);
 await act(async()=>release());
 await screen.findByRole('alert');
 expect(client.applySemanticReview).not.toHaveBeenCalled();
 expect(clear).not.toHaveBeenCalled();expect(submitted).not.toHaveBeenCalled();
 expect(screen.getByText('Retry decisions')).toBeTruthy();
});


it('remove_object retries already-sent exact choice after preview expiry',async()=>{
 const {client,reviewedItem}=previewClient();
 client.semanticReviewStatus.mockReset().mockResolvedValue({...status,kind:'remove_object'});
 client.applySemanticReview.mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce(result);
 const submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[reviewedItem]} onSubmitted={submitted}/>);
 await showCandidate();fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');
 const original=structuredClone(client.applySemanticReview.mock.calls[0].slice(0,4));
 const afterExpiry=Date.now()+61000;vi.spyOn(Date,'now').mockReturnValue(afterExpiry);
 fireEvent.click(screen.getByText('Retry decisions'));
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
 expect(client.semanticReviewPreview).toHaveBeenCalledOnce();
});

it.each(['local_exposure','remove_object','background_cleanup','retouch_region'] as const)('%s permits rejection with no preview endpoint and no available AI',async(kind)=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue({...status,kind}),
  applySemanticReview=vi.fn().mockResolvedValue({...result,child:null}),submitted=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id}
  items={[item]} actionAvailable={()=>false} onSubmitted={submitted}/>);
 const reject=await screen.findByLabelText('reject '+id);
 expect(reject.hasAttribute('disabled')).toBe(false);
 expect(screen.getByLabelText('approve '+id).hasAttribute('disabled')).toBe(true);
 fireEvent.click(reject);fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 expect(applySemanticReview).toHaveBeenCalledOnce();
 expect(applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject'}]});
 expect(applySemanticReview.mock.calls[0][3]).toEqual(expect.any(String));
});

it.each([401,403,404])('rechecks review authority on foreground and hides actions after %s',async(code)=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 const semanticReviewStatus=vi.fn().mockResolvedValueOnce(status).mockRejectedValueOnce(new APIError(code,'ACCESS_DENIED'));
 const applySemanticReview=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 fireEvent.click(await screen.findByLabelText('reject '+id));
 act(()=>listeners.forEach(listener=>listener('background')));
 expect(screen.queryByText('Submit decisions')).toBeNull();
 act(()=>listeners.forEach(listener=>listener('active')));
 await waitFor(()=>expect(semanticReviewStatus).toHaveBeenCalledTimes(2));
 await act(async()=>{});
 expect(screen.queryByLabelText('reject '+id)).toBeNull();expect(screen.queryByText('Submit decisions')).toBeNull();
 expect(applySemanticReview).not.toHaveBeenCalled();
});

it('starts a fresh foreground read while aborted old status never settles and ignores its late response',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();
 vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>listeners.delete(listener)}});
 let old!:(value:unknown)=>void,fresh!:(value:unknown)=>void;
 const semanticReviewStatus=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{fresh=resolve}));
 const applySemanticReview=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(semanticReviewStatus).toHaveBeenCalledOnce());
 const signal=semanticReviewStatus.mock.calls[0][2] as AbortSignal;
 act(()=>listeners.forEach(listener=>listener('background')));
 expect(signal.aborted).toBe(true);
 act(()=>listeners.forEach(listener=>listener('active')));
 await waitFor(()=>expect(semanticReviewStatus).toHaveBeenCalledTimes(2));
 await act(async()=>old(status));
 expect(screen.queryByLabelText('reject '+id)).toBeNull();
 await act(async()=>fresh({...status,state:'completed',remaining:0,can_close:false}));
 expect(screen.queryByLabelText('reject '+id)).toBeNull();
 expect(applySemanticReview).not.toHaveBeenCalled();
});

it.each(['tenant','intent'] as const)('ignores old status after %s scope replacement',async(scope)=>{
 let old!:(value:unknown)=>void;
 const semanticReviewStatus=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockResolvedValue({...status,state:'completed',remaining:0,can_close:false});
 const applySemanticReview=vi.fn(),client={semanticReviewStatus,applySemanticReview};
 const view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(semanticReviewStatus).toHaveBeenCalledOnce());
 const signal=semanticReviewStatus.mock.calls[0][2] as AbortSignal;
 const tenant=scope==='tenant'?other:id,intent=scope==='intent'?other:id;
 view.rerender(<SemanticReviewDecisions client={client} tenant={tenant} intent={intent} items={[{...item,data:{...item.data,tenant_id:tenant,intent_id:intent}}]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(semanticReviewStatus).toHaveBeenCalledTimes(2));
 expect(signal.aborted).toBe(true);
 await act(async()=>old(status));
 expect(screen.queryByLabelText('reject '+id)).toBeNull();
 expect(semanticReviewStatus.mock.calls[1].slice(0,2)).toEqual([tenant,intent]);
 expect(applySemanticReview).not.toHaveBeenCalled();
});

it.each([400,401,403,404,409,410,422])('requires a fresh fence and new key after definitive non-journal refusal %s',async(code)=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockRejectedValueOnce(new APIError(code,'REVISION_CONFLICT')).mockResolvedValueOnce(result)};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 fireEvent.click(await screen.findByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledOnce());
 await waitFor(()=>expect(screen.queryByText('Retry decisions')).toBeNull());
 await waitFor(()=>expect(screen.queryByLabelText('reject '+id)).toBeNull());
 const oldKey=client.applySemanticReview.mock.calls[0][3];
 client.semanticReviewStatus.mockResolvedValue({...status,revision:'10',plan_sha:'b'.repeat(64)});
 fireEvent.click(screen.getByText('Refresh decision authority'));
 fireEvent.click(await screen.findByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledTimes(2));
 expect(client.applySemanticReview.mock.calls[1][2]).toEqual({expected_revision:'10',plan_sha:'b'.repeat(64),target_decisions:[{target_id:id,decision:'reject'}]});
 expect(client.applySemanticReview.mock.calls[1][3]).not.toBe(oldKey);
});


it.each([400,401,403,404,409,410,422])('preserves durable uncertain Apply after HTTP%s and replays its exact key/body',async(code)=>{
 const body={expected_revision:'9',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'reject' as const}]};
 const saved={version:1 as const,key:other,workflow:id,body};
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockResolvedValue(undefined)} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const receipt={...result,revision:'10',child:null};
 const client={semanticReviewStatus:vi.fn().mockResolvedValue({...status,revision:'20',plan_sha:'b'.repeat(64)}),applySemanticReview:vi.fn().mockRejectedValueOnce(new APIError(code,'REVISION_CONFLICT')).mockResolvedValueOnce(receipt)},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await screen.findByRole('alert');await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe([401,403,404,410].includes(code)));
 if([401,403,404,410].includes(code)){fireEvent.click(retry);expect(client.applySemanticReview).toHaveBeenCalledTimes(1);}
 expect(store.clear).not.toHaveBeenCalled();expect(submitted).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'Refresh decision authority'}));await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 for(const call of client.applySemanticReview.mock.calls)expect(call.slice(0,4)).toEqual([id,id,body,other]);
 expect(store.write).toHaveBeenCalledTimes(2);expect(store.clear).toHaveBeenCalledWith(other,receipt);
});

it('retries acknowledged journal cleanup without rewriting or resending Apply',async()=>{
 const body={expected_revision:'9',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'reject' as const}]};
 const saved={version:1 as const,key:other,workflow:id,body},receipt={...result,revision:'10',child:null};
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockRejectedValueOnce(new Error('record deletion failed after marker')).mockResolvedValueOnce(undefined)} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockResolvedValue(receipt)},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await screen.findByRole('alert');expect(submitted).not.toHaveBeenCalled();
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 expect(store.write).toHaveBeenCalledTimes(1);expect(client.applySemanticReview).toHaveBeenCalledTimes(1);
 expect(store.clear).toHaveBeenCalledTimes(2);for(const call of vi.mocked(store.clear).mock.calls)expect(call).toEqual([other,receipt]);
});


it('recovers partial acknowledgement with the real durable store and fences a remounted reader',async()=>{
 const {SemanticApplyAttemptStore}=await import('./semantic-apply-attempt-store');
 const values=new Map<string,string>();let failDelete=true;
 const storage={getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value)},removeItem:async(key:string)=>{if(failDelete){failDelete=false;throw new Error('disk unavailable')}values.delete(key)}};
 const origin='https://semantic-cleanup.example',store=new SemanticApplyAttemptStore(storage,origin,id,id,id,async(_origin,action)=>action());
 const body={expected_revision:'9',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'reject' as const}]};
 const saved={version:1 as const,key:other,workflow:id,body},receipt={...result,revision:'10',child:null};
 await store.write(saved);
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockResolvedValue(receipt)},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await screen.findByRole('alert');expect(values.has(store.key)).toBe(true);expect(submitted).not.toHaveBeenCalled();
 const remounted=new SemanticApplyAttemptStore(storage,origin,id,id,id,async(_origin,action)=>action());
 expect(await remounted.read()).toBeNull();
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());expect(values.has(store.key)).toBe(false);expect(client.applySemanticReview).toHaveBeenCalledTimes(1);
});


it('delivers acknowledged completion after background interrupts journal cleanup without resending',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>{listeners.delete(listener)}}});
 const body={expected_revision:'9',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'reject' as const}]},saved={version:1 as const,key:other,workflow:id,body},receipt={...result,revision:'10',child:null};
 let finish!:()=>void;
 const clear=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve})).mockResolvedValue(undefined);
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),clear} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockResolvedValue(receipt)},submitted=vi.fn();
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} journalFactory={async()=>store} onSubmitted={submitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));await clickWhenEnabled(retry);
 await waitFor(()=>expect(clear).toHaveBeenCalledTimes(1));act(()=>listeners.forEach(listener=>listener('background')));
 await act(async()=>finish());expect(submitted).not.toHaveBeenCalled();
 act(()=>listeners.forEach(listener=>listener('active')));await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
 await clickWhenEnabled(screen.getByRole('button',{name:'Retry decisions'}));await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());
 expect(client.applySemanticReview).toHaveBeenCalledTimes(1);expect(store.write).toHaveBeenCalledTimes(1);expect(clear).toHaveBeenCalledTimes(2);
});

it('cancelled decision mutation retains exact batch while old response cannot finish a new retry',async()=>{
 let first!:(value:typeof result)=>void,second!:(value:typeof result)=>void;const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{first=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{second=resolve})),onSubmitted=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={onSubmitted}/>);await screen.findByLabelText('reject '+id);fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));await waitFor(()=>expect(applySemanticReview).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry decisions'));await waitFor(()=>expect(applySemanticReview).toHaveBeenCalledTimes(2));
 await act(async()=>first(result));expect(onSubmitted).not.toHaveBeenCalled();expect(screen.getByText('Stop loading')).toBeTruthy();await act(async()=>second(result));expect(onSubmitted).toHaveBeenCalledOnce();expect(applySemanticReview.mock.calls[0].slice(0,4)).toEqual(applySemanticReview.mock.calls[1].slice(0,4));
});
it('cancelled journal bootstrap exposes explicit readonly journal recovery and never submits',async()=>{
 let finish!:(value:any)=>void;const journalFactory=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})),semanticReviewStatus=vi.fn(),applySemanticReview=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} journalFactory={journalFactory} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(journalFactory).toHaveBeenCalledOnce());fireEvent.click(screen.getByText('Stop loading'));await screen.findByText('Reload saved decisions');await act(async()=>finish({assertScope:vi.fn(),read:vi.fn().mockResolvedValue(null)}));expect(semanticReviewStatus).not.toHaveBeenCalled();expect(applySemanticReview).not.toHaveBeenCalled();
});

it('cancelled candidate preview cannot clear a replacement preview after its old request settles',async()=>{
 const {client,reviewedItem}=previewClient(),query=semanticPreviewQuery(id,id,reviewedItem,{...status,kind:'retouch_region'} as Parameters<typeof semanticPreviewQuery>[3]);
 const original=client.semanticReviewPreview.getMockImplementation()!;let release!:()=>void;const gate=new Promise<void>(resolve=>{release=resolve});
 client.semanticReviewPreview.mockImplementationOnce(async(bound,signal)=>{await gate;return original(bound,signal)});
 render(<SemanticCandidatePreview client={client} query={query} onReady={vi.fn()}/>);fireEvent.click(screen.getByLabelText('View candidate before approval '+id));await waitFor(()=>expect(client.semanticReviewPreview).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByLabelText('View candidate before approval '+id));await screen.findByLabelText('Candidate '+id);
 await act(async()=>release());expect(screen.getByLabelText('Candidate '+id)).toBeTruthy();expect(client.semanticReviewPreview).toHaveBeenCalledTimes(2);
});

it('queued expiry from a superseded candidate cannot clear its replacement or revoke its displayed proof',async()=>{
 const {client,reviewedItem}=previewClient(),query=semanticPreviewQuery(id,id,reviewedItem,{...status,kind:'retouch_region'} as Parameters<typeof semanticPreviewQuery>[3]);
 const timeout=globalThis.setTimeout,expiries:Array<()=>void>=[];
 vi.spyOn(globalThis,'setTimeout').mockImplementation(((handler:TimerHandler,delay?:number,...args:unknown[])=>{if(typeof handler==='function'&&delay!==undefined&&delay>50000&&delay<100000)expiries.push(handler as ()=>void);return timeout(handler,delay,...args);}) as typeof setTimeout);
 const ready=vi.fn();render(<SemanticCandidatePreview client={client} query={query} onReady={ready}/>);fireEvent.click(screen.getByLabelText('View candidate before approval '+id));await screen.findByLabelText('Candidate '+id);const oldExpiry=expiries[0];expect(oldExpiry).toBeTypeOf('function');
 fireEvent.click(screen.getByLabelText('View candidate before approval '+id));await waitFor(()=>expect(client.semanticReviewPreview).toHaveBeenCalledTimes(2));await screen.findByLabelText('Candidate '+id);
 for(const role of ['Before','Candidate','Affected area','Protected area'])fireEvent.load(screen.getByLabelText(role+' '+id));await waitFor(()=>expect(ready.mock.lastCall?.[0]).not.toBeNull());const accepted=ready.mock.lastCall?.[0];
 act(()=>oldExpiry());expect(screen.getByLabelText('Candidate '+id)).toBeTruthy();expect(ready.mock.lastCall?.[0]).toBe(accepted);
});
it('background aborts pending actor bootstrap and foreground reads a fresh journal before enabling decisions',async()=>{
 const listeners=new Set<(state:AppStateStatus)=>void>();vi.spyOn(AppState,'addEventListener').mockImplementation((_event,listener)=>{listeners.add(listener);return {remove:()=>{listeners.delete(listener)}}});
 let finish!:(value:any)=>void;const oldRead=vi.fn().mockResolvedValue(null),freshRead=vi.fn().mockResolvedValue(null),journalFactory=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({assertScope:vi.fn(),read:freshRead});
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn();render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} journalFactory={journalFactory} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(journalFactory).toHaveBeenCalledOnce());const signal=journalFactory.mock.calls[0][0] as AbortSignal;act(()=>listeners.forEach(listener=>listener('background')));expect(signal.aborted).toBe(true);
 await act(async()=>finish({assertScope:vi.fn(),read:oldRead}));expect(oldRead).not.toHaveBeenCalled();expect(semanticReviewStatus).not.toHaveBeenCalled();
 act(()=>listeners.forEach(listener=>listener('active')));await screen.findByLabelText('reject '+id);expect(journalFactory).toHaveBeenCalledTimes(2);expect(freshRead).toHaveBeenCalledOnce();expect(applySemanticReview).not.toHaveBeenCalled();
});

it('each replacement decision flight owns a fresh deadline independent of rendered busy state',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={vi.fn()}/>);await screen.findByLabelText('reject '+id);fireEvent.click(screen.getByLabelText('reject '+id));
 vi.useFakeTimers();try{
  fireEvent.click(screen.getByText('Submit decisions'));await act(async()=>{for(let n=0;n<8;n++)await Promise.resolve()});expect(applySemanticReview).toHaveBeenCalledOnce();
  await act(async()=>{await vi.advanceTimersByTimeAsync(10000)});fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry decisions'));await act(async()=>{for(let n=0;n<8;n++)await Promise.resolve()});expect(applySemanticReview).toHaveBeenCalledTimes(2);
  const next=applySemanticReview.mock.calls[1][4] as AbortSignal;await act(async()=>{await vi.advanceTimersByTimeAsync(5001)});expect(next.aborted).toBe(false);
  await act(async()=>{await vi.advanceTimersByTimeAsync(10000)});expect(next.aborted).toBe(true);expect(screen.getByText('Retry decisions')).toBeTruthy();
 }finally{vi.useRealTimers();}
});
