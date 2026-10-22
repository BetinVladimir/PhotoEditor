// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {ComponentType} from 'react';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
import {RunReviews} from './run-reviews';
import {RelightRunReviews} from './relight-run-reviews';
const run='11111111-1111-4111-8111-111111111111',workflow='22222222-2222-4222-8222-222222222222',first='33333333-3333-4333-8333-333333333333',second='44444444-4444-4444-8444-444444444444';
const item=(id:string)=>({id,run_id:run,kind:id===first?'first':'second',state:'pending'}),cases=[['run',RunReviews,8000],['relight',RelightRunReviews,15000]] as const;
afterEach(()=>{cleanup();vi.useRealTimers();});
it.each(cases)('%s review pages retain earlier rows and retry the exact failed cursor',async(_name,Action)=>{
 const reviewsForRun=vi.fn().mockResolvedValueOnce({items:[item(first)],next_cursor:'page2'}).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[item(second)],next_cursor:null}),get=vi.fn().mockResolvedValue({run_id:run,workflow_id:workflow,revision:'1',state:'waiting_review'}),Component=Action as ComponentType<any>;
 render(<Component api={{reviewsForRun,get} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);fireEvent.click(await screen.findByText('Load more reviews'));fireEvent.click(await screen.findByText('Unable to load reviews. Retry'));await screen.findByText('second · Pending');expect(screen.getByText('first · Pending')).toBeTruthy();expect(reviewsForRun.mock.calls.map(call=>call[1])).toEqual([undefined,'page2','page2']);
});
it.each(cases)('%s review pages refuse cancelled late rows and retain the exact continuation',async(_name,Action)=>{
 let finish!:(value:unknown)=>void;const reviewsForRun=vi.fn().mockResolvedValueOnce({items:[item(first)],next_cursor:'page2'}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({items:[item(second)],next_cursor:null}),get=vi.fn().mockResolvedValue({run_id:run,workflow_id:workflow,revision:'1',state:'waiting_review'}),Component=Action as ComponentType<any>;
 render(<Component api={{reviewsForRun,get} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);fireEvent.click(await screen.findByText('Load more reviews'));await waitFor(()=>expect(reviewsForRun).toHaveBeenCalledTimes(2));fireEvent.click(screen.getByText('Cancel loading reviews'));await act(async()=>finish({items:[item(second)],next_cursor:null}));expect(screen.queryByText('second · Pending')).toBeNull();
 fireEvent.click(screen.getByText('Unable to load reviews. Retry'));await screen.findByText('second · Pending');expect(reviewsForRun.mock.calls.map(call=>call[1])).toEqual([undefined,'page2','page2']);
});
it.each(cases)('%s review pages bound a noncooperative initial request',async(_name,Action,deadline)=>{
 vi.useFakeTimers();const reviewsForRun=vi.fn(()=>new Promise<never>(()=>{})),get=vi.fn().mockResolvedValue({run_id:run,workflow_id:workflow,revision:'1',state:'waiting_review'}),Component=Action as ComponentType<any>;
 render(<Component api={{reviewsForRun,get} as unknown as OwnerAPI} run={run} workflow={workflow} onOpen={vi.fn()}/>);await act(async()=>{});await act(async()=>vi.advanceTimersByTimeAsync(deadline));expect(screen.getByText('Unable to load reviews. Retry')).toBeTruthy();expect(screen.queryByText('Cancel loading reviews')).toBeNull();
});
