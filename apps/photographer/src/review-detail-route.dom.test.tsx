// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {Review} from './catalog-types';

const mocks=vi.hoisted(()=>({review:vi.fn(),runOutputs:vi.fn(),renderResultSelection:vi.fn(),renderSelectedPreview:vi.fn(),restorationResultSelection:vi.fn(),restorationSelectedPreview:vi.fn(),reviewPreview:vi.fn(),decide:vi.fn()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./review-decision-attempt-store',()=>({ReviewDecisionAttemptStore:class{read=async()=>null;write=async()=>{};clear=async()=>{};}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'00000000-0000-4000-8000-000000000001',tenant:'00000000-0000-4000-8000-000000000005'}),router:{replace:vi.fn(),push:vi.fn()}}));
let auth={origin:'https://detail.test',client:()=>({})};
const lifecycle={active:true,generation:0};
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {
 me=async()=>({id:'00000000-0000-4000-8000-000000000006'});
 decide=mocks.decide;
 review=mocks.review;
 runOutputs=mocks.runOutputs;
 renderResultSelection=mocks.renderResultSelection;
 renderSelectedPreview=mocks.renderSelectedPreview;
 restorationResultSelection=mocks.restorationResultSelection;
 restorationSelectedPreview=mocks.restorationSelectedPreview;
 reviewPreview=mocks.reviewPreview;
}}));
import ReviewDetail from '../screens/reviews/[id]';

const decidedRender=(decision:'reject'|'skip'):Review=>({
 id:'00000000-0000-4000-8000-000000000001',run_id:'00000000-0000-4000-8000-000000000002',kind:'render.compose',state:'decided',revision:2,
 candidate_hash:'c'.repeat(64),decision,decision_route:decision==='reject'?'base':'skip',decision_hash:'b'.repeat(64),
 reasons:[],candidate_ids:[],created_at:'2026-10-13T00:00:00Z',
 candidate:{artifact_id:'11111111-1111-4111-8111-111111111111',sha256:'c'.repeat(64)},
 base:{artifact_id:'22222222-2222-4222-8222-222222222222',sha256:'d'.repeat(64)},
});

afterEach(()=>{cleanup();lifecycle.active=true;lifecycle.generation=0;vi.resetAllMocks()});

it('shows a skipped render selection without loading generic outputs or a selected preview',async()=>{
 const review=decidedRender('skip');
 mocks.review.mockResolvedValue(review);
 mocks.renderResultSelection.mockResolvedValue({schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'skip',image:null});
 render(<ReviewDetail/>);
 await screen.findByText('Result skipped');
 expect(mocks.runOutputs).not.toHaveBeenCalled();
 expect(mocks.renderSelectedPreview).not.toHaveBeenCalled();
 expect(screen.queryByText('Canonical run artifacts')).toBeNull();
});

it('shows the decided open-eyes skip through the bound selection route',async()=>{
 const review={...decidedRender('skip'),kind:'portrait.open_eyes'};
 mocks.review.mockResolvedValue(review);
 mocks.restorationResultSelection.mockResolvedValue({schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'skip',image:null});
 render(<ReviewDetail/>);
 await screen.findByText('Result skipped');
 expect(screen.getByText(/Review the verified before\/after pair/)).toBeTruthy();
 expect(mocks.restorationResultSelection).toHaveBeenCalledWith(review,expect.any(AbortSignal));
 expect(mocks.restorationSelectedPreview).not.toHaveBeenCalled();
 expect(mocks.runOutputs).not.toHaveBeenCalled();
});

it('keeps the decided base result visible when its selected preview is unavailable',async()=>{
 const review=decidedRender('reject');
 mocks.review.mockResolvedValue(review);
 mocks.renderResultSelection.mockResolvedValue({schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'base',image:review.base});
 mocks.renderSelectedPreview.mockRejectedValue(new Error('preview unavailable'));
 render(<ReviewDetail/>);
 await screen.findByText('Original retained');
 await waitFor(()=>expect(mocks.renderSelectedPreview).toHaveBeenCalledWith(review,expect.any(AbortSignal)));
 expect(mocks.renderResultSelection).toHaveBeenCalledTimes(2);
 expect(mocks.runOutputs).not.toHaveBeenCalled();
 expect(screen.queryByText('Canonical run artifacts')).toBeNull();
});

