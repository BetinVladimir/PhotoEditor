// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
import {RelightRunReviews,readRelightRunReviewPage} from './relight-run-reviews';
import type {OwnerAPI} from './owner-api';
import {APIError} from '@photoeditor/api-client';

const run='11111111-1111-4111-8111-111111111111',workflow='22222222-2222-4222-8222-222222222222';
const first='33333333-3333-4333-8333-333333333333',second='44444444-4444-4444-8444-444444444444';
const review=(id:string,run_id=run)=>({id,run_id,kind:'relight',state:'pending',revision:1,candidate_hash:'hash',reasons:[],created_at:'2026-10-13T00:00:00Z'});
const status={run_id:run,workflow_id:workflow,revision:'1',state:'waiting_review'};
afterEach(cleanup);

it('rejects foreign and duplicate reviews across cursor pages',()=>{
 expect(()=>readRelightRunReviewPage({items:[review(first,workflow)]},run,new Set(),new Set())).toThrow('RELIGHT_REVIEWS_SCOPE_INVALID');
 expect(()=>readRelightRunReviewPage({items:[review(first)]},run,new Set([first]),new Set())).toThrow('RELIGHT_REVIEWS_SCOPE_INVALID');
 expect(()=>readRelightRunReviewPage({items:[],next_cursor:'used'},run,new Set(),new Set(['used']))).toThrow('RELIGHT_REVIEWS_INVALID');
 expect(()=>readRelightRunReviewPage({items:[review(first)],next_cursor:'current'},run,new Set(),new Set(),'current')).toThrow('RELIGHT_REVIEWS_INVALID');
});
it('refuses non-advancing relight pages and oversized cursors without truncating history',()=>{
 expect(()=>readRelightRunReviewPage({items:[],next_cursor:'more'},run,new Set(),new Set())).toThrow('RELIGHT_REVIEWS_INVALID');
 expect(()=>readRelightRunReviewPage({items:[review(first)],next_cursor:'x'.repeat(4097)},run,new Set(),new Set())).toThrow('RELIGHT_REVIEWS_INVALID');
 const prior=new Set(Array.from({length:5000},(_,index)=>`prior-${index}`));
 expect(readRelightRunReviewPage({items:[review(first)],next_cursor:'more'},run,prior,new Set()).items).toHaveLength(1);
});
it('loads all scoped pages and opens the selected review',async()=>{
 const get=vi.fn().mockResolvedValue(status),reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockResolvedValueOnce({items:[review(second)],next_cursor:null}),onOpen=vi.fn();
 render(<RelightRunReviews api={{get,reviewsForRun} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={onOpen}/>);
 await screen.findByText('Load more reviews');
 fireEvent.click(screen.getByText('Load more reviews'));
 await waitFor(()=>expect(reviewsForRun).toHaveBeenCalledTimes(2));
 expect(reviewsForRun.mock.calls[1][1]).toBe('page2');
 await waitFor(()=>expect(screen.getAllByText('relight · Pending')).toHaveLength(2));
 fireEvent.click(screen.getAllByText('relight · Pending')[1]);expect(onOpen).toHaveBeenCalledWith(second);
});
it('does not append a relight page that repeats its requested cursor',async()=>{
 const get=vi.fn().mockResolvedValue(status),reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockResolvedValueOnce({items:[review(second)],next_cursor:'page2'});
 render(<RelightRunReviews api={{get,reviewsForRun} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);
 await screen.findByText('Load more reviews');fireEvent.click(screen.getByText('Load more reviews'));
 await screen.findByText('Unable to load reviews. Retry');
 expect(screen.getAllByText('relight · Pending')).toHaveLength(1);
});
it('clears previously displayed relight reviews when access is revoked',async()=>{
 const get=vi.fn().mockResolvedValue(status),reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<RelightRunReviews api={{get,reviewsForRun} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);
 await screen.findByText('Load more reviews');fireEvent.click(screen.getByText('Load more reviews'));
 await screen.findByText('Unable to load reviews. Retry');
 expect(screen.queryByText('relight · Pending')).toBeNull();
});
it('does not fetch reviews when workflow identity differs',async()=>{
 const get=vi.fn().mockResolvedValue({...status,workflow_id:run}),reviewsForRun=vi.fn();
 render(<RelightRunReviews api={{get,reviewsForRun} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);
 await screen.findByText('Unable to load reviews. Retry');expect(reviewsForRun).not.toHaveBeenCalled();
});
