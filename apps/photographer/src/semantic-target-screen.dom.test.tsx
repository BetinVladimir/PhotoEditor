// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import React from 'react';
import {act,render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
import type {SemanticTargetResource} from '@photoeditor/api-client';
import {APIError} from '@photoeditor/api-client';
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:true,actor:'actor'})}));
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticTargetScreen} from './semantic-target-screen';
const id='11111111-1111-4111-8111-111111111111';
const item:SemanticTargetResource={id,revision:'3',document:null,data:{resource_type:'semantic_targets',id,tenant_id:id,project_id:id,created_at:'2026-10-04T12:00:00Z',revision:'3',updated_at:'2026-10-04T12:00:00Z',intent_id:id,child_run_id:id,batch_id:id,asset_version_id:id,candidate_artifact_id:null,review_id:null,override_id:null,state:'applied'}};
afterEach(cleanup);
it('refreshes initial empty targets when original parent becomes reviewable',async()=>{
 let finish!:(value:unknown)=>void;
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[],next_cursor:null}).mockResolvedValue({items:[{...item,data:{...item.data,state:'proposed'}}],next_cursor:null});
 const semanticReviewStatus=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
 render(<SemanticTargetScreen client={{semanticTargets,semanticReviewStatus}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(semanticTargets).toHaveBeenCalledTimes(1));
 finish({run_id:id,workflow_id:id,revision:'4',state:'waiting_review',plan_sha:null,remaining:1,can_close:false});
 await waitFor(()=>expect(screen.getByText('Ready for review')).toBeTruthy());
 expect(semanticTargets).toHaveBeenCalledTimes(2);
});
it('removes loaded targets when parent status loses access',async()=>{
 let deny!:(reason:unknown)=>void;
 const semanticTargets=vi.fn().mockResolvedValue({items:[item],next_cursor:null});
 const semanticReviewStatus=vi.fn().mockImplementation(()=>new Promise((_resolve,reject)=>{deny=reject}));
 render(<SemanticTargetScreen client={{semanticTargets,semanticReviewStatus}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 deny(new APIError(403,'ACCESS_DENIED'));
 await waitFor(()=>expect(screen.queryByText('Applied')).toBeNull());
 expect(screen.getByText(/Unable to load edit results/)).toBeTruthy();
});
it('shows completion and clears results when refresh loses access',async()=>{
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[item],next_cursor:null}).mockRejectedValueOnce(new Error('ACCESS_REVOKED'));
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Refresh'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Refresh'}));
 await waitFor(()=>expect(screen.getByText(/Unable to load edit results/)).toBeTruthy());
 expect(screen.queryByText('Applied')).toBeNull();
});
it('cancels an outstanding request on unmount',async()=>{
 const semanticTargets=vi.fn().mockImplementation(()=>new Promise(()=>{}));
 const mounted=render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(semanticTargets).toHaveBeenCalledOnce());
 const signal=semanticTargets.mock.calls[0][2].signal as AbortSignal;
 mounted.unmount();expect(signal.aborted).toBe(true);
});
it('merges overlapping pages without regressing a target state or revision',async()=>{
 const other='22222222-2222-4222-8222-222222222222';
 const stale={...item,revision:'2',data:{...item.data,revision:'2',state:'queued' as const}};
 const next={...item,id:other,data:{...item.data,id:other,state:'failed' as const}};
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[item],next_cursor:'signed+cursor'}).mockResolvedValueOnce({items:[stale,next],next_cursor:null});
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Load more')).toBeTruthy());
 fireEvent.click(screen.getByText('Load more'));
 await waitFor(()=>expect(screen.getByText('Processing failed')).toBeTruthy());
 expect(screen.getAllByText('Applied')).toHaveLength(1);expect(screen.queryByText('Processing')).toBeNull();expect(screen.queryByText('Load more')).toBeNull();
 expect(semanticTargets.mock.calls[1][2].cursor).toBe('signed+cursor');
});
it('clears earlier pages when continuation loses authorization',async()=>{
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[item],next_cursor:'cursor'}).mockRejectedValueOnce(new Error('FORBIDDEN'));
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Load more')).toBeTruthy());fireEvent.click(screen.getByText('Load more'));
 await waitFor(()=>expect(screen.getByText(/Unable to load edit results/)).toBeTruthy());expect(screen.queryByText('Applied')).toBeNull();expect(screen.queryByText('Load more')).toBeNull();
});
it('serializes duplicate continuation clicks while the page is pending',async()=>{
 let complete!:(value:unknown)=>void;
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[item],next_cursor:'cursor'}).mockImplementationOnce(()=>new Promise(resolve=>{complete=resolve}));
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Load more')).toBeTruthy());const button=screen.getByText('Load more');fireEvent.click(button);fireEvent.click(button);
 expect(semanticTargets).toHaveBeenCalledTimes(2);
 complete({items:[],next_cursor:null});await waitFor(()=>expect(screen.queryByText('Load more')).toBeNull());
});

