// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {Review} from './catalog-types';

const mocks=vi.hoisted(()=>({review:vi.fn(),decide:vi.fn(),preview:vi.fn(),pair:vi.fn(),replace:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>({id:'review-id',tenant:'tenant-id'}),router:{replace:mocks.replace,push:vi.fn()}}));
vi.mock('./auth-context',()=>({useAuth:()=>({client:()=>({})})}));
vi.mock('./owner-api',()=>({OwnerAPI:class {review=mocks.review;decide=mocks.decide;runOutputs=vi.fn()}}));
vi.mock('./review-preview',()=>({loadReviewPreview:mocks.preview}));
vi.mock('./review-preview-display',()=>({prepareReviewPreviewPair:mocks.pair,prepareReviewPreviewDisplay:vi.fn()}));
import ReviewDetail from '../app/reviews/[id]';

const pending:Review={id:'review-id',run_id:'run-id',kind:'portrait.glare',state:'pending',revision:1,
 candidate_hash:'a'.repeat(64),candidate:{artifact_id:'11111111-1111-4111-8111-111111111111',sha256:'a'.repeat(64)},
 base:{artifact_id:'22222222-2222-4222-8222-222222222222',sha256:'b'.repeat(64)},
 reasons:[],created_at:'2026-10-13T00:00:00Z'};

afterEach(()=>{cleanup();vi.resetAllMocks()});

it('submits a V28 human decision only after both previews are ready',async()=>{
 mocks.review.mockResolvedValue(pending);
 mocks.preview.mockResolvedValue({});
 mocks.pair.mockResolvedValue([{uri:'https://media.test/before',width:10,height:10},{uri:'https://media.test/after',width:10,height:10}]);
 mocks.decide.mockResolvedValue({...pending,state:'decided',revision:2});
 render(<ReviewDetail/>);
 await screen.findByText('Before');
 expect(mocks.preview).toHaveBeenCalledTimes(2);
 fireEvent.click(screen.getByText('Accept'));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(pending,'accept',[],[]));
 expect(mocks.replace).toHaveBeenCalled();
});

it('does not expose V28 decision controls when a preview is unavailable',async()=>{
 mocks.review.mockResolvedValue(pending);
 mocks.preview.mockRejectedValue(new Error('preview missing'));
 render(<ReviewDetail/>);
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(screen.queryByText('Accept')).toBeNull();
 expect(mocks.decide).not.toHaveBeenCalled();
});

it('does not expose stale decision controls when the review leaves pending during preview loading',async()=>{
 mocks.review.mockResolvedValueOnce(pending).mockResolvedValueOnce({...pending,state:'decided'});
 mocks.preview.mockResolvedValue({});
 mocks.pair.mockResolvedValue([{uri:'https://media.test/before',width:10,height:10},{uri:'https://media.test/after',width:10,height:10}]);
 render(<ReviewDetail/>);
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(mocks.review).toHaveBeenCalledTimes(2);
 expect(screen.queryByText('Accept')).toBeNull();
 expect(mocks.decide).not.toHaveBeenCalled();
});
