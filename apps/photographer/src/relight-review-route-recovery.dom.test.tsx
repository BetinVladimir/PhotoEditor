// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {relightReviewDecisionIntent} from './relight-review-decision';
import type {Review} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const mocks=vi.hoisted(()=>({review:vi.fn(),decide:vi.fn(),read:vi.fn(),write:vi.fn(),clear:vi.fn(),replace:vi.fn(),selection:vi.fn(),me:vi.fn(),acknowledge:vi.fn()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'00000000-0000-4000-8000-000000000001',tenant:'00000000-0000-4000-8000-000000000005'}),router:{replace:mocks.replace,push:vi.fn()}}));
const auth={origin:'https://photo.invalid',client:()=>({})};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class{review=mocks.review;decide=mocks.decide;me=mocks.me;acknowledgeRelightViewed=mocks.acknowledge;restorationResultSelection=mocks.selection;}}));
vi.mock('./relight-review-decision-attempt-store',()=>({RelightReviewDecisionAttemptStore:class{read=mocks.read;write=mocks.write;clear=mocks.clear;}}));
vi.mock('./review-preview',()=>({loadReviewPreview:async(_api:unknown,_review:unknown,side:string)=>({image:{url:`https://photo.invalid/api/v1/media/review-pending?token=${side}.signed&versionId=v1`}})}));
vi.mock('./review-preview-display',()=>({prepareReviewPreviewPair:async()=>[{uri:'before',width:1,height:1},{uri:'after',width:1,height:1}]}));
vi.mock('./verified-review-comparison',()=>({VerifiedReviewComparison:({onViewed}:{onViewed:(v:boolean)=>void})=><button onClick={()=>onViewed(true)}>Display verified pair</button>}));
import ReviewDetail from '../screens/reviews/[id]';
function fixture(){
 const review:Review={id:id(1),run_id:id(2),kind:'image.relight',state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},reasons:[],created_at:'2026-10-16T00:00:00Z'};
 const body=relightReviewDecisionIntent(id(5),id(6),review,'skip');
 return {review,attempt:{version:1 as const,key:id(7),body},receipt:{...review,state:'decided' as const,revision:2,decision:'skip' as const,decision_route:'skip' as const,decision_hash:body.decision_hash}};
}
beforeEach(()=>{mocks.me.mockResolvedValue({id:id(6)});mocks.acknowledge.mockResolvedValue({version:1,token:'viewed.signed',expires_at:new Date(Date.now()+60000).toISOString()})});
afterEach(()=>{cleanup();vi.resetAllMocks()});
it('restores the saved choice and recovers a lost response before clearing the journal',async()=>{
 const f=fixture();let committed=false;
 mocks.read.mockResolvedValue(f.attempt);mocks.review.mockImplementation(async()=>committed?f.receipt:f.review);
 mocks.decide.mockImplementation(async()=>{committed=true;throw new Error('lost response')});
 render(<ReviewDetail/>);await screen.findByText(/Saved choice: Skip/);
 fireEvent.click(screen.getByText('Display verified pair'));
 fireEvent.click(screen.getByText('Recover saved decision'));
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalled());
 expect(mocks.decide).toHaveBeenCalledTimes(1);
 expect(mocks.decide).toHaveBeenCalledWith(f.review,'skip',[],[],f.attempt.key,expect.any(AbortSignal),'viewed.signed');
 expect(mocks.clear).toHaveBeenCalledWith(f.attempt.key,f.receipt);
 expect(mocks.write).toHaveBeenCalledWith(f.attempt);
});
it('acknowledges an already committed choice after reload without another mutation',async()=>{
 const f=fixture();mocks.read.mockResolvedValue(f.attempt);mocks.review.mockResolvedValue(f.receipt);
 mocks.selection.mockResolvedValue({route:'skip',image:null,decision_hash:f.receipt.decision_hash});
 render(<ReviewDetail/>);await screen.findByText('Result skipped');
 expect(mocks.clear).toHaveBeenCalledWith(f.attempt.key,f.receipt);
 expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.acknowledge).not.toHaveBeenCalled();
});

it('preserves the initial choice when the first durable write fails',async()=>{
 const f=fixture();mocks.review.mockResolvedValue(f.review);mocks.read.mockResolvedValue(null);mocks.write.mockRejectedValue(new Error('disk full'));
 render(<ReviewDetail/>);await screen.findByText('Relighting review');
 fireEvent.click(screen.getByText('Display verified pair'));fireEvent.click(screen.getByText('Skip'));
 await screen.findByText(/Saved choice: Skip/);await screen.findByRole('alert');
 expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Recover saved decision'));
 await waitFor(()=>expect(mocks.write).toHaveBeenCalledTimes(2));
 expect(mocks.write.mock.calls[1][0]).toEqual(mocks.write.mock.calls[0][0]);
});
it('retains the committed receipt when journal acknowledgement fails and retries without mutation',async()=>{
 const f=fixture();let committed=false;mocks.read.mockResolvedValue(f.attempt);
 mocks.review.mockImplementation(async()=>committed?f.receipt:f.review);
 mocks.decide.mockImplementation(async()=>{committed=true;return f.receipt});
 mocks.clear.mockRejectedValueOnce(new Error('disk full')).mockResolvedValue(undefined);
 render(<ReviewDetail/>);await screen.findByText(/Saved choice: Skip/);
 fireEvent.click(screen.getByText('Display verified pair'));fireEvent.click(screen.getByText('Recover saved decision'));
 await screen.findByRole('alert');expect(mocks.replace).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Recover saved decision'));
 await waitFor(()=>expect(mocks.replace).toHaveBeenCalled());expect(mocks.decide).toHaveBeenCalledTimes(1);
});
it('refuses an account change before sending the saved decision',async()=>{
 const f=fixture();mocks.read.mockResolvedValue(f.attempt);mocks.review.mockResolvedValue(f.review);
 render(<ReviewDetail/>);await screen.findByText(/Saved choice: Skip/);
 mocks.me.mockResolvedValue({id:id(9)});
 fireEvent.click(screen.getByText('Display verified pair'));fireEvent.click(screen.getByText('Recover saved decision'));
 await screen.findByRole('alert');expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.clear).not.toHaveBeenCalled();
});
it('does not mutate after unmount during the journal read',async()=>{
 const f=fixture();mocks.read.mockResolvedValueOnce(f.attempt);mocks.review.mockResolvedValue(f.review);
 let release!:(value:typeof f.attempt)=>void;const blocked=new Promise<typeof f.attempt>(resolve=>{release=resolve});
 mocks.read.mockReturnValueOnce(blocked);
 const view=render(<ReviewDetail/>);await screen.findByText(/Saved choice: Skip/);
 fireEvent.click(screen.getByText('Display verified pair'));fireEvent.click(screen.getByText('Recover saved decision'));
 await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(2));view.unmount();
 await act(async()=>{release(f.attempt);await blocked});
 expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.clear).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});

it('does not send a decision when server acknowledgement fails',async()=>{
 const f=fixture();mocks.read.mockResolvedValue(f.attempt);mocks.review.mockResolvedValue(f.review);mocks.acknowledge.mockRejectedValue(new Error('preview changed'));
 render(<ReviewDetail/>);await screen.findByText(/Saved choice: Skip/);
 fireEvent.click(screen.getByText('Display verified pair'));fireEvent.click(screen.getByText('Recover saved decision'));
 await screen.findByRole('alert');expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.clear).not.toHaveBeenCalled();
 expect(mocks.acknowledge).toHaveBeenCalledWith(f.review,'before.signed','after.signed',expect.any(AbortSignal));
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