it('refreshes completed processing after an in-flight continuation instead of dropping the notification',async()=>{
 let finishPage!:(value:unknown)=>void,finishChild!:(value:unknown)=>void;
 const queued={...item,data:{...item.data,state:'queued' as const}};
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[queued],next_cursor:'cursor'}).mockImplementationOnce(()=>new Promise(resolve=>{finishPage=resolve})).mockResolvedValueOnce({items:[item],next_cursor:null});
 const semanticChildStatus=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finishChild=resolve}));
 render(<SemanticTargetScreen client={{semanticTargets,semanticChildStatus}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Load more')).toBeTruthy());
 fireEvent.click(screen.getByText('Load more'));
 finishChild({id,kind:'apply',status:'completed',stage:'completed',revision:'4',counts:{total:1,succeeded:1,failed:0,skipped:0,review:0},progress:1,errors:[]});
 await waitFor(()=>expect(screen.getByText('Processing: Completed · 100%')).toBeTruthy());
 expect(semanticTargets).toHaveBeenCalledTimes(2);
 finishPage({items:[],next_cursor:null});
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 expect(semanticTargets).toHaveBeenCalledTimes(3);
 expect(semanticTargets.mock.calls[2][2].cursor).toBeUndefined();
});

it('drops old results immediately when authenticated client changes and replacement fails',async()=>{const first={semanticTargets:vi.fn().mockResolvedValue({items:[item],next_cursor:null})},next={semanticTargets:vi.fn().mockRejectedValue(new Error('DENIED'))};const view=render(<SemanticTargetScreen client={first} tenant={id} intent={id}/>);await screen.findByText('Applied');view.rerender(<SemanticTargetScreen client={next} tenant={id} intent={id}/>);expect(screen.queryByText('Applied')).toBeNull();await screen.findByRole('alert');expect(screen.queryByText('Applied')).toBeNull();});