it('clears a decided render receipt when selection authority is revoked after preview failure',async()=>{
 const review=decidedRender('reject');
 mocks.review.mockResolvedValue(review);
 mocks.renderResultSelection.mockResolvedValueOnce({schema_version:1,review_id:review.id,decision_hash:review.decision_hash,route:'base',image:review.base});
 mocks.renderResultSelection.mockRejectedValueOnce(new Error('access revoked'));
 mocks.renderSelectedPreview.mockRejectedValue(new Error('preview unavailable'));
 render(<ReviewDetail/>);
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(mocks.renderResultSelection).toHaveBeenCalledTimes(2);
 expect(screen.queryByText('Original retained')).toBeNull();
 expect(mocks.runOutputs).not.toHaveBeenCalled();
});

it('retains canonical output loading for an ordinary review',async()=>{
 const review:Review={...decidedRender('skip'),kind:'generic.quality',state:'pending',revision:1,decision:undefined,decision_route:undefined,decision_hash:undefined};
 const output={artifact_id:'33333333-3333-4333-8333-333333333333',role:'quality.report',sha256:'e'.repeat(64),bytes:123,media_type:'application/json',url:'https://example.invalid/report',expires_at:new Date(Date.now()+60000).toISOString()};
 mocks.review.mockResolvedValue(review);
 mocks.runOutputs.mockResolvedValue({items:[output]});
 render(<ReviewDetail/>);
 await screen.findByText('Canonical run artifacts');
 expect(screen.getByText('quality.report')).toBeTruthy();
 expect(mocks.runOutputs).toHaveBeenCalledWith(review.run_id,expect.any(AbortSignal));
 expect(mocks.renderResultSelection).not.toHaveBeenCalled();
 expect(mocks.renderSelectedPreview).not.toHaveBeenCalled();
});

it('requests both V28 restoration previews before allowing a decision',async()=>{
 const review:Review={...decidedRender('skip'),kind:'portrait.glare',state:'pending',revision:1,candidate_hash:'c'.repeat(64),decision:undefined,decision_route:undefined,decision_hash:undefined};
 mocks.review.mockResolvedValue(review);
 mocks.reviewPreview.mockRejectedValue(new Error('four-task preview is not ready'));
 render(<ReviewDetail/>);
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(mocks.reviewPreview).toHaveBeenCalledTimes(2);
 expect(mocks.reviewPreview.mock.calls.map(call=>call[1]).sort()).toEqual(['after','before']);
 expect(mocks.runOutputs).not.toHaveBeenCalled();
});


it('removes review authority in background and reads again on foreground',async()=>{
 const review={...decidedRender('skip'),kind:'generic.quality',state:'pending'};
 mocks.review.mockResolvedValue(review);mocks.runOutputs.mockResolvedValue({items:[]});
 const view=render(<ReviewDetail/>);await screen.findByText('generic.quality review');
 lifecycle.active=false;view.rerender(<ReviewDetail/>);
 expect(screen.queryByText('generic.quality review')).toBeNull();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<ReviewDetail/>);
 await screen.findByText('generic.quality review');expect(mocks.review).toHaveBeenCalledTimes(2);
});

it('unmounts old review authority when auth identity changes',async()=>{
 mocks.review.mockResolvedValue({...decidedRender('skip'),kind:'generic.quality',state:'pending'});
 mocks.runOutputs.mockResolvedValue({items:[]});
 const view=render(<ReviewDetail/>);await screen.findByText('generic.quality review');
 auth={origin:'https://detail.test',client:()=>({})};mocks.review.mockRejectedValue(new APIError(403,'FORBIDDEN','revoked'));
 view.rerender(<ReviewDetail/>);
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(screen.queryByText('generic.quality review')).toBeNull();
});

it.each([401,403,404,410])('removes media and decisions after mutation access loss %s',async status=>{
 mocks.review.mockResolvedValue({...decidedRender('skip'),kind:'generic.quality',state:'pending'});
 mocks.runOutputs.mockResolvedValue({items:[]});mocks.decide.mockRejectedValue(new APIError(status,'FORBIDDEN','revoked'));
 render(<ReviewDetail/>);await screen.findByText('generic.quality review');
 fireEvent.click(screen.getByText('Accept'));
 await screen.findByText('Review or artifacts are unavailable; access may have changed.');
 expect(screen.queryByText('generic.quality review')).toBeNull();expect(screen.queryByText('Accept')).toBeNull();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
