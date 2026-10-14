// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
import {RunReviews,readRunReviewPage} from './run-reviews';
import type {OwnerAPI} from './owner-api';
import {APIError} from '@photoeditor/api-client';

const run='11111111-1111-4111-8111-111111111111';
const first='33333333-3333-4333-8333-333333333333';
const second='44444444-4444-4444-8444-444444444444';
const review=(id:string,run_id=run)=>({id,run_id,kind:'portrait.open_eyes',state:'pending',revision:1,candidate_hash:'hash',reasons:[],created_at:'2026-10-13T00:00:00Z'});
afterEach(cleanup);

it('rejects a foreign or repeated review and a reused cursor',()=>{
 expect(()=>readRunReviewPage({items:[review(first,second)]},run,new Set(),new Set())).toThrow('RUN_REVIEWS_SCOPE_INVALID');
 expect(()=>readRunReviewPage({items:[review(first)]},run,new Set([first]),new Set())).toThrow('RUN_REVIEWS_SCOPE_INVALID');
 expect(()=>readRunReviewPage({items:[],next_cursor:'used'},run,new Set(),new Set(['used']))).toThrow('RUN_REVIEWS_INVALID');
 expect(()=>readRunReviewPage({items:[review(first)],next_cursor:'current'},run,new Set(),new Set(),'current')).toThrow('RUN_REVIEWS_INVALID');
});
it('refuses non-advancing review pages and oversized cursors without truncating history',()=>{
 expect(()=>readRunReviewPage({items:[],next_cursor:'more'},run,new Set(),new Set())).toThrow('RUN_REVIEWS_INVALID');
 expect(()=>readRunReviewPage({items:[review(first)],next_cursor:'x'.repeat(4097)},run,new Set(),new Set())).toThrow('RUN_REVIEWS_INVALID');
 const prior=new Set(Array.from({length:5000},(_,index)=>`prior-${index}`));
 expect(readRunReviewPage({items:[review(first)],next_cursor:'more'},run,prior,new Set()).items).toHaveLength(1);
});
it('loads paged open-eyes reviews and opens the selected review',async()=>{
 const reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockResolvedValueOnce({items:[review(second)],next_cursor:null});
 const onOpen=vi.fn();
 render(<RunReviews api={{reviewsForRun} as unknown as OwnerAPI} run={run} onOpen={onOpen}/>);
 await screen.findByText('Load more reviews');
 fireEvent.click(screen.getByText('Load more reviews'));
 await waitFor(()=>expect(reviewsForRun).toHaveBeenCalledTimes(2));
 expect(reviewsForRun.mock.calls[1][1]).toBe('page2');
 await waitFor(()=>expect(screen.getAllByText('portrait.open_eyes · pending')).toHaveLength(2));
 fireEvent.click(screen.getAllByText('portrait.open_eyes · pending')[1]);
 expect(onOpen).toHaveBeenCalledWith(second);
});
it('does not append a page that repeats its requested cursor',async()=>{
 const reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockResolvedValueOnce({items:[review(second)],next_cursor:'page2'});
 render(<RunReviews api={{reviewsForRun} as unknown as OwnerAPI} run={run} onOpen={vi.fn()}/>);
 await screen.findByText('Load more reviews');fireEvent.click(screen.getByText('Load more reviews'));
 await screen.findByText('Unable to load reviews. Retry');
 expect(screen.getAllByText('portrait.open_eyes · pending')).toHaveLength(1);
});
it('clears previously displayed reviews when access is revoked during pagination',async()=>{
 const reviewsForRun=vi.fn().mockResolvedValueOnce({items:[review(first)],next_cursor:'page2'}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<RunReviews api={{reviewsForRun} as unknown as OwnerAPI} run={run} onOpen={vi.fn()}/>);
 await screen.findByText('Load more reviews');fireEvent.click(screen.getByText('Load more reviews'));
 await screen.findByText('Unable to load reviews. Retry');
 expect(screen.queryByText('portrait.open_eyes · pending')).toBeNull();
});