it.each(['self','empty','cycle','oversized'] as const)('refuses %s pagination continuation and clears exposed rows',async kind=>{const semanticTargets=vi.fn().mockResolvedValueOnce({items:[item],next_cursor:'A'}).mockResolvedValueOnce({items:kind==='empty'?[]:[item],next_cursor:kind==='cycle'?'B':kind==='oversized'?'x'.repeat(2049):'A'}).mockResolvedValueOnce({items:[item],next_cursor:'A'});render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);const more=await screen.findByRole('button',{name:'Load more'});await waitFor(()=>expect(more.hasAttribute('disabled')).toBe(false));fireEvent.click(more);if(kind==='cycle'){await waitFor(()=>expect(semanticTargets).toHaveBeenCalledTimes(2));await waitFor(()=>expect(screen.getByRole('button',{name:'Load more'}).hasAttribute('disabled')).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Load more'}));}await screen.findByRole('alert');expect(screen.queryByText('Applied')).toBeNull();expect(screen.queryByText('Load more')).toBeNull();expect(semanticTargets).toHaveBeenCalledTimes(kind==='cycle'?3:2);});
it('limits initial child polling to mounted cells for5000 queued targets and aborts them on disposal',async()=>{const items=Array.from({length:5000},(_,i)=>{const target=`00000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`;return {...item,id:target,data:{...item.data,id:target,asset_version_id:target,child_run_id:target,state:'queued' as const}};});const semanticTargets=vi.fn().mockResolvedValue({items,next_cursor:null}),semanticChildStatus=vi.fn().mockImplementation(()=>new Promise(()=>{}));const view=render(<SemanticTargetScreen client={{semanticTargets,semanticChildStatus}} tenant={id} intent={id}/>);await waitFor(()=>expect(semanticChildStatus.mock.calls.length).toBeGreaterThan(0));expect(semanticChildStatus.mock.calls.length).toBeLessThanOrEqual(50);expect(screen.getAllByText('Processing').length).toBeLessThanOrEqual(50);view.unmount();expect(semanticChildStatus.mock.calls.every(call=>(call[2] as AbortSignal).aborted)).toBe(true);});
it('shares one child request across mounted targets from the same Apply batch',async()=>{const items=Array.from({length:3},(_,i)=>{const target=`00000000-0000-4000-8000-${String(i+1).padStart(12,'0')}`;return {...item,id:target,data:{...item.data,id:target,state:'queued' as const,child_run_id:id}};});const semanticTargets=vi.fn().mockResolvedValue({items,next_cursor:null}),semanticChildStatus=vi.fn().mockImplementation(()=>new Promise(()=>{}));const view=render(<SemanticTargetScreen client={{semanticTargets,semanticChildStatus}} tenant={id} intent={id}/>);await screen.findAllByText('Processing');await waitFor(()=>expect(screen.getAllByText('Processing')).toHaveLength(3));expect(semanticChildStatus).toHaveBeenCalledTimes(1);view.unmount();expect((semanticChildStatus.mock.calls[0][2] as AbortSignal).aborted).toBe(true);});
it('opens accepted semantic version with its asset and never exposes that action for proposed target',async()=>{
 const version='22222222-2222-4222-8222-222222222222',asset='33333333-3333-4333-8333-333333333333';
 const semanticTargets=vi.fn().mockResolvedValue({items:[item],next_cursor:null}),materializeSemanticShownVersion=vi.fn().mockResolvedValue({asset_version_id:version,asset_id:asset}),onOpenVersion=vi.fn();
 const view=render(<SemanticTargetScreen client={{semanticTargets,materializeSemanticShownVersion}} tenant={id} intent={id} onOpenVersion={onOpenVersion}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Open edited photo'}));await waitFor(()=>expect(onOpenVersion).toHaveBeenCalledWith(version,asset,id));
 view.unmount();semanticTargets.mockResolvedValue({items:[{...item,data:{...item.data,state:'proposed'}}],next_cursor:null});
 render(<SemanticTargetScreen client={{semanticTargets,materializeSemanticShownVersion}} tenant={id} intent={id} onOpenVersion={onOpenVersion}/>);
 await waitFor(()=>expect(semanticTargets).toHaveBeenCalledTimes(2));expect(screen.queryByRole('button',{name:'Open edited photo'})).toBeNull();
 expect(materializeSemanticShownVersion).toHaveBeenCalledTimes(1);
});

it('keeps child processing visible after parent review is completed',async()=>{
 let finishChild!:(value:unknown)=>void;
 const queued={...item,data:{...item.data,state:'queued' as const}};
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[queued],next_cursor:null}).mockResolvedValue({items:[item],next_cursor:null});
 const semanticReviewStatus=vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'4',state:'completed',plan_sha:null,remaining:0,can_close:false});
 const semanticChildStatus=vi.fn().mockImplementation(()=>new Promise(resolve=>{finishChild=resolve}));
 render(<SemanticTargetScreen client={{semanticTargets,semanticReviewStatus,semanticChildStatus}} tenant={id} intent={id}/>);
 await waitFor(()=>expect(screen.getByText('Batch status: Completed')).toBeTruthy());
 await waitFor(()=>expect(semanticChildStatus).toHaveBeenCalledOnce());
 expect(screen.getByText('Processing')).toBeTruthy();expect(screen.queryByText('Applied')).toBeNull();
 finishChild({id,kind:'apply',status:'completed',stage:'completed',revision:'4',counts:{total:1,succeeded:1,failed:0,skipped:0,review:0},progress:1,errors:[]});
 await waitFor(()=>expect(screen.getByText('Applied')).toBeTruthy());
 expect(screen.getByText('Batch status: Completed')).toBeTruthy();
 expect(semanticTargets).toHaveBeenCalledTimes(2);
});

