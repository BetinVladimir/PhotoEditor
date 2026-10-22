// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import en from '../../../locales/en/common.json';
const mocks=vi.hoisted(()=>({list:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./multi-apply-store',()=>({listMultiApplyAttempts:mocks.list}));
vi.mock('./multi-apply-recovery',()=>({MultiApplyRecovery:({producer}:{producer:string})=><div data-testid="recovery">{producer}</div>}));
import {MultiApplyInbox} from './multi-apply-inbox';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const api={me:async()=>({id:id(1)})} as unknown as OwnerAPI;
const props={api,origin:'https://studio.test',tenant:id(2),project:id(3),collection:id(4),onApplied:vi.fn()};
// UI transport fixtures only: immutable record validation belongs to store/attempt suites.
const record=(producer:string)=>({review:{proposal:{producer_task_id:producer,operation:'optical.panorama',sources:[{},{}],canvas:{width:100,height:80}}},intent:{}});
afterEach(()=>{cleanup();vi.resetAllMocks();});
it('renders loading and empty states; retries a discovery error',async()=>{
 mocks.list.mockRejectedValueOnce(new Error('storage')).mockResolvedValueOnce({records:[],nextCursor:null});
 render(<MultiApplyInbox {...props}/>);expect(screen.getByText(en.multi_recovery_loading)).toBeTruthy();
 await screen.findByRole('alert');fireEvent.click(screen.getByText(en.perspective_apply_reload));
 await screen.findByText(en.multi_recovery_empty);expect(screen.queryByRole('alert')).toBeNull();
});
it('passes cursor for the next page and refresh returns to the beginning',async()=>{
 mocks.list.mockResolvedValueOnce({records:[record(id(10))],nextCursor:id(10)}).mockResolvedValueOnce({records:[record(id(11))],nextCursor:null}).mockResolvedValueOnce({records:[],nextCursor:null});
 render(<MultiApplyInbox {...props}/>);await screen.findByText(id(10));
 fireEvent.click(screen.getByText(en.multi_recovery_next));await screen.findByText(id(11));
 expect(mocks.list.mock.calls[1][1].cursor).toBe(id(10));expect(screen.queryByText(id(10))).toBeNull();
 fireEvent.click(screen.getByText(en.perspective_apply_reload));await screen.findByText(en.multi_recovery_empty);
 expect(mocks.list.mock.calls[2][1].cursor).toBeUndefined();
});
it('resets pagination on collection change and ignores late prior-scope results',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.list.mockResolvedValueOnce({records:[record(id(10))],nextCursor:id(10)}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({records:[],nextCursor:null});
 const view=render(<MultiApplyInbox {...props}/>);await screen.findByText(id(10));
 fireEvent.click(screen.getByText(en.multi_recovery_next));await waitFor(()=>expect(mocks.list).toHaveBeenCalledTimes(2));
 view.rerender(<MultiApplyInbox {...props} collection={id(5)}/>);await screen.findByText(en.multi_recovery_empty);
 expect(mocks.list.mock.calls[2][1].collection).toBe(id(5));expect(mocks.list.mock.calls[2][1].cursor).toBeUndefined();
 await act(async()=>{finish({records:[record(id(99))],nextCursor:null});});
 expect(screen.queryByText(id(99))).toBeNull();
});
