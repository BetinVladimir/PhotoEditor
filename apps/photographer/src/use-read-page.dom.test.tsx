// @vitest-environment jsdom
import {act,cleanup,renderHook} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const foreground=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>foreground}));
import {useReadPage} from './use-read-page';
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return{promise,resolve};}
afterEach(()=>{cleanup();foreground.active=true;foreground.generation=0;vi.useRealTimers();});
it('preserves the first page and explicitly retries the same cursor after failure',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'next'}).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[{id:'b'}]});
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});
 await act(async()=>{await result.current.more();});expect(result.current.morePhase).toBe('error');expect(result.current.data?.items).toEqual([{id:'a'}]);
 await act(async()=>{await result.current.more();});expect(result.current.data?.items).toEqual([{id:'a'},{id:'b'}]);expect(read.mock.calls.slice(1).map(call=>call[0])).toEqual(['next','next']);
});
it('cancels a late page without removing already loaded items or accepting the late append',async()=>{
 const late=deferred<{items:{id:string}[]}>(),read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'next'}).mockReturnValueOnce(late.promise);
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});let flight!:Promise<void>;
 act(()=>{flight=result.current.more();});act(()=>result.current.cancelMore());await act(async()=>{late.resolve({items:[{id:'late'}]});await flight;});
 expect(result.current.data?.items).toEqual([{id:'a'}]);expect(result.current.morePhase).toBe('cancelled');
});
it('rejects a cursor cycle and duplicate IDs rather than appending corrupt pagination',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'one'}).mockResolvedValueOnce({items:[{id:'b'}],next_cursor:'two'}).mockResolvedValueOnce({items:[{id:'c'}],next_cursor:'one'});
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});await act(async()=>{await result.current.more();});await act(async()=>{await result.current.more();});
 expect(result.current.morePhase).toBe('error');expect(result.current.data?.items).toEqual([{id:'a'},{id:'b'}]);
});
it('does not append a second occurrence of an already loaded item',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'next'}).mockResolvedValueOnce({items:[{id:'a'}]});
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});await act(async()=>{await result.current.more();});
 expect(result.current.morePhase).toBe('error');expect(result.current.data?.items).toEqual([{id:'a'}]);
});
it('refuses a retained old More callback before issuing any request in a new scope',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'private-cursor'}).mockResolvedValueOnce({items:[{id:'b'}]});const connection={};
 const{result,rerender}=renderHook(({scope})=>useReadPage(scope,connection,read),{initialProps:{scope:'a'}});await act(async()=>{});const oldMore=result.current.more;
 rerender({scope:'b'});await act(async()=>{});await act(async()=>{await oldMore();});
 expect(read).toHaveBeenCalledTimes(2);expect(read.mock.calls.every(call=>call[0]===undefined)).toBe(true);expect(result.current.data?.items).toEqual([{id:'b'}]);
});

it.each(['2',undefined])('does not merge a page with changed or missing snapshot revision: %s',async revision=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],revision:'1',next_cursor:'next'}).mockResolvedValueOnce({items:[{id:'b'}],revision});
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});await act(async()=>{await result.current.more();});
 expect(result.current.morePhase).toBe('error');expect(result.current.data).toEqual({items:[{id:'a'}],revision:'1',next_cursor:'next'});
});
it('appends pages only within the same declared revision',async()=>{
 const read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],revision:'1',next_cursor:'next'}).mockResolvedValueOnce({items:[{id:'b'}],revision:'1'});
 const{result}=renderHook(()=>useReadPage('scope',read,read));await act(async()=>{});await act(async()=>{await result.current.more();});
 expect(result.current.morePhase).toBe('idle');expect(result.current.data?.items).toEqual([{id:'a'},{id:'b'}]);expect(result.current.data?.revision).toBe('1');
});
it('ignores a late continuation after same-scope refresh',async()=>{
 const late=deferred<{items:{id:string}[]}>(),read=vi.fn().mockResolvedValueOnce({items:[{id:'a'}],next_cursor:'next'}).mockReturnValueOnce(late.promise).mockResolvedValueOnce({items:[{id:'fresh'}]});
 const{result}=renderHook(()=>useReadPage('same',read,read));await act(async()=>{});let pending!:Promise<void>;
 act(()=>{pending=result.current.more();});act(()=>result.current.retry());await act(async()=>{});
 await act(async()=>{late.resolve({items:[{id:'stale'}]});await pending;});
 expect(result.current.data?.items).toEqual([{id:'fresh'}]);
});