it('keeps close disabled during journal boot and enables it after an empty journal loads',async()=>{
 let finish!:(value:null)=>void;
 const store={assertScope:vi.fn(),read:vi.fn(()=>new Promise<null>(resolve=>{finish=resolve})),write:vi.fn(),clear:vi.fn()} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticTargets:vi.fn().mockResolvedValue({items:[],next_cursor:null}),semanticReviewStatus:vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'9',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:0,can_close:true}),applySemanticReview:vi.fn(),closeSemanticReview:vi.fn()};
 render(<SemanticTargetScreen client={client} tenant={id} intent={id} journalFactory={async()=>store}/>);
 const close=await screen.findByRole('button',{name:'Close review'});
 expect(close.hasAttribute('disabled')).toBe(true);fireEvent.click(close);expect(client.closeSemanticReview).not.toHaveBeenCalled();
 await act(async()=>finish(null));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Close review'}).hasAttribute('disabled')).toBe(false));
});
it('keeps close blocked after journal read failure',async()=>{
 const store={assertScope:vi.fn(),read:vi.fn().mockRejectedValue(new Error('unavailable')),write:vi.fn(),clear:vi.fn()} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticTargets:vi.fn().mockResolvedValue({items:[],next_cursor:null}),semanticReviewStatus:vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'9',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:0,can_close:true}),applySemanticReview:vi.fn(),closeSemanticReview:vi.fn()};
 render(<SemanticTargetScreen client={client} tenant={id} intent={id} journalFactory={async()=>store}/>);
 await screen.findByRole('button',{name:'Reload saved decisions'});
 const close=await screen.findByRole('button',{name:'Close review'});expect(close.hasAttribute('disabled')).toBe(true);
 fireEvent.click(close);expect(client.closeSemanticReview).not.toHaveBeenCalled();
});
it('blocks close until a recovered uncertain decision is acknowledged and cleared',async()=>{
 const key='22222222-2222-4222-8222-222222222222';
 const body={expected_revision:'8',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id,decision:'reject' as const}]};
 const saved={version:1 as const,key,workflow:id,body};
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockResolvedValue(undefined)} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const client={semanticTargets:vi.fn().mockResolvedValue({items:[],next_cursor:null}),semanticReviewStatus:vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'9',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:0,can_close:true}),applySemanticReview:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({workflow_id:id,revision:'9',remaining:0,batch_id:key,child:null}),closeSemanticReview:vi.fn()};
 render(<SemanticTargetScreen client={client} tenant={id} intent={id} journalFactory={async()=>store}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});
 await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 expect(screen.getByRole('button',{name:'Close review'}).hasAttribute('disabled')).toBe(true);
 fireEvent.click(retry);
 await waitFor(()=>expect(client.applySemanticReview).toHaveBeenCalledOnce());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
 expect(screen.getByRole('button',{name:'Close review'}).hasAttribute('disabled')).toBe(true);
 expect(store.clear).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:'Retry decisions'}));
 await waitFor(()=>expect(store.clear).toHaveBeenCalledOnce());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Close review'}).hasAttribute('disabled')).toBe(false));
 expect(client.applySemanticReview.mock.calls[0].slice(0,4)).toEqual([id,id,body,key]);
 expect(client.applySemanticReview.mock.calls[1].slice(0,4)).toEqual([id,id,body,key]);
 expect(client.closeSemanticReview).not.toHaveBeenCalled();
});


