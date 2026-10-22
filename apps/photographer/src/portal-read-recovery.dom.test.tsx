// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
const calls=vi.hoisted(()=>({getCollection:vi.fn(),exchange:vi.fn()}));
vi.mock('../../portal/src/lib/api',()=>({getCollection:calls.getCollection,exchange:calls.exchange,secretFromFragment:()=>null,APIError:class extends Error{constructor(public status:number,public code:string){super(code);}}}));
import {Portal} from '../../portal/src/components/Portal';
const collection=(name:string)=>({name,description:'',revision:'1',capabilities:[],allowed_presets:[],sections:[],empty:true});
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return{promise,resolve};}
beforeEach(()=>{sessionStorage.setItem('photo:portal:a:csrf','token-a');sessionStorage.setItem('photo:portal:b:csrf','token-b');});
afterEach(()=>{cleanup();sessionStorage.clear();vi.useRealTimers();vi.resetAllMocks();});
it('retries failed gallery opening and does not replay session exchange',async()=>{
 calls.getCollection.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(collection('Gallery'));
 render(<Portal locale="en" shareID="a"/>);await act(async()=>{});
 fireEvent.click(screen.getByText('Try again'));await act(async()=>{});
 expect(screen.getByRole('heading',{name:'Gallery'})).toBeTruthy();expect(calls.exchange).not.toHaveBeenCalled();
});
it('cancels a noncooperative opening, rejects its late result, and recovers with retry',async()=>{
 const old=deferred<ReturnType<typeof collection>>();calls.getCollection.mockReturnValueOnce(old.promise).mockResolvedValueOnce(collection('Fresh'));
 render(<Portal locale="en" shareID="a"/>);fireEvent.click(screen.getByText('Cancel'));
 await act(async()=>{old.resolve(collection('Old'));});expect(screen.queryByRole('heading',{name:'Old'})).toBeNull();
 fireEvent.click(screen.getByText('Try again'));await act(async()=>{});expect(screen.getByRole('heading',{name:'Fresh'})).toBeTruthy();
});
it('does not project a late old-share response into a newly selected gallery',async()=>{
 const old=deferred<ReturnType<typeof collection>>(),next=deferred<ReturnType<typeof collection>>();calls.getCollection.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise);
 const view=render(<Portal locale="en" shareID="a"/>);view.rerender(<Portal locale="en" shareID="b"/>);
 await act(async()=>{old.resolve(collection('Private A'));});expect(screen.queryByRole('heading',{name:'Private A'})).toBeNull();
 await act(async()=>{next.resolve(collection('B'));});expect(screen.getByRole('heading',{name:'B'})).toBeTruthy();
});
it('offers retry after bounded opening even when transport ignores abort',()=>{
 vi.useFakeTimers();calls.getCollection.mockReturnValue(new Promise(()=>{}));render(<Portal locale="en" shareID="a"/>);
 act(()=>vi.advanceTimersByTime(10000));expect(screen.getByText('Try again')).toBeTruthy();
});
