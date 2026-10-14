// @vitest-environment jsdom
import React from 'react';
import {render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {afterEach,it,expect,vi} from 'vitest';
import type {SemanticTargetResource} from '@photoeditor/api-client';
import {APIError} from '@photoeditor/api-client';
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