it('replays persisted approval, finishes failed cleanup, refreshes targets and opens exact accepted result',async()=>{
 const target='22222222-2222-4222-8222-222222222222',version='33333333-3333-4333-8333-333333333333',asset='44444444-4444-4444-8444-444444444444',project='55555555-5555-4555-8555-555555555555';
 const body={expected_revision:'9',plan_sha:'a'.repeat(64),target_decisions:[{target_id:target,decision:'approve' as const,preview:{render:{artifact_id:version,sha256:'b'.repeat(64)},evidence:{artifact_id:asset,sha256:'c'.repeat(64)}}}]};
 const saved={version:1 as const,key:target,workflow:id,body};
 const store={assertScope:vi.fn(),read:vi.fn().mockResolvedValue(saved),write:vi.fn().mockResolvedValue(undefined),clear:vi.fn().mockRejectedValueOnce(new Error('delete failed')).mockResolvedValueOnce(undefined)} as unknown as import('./semantic-apply-attempt-store').SemanticApplyAttemptStore;
 const applied={...item,id:target,data:{...item.data,id:target,project_id:project}};
 const semanticTargets=vi.fn().mockResolvedValueOnce({items:[],next_cursor:null}).mockResolvedValue({items:[applied],next_cursor:null});
 const semanticReviewStatus=vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'10',state:'completed',plan_sha:null,remaining:0,can_close:false});
 const applySemanticReview=vi.fn().mockResolvedValue({workflow_id:id,revision:'10',remaining:0,batch_id:target,child:{run_id:version,state:'queued'}});
 const materializeSemanticShownVersion=vi.fn().mockResolvedValue({asset_version_id:version,asset_id:asset}),onOpenVersion=vi.fn();
 render(<SemanticTargetScreen client={{semanticTargets,semanticReviewStatus,applySemanticReview,materializeSemanticShownVersion}} tenant={id} intent={id} journalFactory={async()=>store} onOpenVersion={onOpenVersion}/>);
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));fireEvent.click(retry);
 await waitFor(()=>expect(store.clear).toHaveBeenCalledTimes(1));await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 expect(screen.queryByRole('button',{name:'Open edited photo'})).toBeNull();
 fireEvent.click(retry);await clickWhenEnabled(await screen.findByRole('button',{name:'Open edited photo'}));
 await waitFor(()=>expect(onOpenVersion).toHaveBeenCalledWith(version,asset,project));
 expect(applySemanticReview).toHaveBeenCalledTimes(1);expect(applySemanticReview.mock.calls[0].slice(0,4)).toEqual([id,id,body,target]);
 expect(store.write).toHaveBeenCalledTimes(1);expect(store.clear).toHaveBeenCalledTimes(2);
 expect(materializeSemanticShownVersion.mock.calls[0].slice(0,2)).toEqual([id,target]);
});


it('keeps a non-journal uncertain decision mounted while target refresh removes proposals',async()=>{
 let proposed=true;
 const semanticTargets=vi.fn().mockImplementation(async()=>({items:proposed?[{...item,data:{...item.data,state:'proposed'}}]:[],next_cursor:null}));
 const semanticReviewStatus=vi.fn().mockResolvedValue({run_id:id,workflow_id:id,revision:'9',state:'waiting_review',plan_sha:'a'.repeat(64),remaining:1,can_close:false});
 const applySemanticReview=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({workflow_id:id,revision:'10',remaining:0,batch_id:id,child:null});
 render(<SemanticTargetScreen client={{semanticTargets,semanticReviewStatus,applySemanticReview}} tenant={id} intent={id}/>);
 fireEvent.click(await screen.findByLabelText('reject '+id));fireEvent.click(screen.getByText('Submit decisions'));
 const retry=await screen.findByRole('button',{name:'Retry decisions'});await waitFor(()=>expect(retry.hasAttribute('disabled')).toBe(false));
 const original=applySemanticReview.mock.calls[0].slice(0,4);proposed=false;
 fireEvent.click(screen.getByRole('button',{name:'Refresh'}));await waitFor(()=>expect(screen.queryByLabelText('reject '+id)).toBeNull());
 await waitFor(()=>expect(screen.getByRole('button',{name:'Retry decisions'}).hasAttribute('disabled')).toBe(false));
 fireEvent.click(screen.getByRole('button',{name:'Retry decisions'}));await waitFor(()=>expect(applySemanticReview).toHaveBeenCalledTimes(2));
 expect(applySemanticReview.mock.calls[1].slice(0,4)).toEqual(original);
});

it('cancelled target read can refresh before the ignored-abort old result settles',async()=>{
 let finish!:(value:{items:SemanticTargetResource[];next_cursor:null})=>void;
 const semanticTargets=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({items:[],next_cursor:null});
 render(<SemanticTargetScreen client={{semanticTargets}} tenant={id} intent={id}/>);await waitFor(()=>expect(semanticTargets).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByRole('button',{name:'Refresh'}));await waitFor(()=>expect(semanticTargets).toHaveBeenCalledTimes(2));
 await act(async()=>finish({items:[item],next_cursor:null}));expect(screen.queryByText('Applied')).toBeNull();
});
