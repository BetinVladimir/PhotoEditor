// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({reviews:vi.fn(),push:vi.fn(),foreground:{active:true,generation:0},tenant:'tenant-a'}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:mocks.tenant}),router:{push:mocks.push}}));
const auth={origin:'',client:()=>({})};vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>mocks.foreground}));
vi.mock('./owner-api',()=>({OwnerAPI:class{reviews=mocks.reviews;}}));
import Reviews from '../screens/reviews/index';
const item=(id:string)=>({id,run_id:'run-123456',kind:id,state:'pending'});
afterEach(()=>{cleanup();vi.useRealTimers();vi.resetAllMocks();mocks.foreground={active:true,generation:0};mocks.tenant='tenant-a'});
it('loads every cursor page and retries a failed page without replacing prior rows',async()=>{
 mocks.reviews.mockResolvedValueOnce({items:[item('first')],next_cursor:'page2'}).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[item('second')],next_cursor:null});
 render(<Reviews/>);await screen.findByText('first');fireEvent.click(screen.getByText('Load more reviews'));await screen.findByRole('alert');expect(screen.getByText('first')).toBeTruthy();fireEvent.click(screen.getByText('Retry reviews'));await screen.findByText('second');expect(mocks.reviews.mock.calls.map(call=>call[1])).toEqual([undefined,'page2','page2']);
});
it('aborts old tenant loading and ignores its delayed rows',async()=>{
 let resolve!:(value:unknown)=>void;mocks.reviews.mockReturnValueOnce(new Promise(done=>{resolve=done})).mockResolvedValueOnce({items:[item('new')],next_cursor:null});
 const view=render(<Reviews/>);await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledTimes(1));const signal=mocks.reviews.mock.calls[0][2] as AbortSignal;mocks.tenant='tenant-b';view.rerender(<Reviews/>);await screen.findByText('new');expect(signal.aborted).toBe(true);resolve({items:[item('stale')],next_cursor:null});await waitFor(()=>expect(screen.queryByText('stale')).toBeNull());
});
it('removes rows in background and reloads authority when foreground returns',async()=>{
 mocks.reviews.mockResolvedValue({items:[item('visible')],next_cursor:null});const view=render(<Reviews/>);await screen.findByText('visible');mocks.foreground={active:false,generation:0};view.rerender(<Reviews/>);expect(screen.queryByText('visible')).toBeNull();mocks.foreground={active:true,generation:1};view.rerender(<Reviews/>);await screen.findByText('visible');expect(mocks.reviews).toHaveBeenCalledTimes(2);
});

it('cancels a continuation, retains earlier rows, and retries its exact cursor',async()=>{
 let finish!:(value:unknown)=>void;mocks.reviews.mockResolvedValueOnce({items:[item('first')],next_cursor:'page2'}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({items:[item('second')],next_cursor:null});
 render(<Reviews/>);fireEvent.click(await screen.findByText('Load more reviews'));await waitFor(()=>expect(mocks.reviews).toHaveBeenCalledTimes(2));fireEvent.click(screen.getByRole('button',{name:'Cancel loading reviews'}));await act(async()=>finish({items:[item('late')],next_cursor:null}));expect(screen.queryByText('late')).toBeNull();expect(screen.getByText('first')).toBeTruthy();
 fireEvent.click(screen.getByText('Retry reviews'));await screen.findByText('second');expect(mocks.reviews.mock.calls.map(call=>call[1])).toEqual([undefined,'page2','page2']);
});
it('bounds an ignored initial read and offers a fresh readonly retry',async()=>{
 vi.useFakeTimers();mocks.reviews.mockImplementation(()=>new Promise(()=>{}));render(<Reviews/>);await act(async()=>vi.advanceTimersByTimeAsync(8000));expect(screen.getByRole('alert')).toBeTruthy();expect(screen.getByText('Retry reviews')).toBeTruthy();
});
it('rejects duplicate continuation identities while preserving previous rows',async()=>{
 mocks.reviews.mockResolvedValueOnce({items:[item('first')],next_cursor:'page2'}).mockResolvedValueOnce({items:[item('first')],next_cursor:null});render(<Reviews/>);fireEvent.click(await screen.findByText('Load more reviews'));await screen.findByRole('alert');expect(screen.getAllByText('first')).toHaveLength(1);
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
