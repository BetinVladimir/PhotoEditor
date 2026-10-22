// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {reviewDecisionIntent} from './review-decision';
import type {Review} from './catalog-types';
const mocks=vi.hoisted(()=>({review:vi.fn(),decide:vi.fn(),read:vi.fn(),write:vi.fn(),clear:vi.fn(),replace:vi.fn(),me:vi.fn()}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:'00000000-0000-4000-8000-000000000001',tenant:'00000000-0000-4000-8000-000000000005'}),router:{replace:mocks.replace,push:vi.fn()}}));
const auth={origin:'https://photo.invalid',client:()=>({})};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:1})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{review=mocks.review;decide=mocks.decide;me=mocks.me;runOutputs=async()=>({items:[]});}}));
vi.mock('./review-decision-attempt-store',()=>({ReviewDecisionAttemptStore:class{read=mocks.read;write=mocks.write;clear=mocks.clear;}}));
import ReviewDetail from '../screens/reviews/[id]';
function fixture(kind='defects'){
 const candidate={artifact_id:id(3),sha256:'a'.repeat(64)},base={artifact_id:id(4),sha256:'b'.repeat(64)};
 const review:Review={id:id(1),run_id:id(2),kind,state:'pending',revision:1,candidate_hash:candidate.sha256,proposal:candidate,image:base,base,label_map:{artifact_id:id(10),sha256:'c'.repeat(64)},candidate_ids:['d'.repeat(64),'e'.repeat(64)],reasons:[],created_at:'2026-10-17T00:00:00Z'};
 const body=reviewDecisionIntent(id(5),id(6),review,'accept',[review.candidate_ids![0]],[review.candidate_ids![1]]);
 return {review,attempt:{version:1 as const,key:id(7),body},receipt:{...review,state:'decided',revision:2,decision:'accept',decision_route:'repair',decision_hash:body.decision_hash}};
}
beforeEach(()=>{mocks.me.mockResolvedValue({id:id(6)});mocks.write.mockResolvedValue(undefined);mocks.clear.mockResolvedValue(undefined)});
afterEach(()=>{cleanup();vi.useRealTimers();vi.resetAllMocks()});
it.each(['defects','sensor_dust'])('recovers %s saved subset after ambiguous response without replacing intent',async kind=>{
 const f=fixture(kind);let committed=false;mocks.read.mockResolvedValue(f.attempt);mocks.review.mockImplementation(async()=>committed?f.receipt:f.review);mocks.decide.mockImplementation(async()=>{committed=true;throw new Error('lost response')});
 render(<ReviewDetail/>);await screen.findByText(/Saved choice: Accept/);
 expect(screen.getAllByRole('checkbox').every(node=>node.getAttribute('aria-disabled')==='true')).toBe(true);
 fireEvent.click(screen.getByText('Recover saved decision'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalled());
 expect(mocks.decide).toHaveBeenCalledTimes(1);expect(mocks.decide).toHaveBeenCalledWith(f.review,'accept',f.attempt.body.accepted,f.attempt.body.rejected,f.attempt.key,expect.any(AbortSignal));expect(mocks.clear).toHaveBeenCalledWith(f.attempt.key,f.receipt);
});
it('reject submits empty candidate arrays and saves before mutation',async()=>{
 const f=fixture();mocks.read.mockResolvedValue(null);mocks.review.mockResolvedValue(f.review);mocks.write.mockRejectedValue(new Error('disk full'));
 render(<ReviewDetail/>);await screen.findByText('defects review');fireEvent.click(screen.getByText('Reject'));await screen.findByText(/Saved choice: Reject/);
 expect(mocks.write.mock.calls[0][0].body.accepted).toEqual([]);expect(mocks.write.mock.calls[0][0].body.rejected).toEqual([]);expect(mocks.decide).not.toHaveBeenCalled();
});
it('refuses a changed actor before replay',async()=>{
 const f=fixture();mocks.read.mockResolvedValue(f.attempt);mocks.review.mockResolvedValue(f.review);render(<ReviewDetail/>);await screen.findByText(/Saved choice: Accept/);mocks.me.mockResolvedValue({id:id(9)});fireEvent.click(screen.getByText('Recover saved decision'));await screen.findByRole('alert');expect(mocks.decide).not.toHaveBeenCalled();expect(mocks.clear).not.toHaveBeenCalled();
});

it('cancels initial read and ignores its late response before readonly retry',async()=>{
 const f=fixture();let finish!:(value:unknown)=>void;mocks.review.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValue(f.review);mocks.read.mockResolvedValue(null);
 render(<ReviewDetail/>);fireEvent.click(screen.getByRole('button',{name:'Cancel loading review'}));await act(async()=>finish(f.review));expect(screen.queryByText('defects review')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Retry review previews'}));await screen.findByText('defects review');expect(mocks.decide).not.toHaveBeenCalled();
});
it('bounds a noncooperative initial review and exposes readonly retry',async()=>{
 vi.useFakeTimers();mocks.review.mockImplementation(()=>new Promise(()=>{}));render(<ReviewDetail/>);await act(async()=>vi.advanceTimersByTimeAsync(15000));expect(screen.getByRole('alert')).toBeTruthy();expect(screen.getByRole('button',{name:'Retry review previews'})).toBeTruthy();expect(mocks.decide).not.toHaveBeenCalled();
});
it('preserves a cancelled saved decision and retries exact body/key while refusing the old late receipt',async()=>{
 const f=fixture();let finish!:(value:unknown)=>void,committed=false;mocks.read.mockResolvedValue(f.attempt);mocks.review.mockImplementation(async()=>committed?f.receipt:f.review);
 mocks.decide.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockImplementationOnce(async()=>{committed=true;return f.receipt;});
 render(<ReviewDetail/>);fireEvent.click(await screen.findByText('Recover saved decision'));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByRole('button',{name:'Cancel decision request'}));await act(async()=>finish(f.receipt));expect(mocks.clear).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('Recover saved decision'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledOnce());expect(mocks.decide.mock.calls[1].slice(0,5)).toEqual(mocks.decide.mock.calls[0].slice(0,5));
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
