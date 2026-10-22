// @vitest-environment jsdom
import {expectEnabled} from './test/ui-readiness';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native,Image:({source,accessibilityLabel,onLoad,onError}:any)=>React.createElement('img',{src:source?.uri,'aria-label':accessibilityLabel,onLoad,onError})};});
const mocks=vi.hoisted(()=>({review:vi.fn(),decide:vi.fn(),foreground:{active:true,generation:0}}));
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({id:id(1),tenant:id(5)}),router:{replace:vi.fn(),push:vi.fn()}}));
const auth={origin:'https://display.test',client:()=>({})};vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canEdit:true,actor:id(6)})}));
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>mocks.foreground}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./review-decision-attempt-store',()=>({ReviewDecisionAttemptStore:class{read=async()=>null;}}));
vi.mock('./owner-api',()=>({OwnerAPI:class{review=mocks.review;decide=mocks.decide;me=async()=>({id:id(6)});}}));
vi.mock('./review-preview',()=>({loadReviewPreview:async(_api:unknown,_review:unknown,side:string)=>({side})}));
vi.mock('./review-preview-display',()=>({prepareReviewPreviewPair:async()=>[{uri:'data:image/jpeg;base64,before',width:100,height:100},{uri:'data:image/jpeg;base64,after',width:100,height:100}]}));
import ReviewDetail from '../screens/reviews/[id]';
afterEach(()=>{cleanup();vi.resetAllMocks();mocks.foreground={active:true,generation:0};});
it.each(['optical.negative','render.compose','portrait.hair','portrait.clothing','portrait.open_eyes','portrait.glare','image.reflection'])('requires both visible %s images before permitting a photographer decision',async kind=>{
 mocks.review.mockResolvedValue({id:id(1),run_id:id(2),kind,state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},candidate_ids:[],reasons:[]});
 const view=render(<ReviewDetail/>);const before=await screen.findByLabelText('Review before');const accept=screen.getByRole('button',{name:'Accept'});expect(accept?.getAttribute('aria-disabled')).toBe('true');
 fireEvent.load(before);expect(accept?.getAttribute('aria-disabled')).toBe('true');fireEvent.load(screen.getByLabelText('Review after'));await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Accept'})));
 mocks.foreground={active:false,generation:0};view.rerender(<ReviewDetail/>);expect(screen.queryByText('Accept')).toBeNull();mocks.foreground={active:true,generation:1};view.rerender(<ReviewDetail/>);await screen.findByLabelText('Review before');fireEvent.load(before);expect(screen.getByRole('button',{name:'Accept'})?.getAttribute('aria-disabled')).toBe('true');expect(mocks.decide).not.toHaveBeenCalled();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
