// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {reviewDecisionIntent} from './review-decision';
import type {Review} from './catalog-types';

const mocks=vi.hoisted(()=>({review:vi.fn(),decide:vi.fn(),preview:vi.fn(),pair:vi.fn(),replace:vi.fn()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),Image:({accessibilityLabel,source,onLoad,onError}:any)=><img aria-label={accessibilityLabel} src={source?.uri} onLoad={onLoad} onError={onError}/> }));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'33333333-3333-4333-8333-333333333333',tenant:'44444444-4444-4444-8444-444444444444'}),router:{replace:mocks.replace,push:vi.fn()}}));
const auth={origin:'https://restoration-review.test',client:()=>({})};
const storage=new Map<string,string>();
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>storage.get(k)??null,setItem:async(k:string,v:string)=>{storage.set(k,v)},removeItem:async(k:string)=>{storage.delete(k)}}}));
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=async()=>({id:'55555555-5555-4555-8555-555555555555'});review=mocks.review;decide=mocks.decide;runOutputs=vi.fn()}}));
vi.mock('./review-preview',()=>({loadReviewPreview:mocks.preview}));
vi.mock('./review-preview-display',()=>({prepareReviewPreviewPair:mocks.pair,prepareReviewPreviewDisplay:vi.fn()}));
import ReviewDetail from '../screens/reviews/[id]';

const pending:Review={id:'33333333-3333-4333-8333-333333333333',run_id:'66666666-6666-4666-8666-666666666666',kind:'portrait.glare',state:'pending',revision:1,
 candidate_hash:'a'.repeat(64),candidate:{artifact_id:'11111111-1111-4111-8111-111111111111',sha256:'a'.repeat(64)},
 base:{artifact_id:'22222222-2222-4222-8222-222222222222',sha256:'b'.repeat(64)},
 reasons:[],created_at:'2026-10-13T00:00:00Z'};

afterEach(()=>{cleanup();storage.clear();vi.resetAllMocks()});

it('submits a V28 human decision only after both previews are ready',async()=>{
 mocks.review.mockResolvedValue(pending);
 mocks.preview.mockResolvedValue({});
 mocks.pair.mockResolvedValue([{uri:'https://media.test/before',width:10,height:10},{uri:'https://media.test/after',width:10,height:10}]);
 mocks.decide.mockImplementation(async()=>{const intent=reviewDecisionIntent('44444444-4444-4444-8444-444444444444','55555555-5555-4555-8555-555555555555',pending,'accept');const receipt={...pending,state:'decided',revision:2,decision:'accept',decision_route:'candidate',decision_hash:intent.decision_hash};mocks.review.mockResolvedValue(receipt);return receipt});
 render(<ReviewDetail/>);
 await screen.findByText('Before');
 expect(mocks.preview).toHaveBeenCalledTimes(2);
 expect(screen.getByRole('button',{name:'Accept'}).getAttribute('aria-disabled')).toBe('true');
 fireEvent.load(screen.getByLabelText('Review before'));expect(screen.getByRole('button',{name:'Accept'}).getAttribute('aria-disabled')).toBe('true');fireEvent.load(screen.getByLabelText('Review after'));
 fireEvent.click(screen.getByText('Accept'));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(pending,'accept',[],[],expect.any(String),expect.any(AbortSignal)));
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

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
