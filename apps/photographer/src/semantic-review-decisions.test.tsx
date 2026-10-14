// @vitest-environment jsdom
import React from 'react';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
import {APIError,type SemanticTargetResource} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticReviewDecisions} from './semantic-review-decisions';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const item:SemanticTargetResource={id,revision:'1',document:null,data:{resource_type:'semantic_targets',id,tenant_id:id,project_id:id,created_at:'2026-10-05T12:00:00Z',revision:'1',updated_at:'2026-10-05T12:00:00Z',intent_id:id,child_run_id:null,batch_id:null,asset_version_id:id,candidate_artifact_id:null,review_id:null,override_id:null,state:'proposed'}};
const status={run_id:id,workflow_id:id,revision:'9',state:'waiting_review' as const,plan_sha:'a'.repeat(64),remaining:2,can_close:false};
const result={workflow_id:id,revision:'11',remaining:1,batch_id:id,child:{run_id:other,state:'queued' as const}};
afterEach(cleanup);
it('uses refreshed parent revision and only explicit selected targets',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce({...status,revision:'10'}),applySemanticReview=vi.fn().mockResolvedValue(result),onSubmitted=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item,{...item,id:other,data:{...item.data,id:other}}]} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expect(screen.getByLabelText('approve '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'10',plan_sha:status.plan_sha,target_decisions:[{target_id:id,decision:'approve'}]});
});
it('retains the batch and key after an uncertain response',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({...result,revision:'10',child:null});
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(screen.getByLabelText('reject '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
 // Pressable updates its responder configuration in a passive effect after DOM commit.
 await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Retry decisions'}));
 await waitFor(()=>expect(applySemanticReview).toHaveBeenCalledTimes(2));expect(applySemanticReview.mock.calls[0].slice(0,4)).toEqual(applySemanticReview.mock.calls[1].slice(0,4));expect(semanticReviewStatus).toHaveBeenCalledTimes(2);
});
it('removes stale authority and decisions after conflict',async()=>{
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockRejectedValue(new APIError(409,'REVISION_CONFLICT'));
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await waitFor(()=>expect(screen.getByLabelText('approve '+id)).toBeTruthy());fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());expect(screen.queryByText('Submit decisions')).toBeNull();expect(screen.queryByText('Retry decisions')).toBeNull();
});

it('clears uncertain decisions when authenticated client is replaced in the same tenant/intent',async()=>{const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockRejectedValue(new Error('lost'))},submitted=vi.fn();const view=render(<SemanticReviewDecisions client={first} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));const oldKey=first.applySemanticReview.mock.calls[0][3];const next={semanticReviewStatus:vi.fn().mockResolvedValue({...status,workflow_id:other,revision:'20',plan_sha:'b'.repeat(64)}),applySemanticReview:vi.fn().mockResolvedValue({...result,workflow_id:other})};view.rerender(<SemanticReviewDecisions client={next} tenant={id} intent={id} items={[{...item,id:other,data:{...item.data,id:other}}]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('reject '+other));expect(screen.queryByText('Retry decisions')).toBeNull();fireEvent.click(screen.getByText('Submit decisions'));await waitFor(()=>expect(next.applySemanticReview).toHaveBeenCalledTimes(1));expect(next.applySemanticReview.mock.calls[0][2]).toEqual({expected_revision:'20',plan_sha:'b'.repeat(64),target_decisions:[{target_id:other,decision:'reject'}]});expect(next.applySemanticReview.mock.calls[0][3]).not.toBe(oldKey);});
it('aborts pending old-client admission and ignores its late success',async()=>{let finish!:(value:unknown)=>void;const first={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))},submitted=vi.fn();const view=render(<SemanticReviewDecisions client={first} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);fireEvent.click(await screen.findByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));await waitFor(()=>expect(first.applySemanticReview).toHaveBeenCalledTimes(1));const signal=first.applySemanticReview.mock.calls[0][4] as AbortSignal,next={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};view.rerender(<SemanticReviewDecisions client={next} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);await screen.findByLabelText('approve '+id);await act(async()=>finish(result));expect(signal.aborted).toBe(true);expect(submitted).not.toHaveBeenCalled();expect(next.applySemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Submit decisions')).toBeNull();});

it('mounts a bounded initial decision window for5000 proposed targets',async()=>{const items=Array.from({length:5000},(_,i)=>{const target=`00000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`;return {...item,id:target,data:{...item.data,id:target,asset_version_id:target}};});const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn()};render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={items} onSubmitted={()=>{}}/>);await screen.findByLabelText('approve '+items[0].id);expect(screen.getAllByLabelText(/^approve /).length).toBeLessThanOrEqual(50);expect(screen.queryByLabelText('approve '+items[4999].id)).toBeNull();expect(client.applySemanticReview).not.toHaveBeenCalled();});
it.each([401,403,404])('drops uncertain batch after authority refresh refuses HTTP%s',async(code)=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce(status).mockRejectedValueOnce(new APIError(code,'DENIED')),applySemanticReview:vi.fn().mockRejectedValue(new Error('lost'))};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('approve '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByRole('button',{name:'Retry decisions'});await act(async()=>{});
 fireEvent.click(screen.getByRole('button',{name:'Refresh decision authority'}));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(3));
 await waitFor(()=>expect(screen.queryByRole('button',{name:'Retry decisions'})).toBeNull());expect(screen.queryByText('Submit decisions')).toBeNull();expect(client.applySemanticReview).toHaveBeenCalledTimes(1);
});
it('retains exact uncertain batch through a transient authority-read failure',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockResolvedValueOnce(status).mockRejectedValueOnce(new Error('offline')),applySemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result)};
 render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('approve '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');await act(async()=>{});const original=client.applySemanticReview.mock.calls[0].slice(0,4);
 fireEvent.click(screen.getByText('Refresh decision authority'));await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(3));await act(async()=>{});
 fireEvent.click(screen.getByText('Retry decisions'));await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledTimes(2));expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});
it('does not submit targets removed while fresh authority lookup is pending',async()=>{
 let finish!:(value:typeof status)=>void;const client={semanticReviewStatus:vi.fn().mockResolvedValueOnce(status).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})),applySemanticReview:vi.fn()};
 const view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={()=>{}}/>);
 await screen.findByLabelText('approve '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2));
 view.rerender(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[{...item,data:{...item.data,state:'applied'}}]} onSubmitted={()=>{}}/>);
 await act(async()=>finish(status));expect(client.applySemanticReview).not.toHaveBeenCalled();expect(screen.queryByText('Submit decisions')).toBeNull();
});
it('retains immutable uncertain batch when refreshed targets remove its selections',async()=>{
 const client={semanticReviewStatus:vi.fn().mockResolvedValue(status),applySemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(result)};
 const submitted=vi.fn(),view=render(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[item]} onSubmitted={submitted}/>);
 await screen.findByLabelText('approve '+id);await act(async()=>{});fireEvent.click(screen.getByLabelText('approve '+id));fireEvent.click(screen.getByText('Submit decisions'));
 await screen.findByText('Retry decisions');await act(async()=>{});const original=client.applySemanticReview.mock.calls[0].slice(0,4);
 view.rerender(<SemanticReviewDecisions client={client} tenant={id} intent={id} items={[]} onSubmitted={submitted}/>);await act(async()=>{});
 fireEvent.click(screen.getByText('Retry decisions'));await waitFor(()=>expect(submitted).toHaveBeenCalledOnce());expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);expect(client.semanticReviewStatus).toHaveBeenCalledTimes(2);
});
