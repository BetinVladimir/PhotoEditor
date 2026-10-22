import {describe,it,expect,vi} from 'vitest';
import {eventCursor,readTenantEvent,readTenantEventPage} from './tenant-admin-response';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const event=(cursor='100')=>({cursor,type:'capture.alert',run_id:null,collection_id:id(2),payload:{session_id:id(3)},created_at:'2026-10-17T10:00:00.000001Z'});
describe('actual tenant event boundary',()=>{
 it('retains nullable IDs and exact ordered cursor strings',()=>{
  expect(eventCursor('9223372036854775807')).toBe('9223372036854775807');
  expect(readTenantEventPage({items:[event()],next_cursor:'100'},'99')).toEqual({items:[event()],next_cursor:'100'});
  expect(readTenantEventPage({items:[],next_cursor:''})).toEqual({items:[],next_cursor:''});
  expect(readTenantEventPage({items:[],next_cursor:'99'},'99')).toEqual({items:[],next_cursor:'99'});
 });
 it('refuses corrupt, aliased and regressed frames/pages',()=>{
  for(const value of ['01','+1','-0','9223372036854775808'])expect(()=>eventCursor(value)).toThrow();
  for(const value of [{...event(),collection_id:undefined},{...event(),run_id:"00000000-0000-0000-0000-000000000000"},{...event(),extra:true},{...event(),created_at:null}])expect(()=>readTenantEvent(value)).toThrow();
  for(const value of [{items:[event(),event()],next_cursor:'100'},{items:[event()],next_cursor:null},{items:[],next_cursor:'101'},{items:[event()],next_cursor:'101'}])expect(()=>readTenantEventPage(value,'99')).toThrow();
 });
 it('mounts JSON and SSE decoding and refuses callbacks after cancellation',async()=>{
  let callback:((e:unknown)=>void)|undefined;
  const request=vi.fn(async()=>({items:[event()],next_cursor:'100'}));
  const client={request,eventStream:vi.fn(async(_path:unknown,_cursor:unknown,fn:(e:unknown)=>void)=>{callback=fn;})};
  const api=new OwnerAPI(client as never,id(1));
  expect((await api.events('99')).items[0]).toEqual(event());
  const seen=vi.fn(),controller=new AbortController();await api.streamEvents('99',seen,controller.signal);callback!(event());expect(seen).toHaveBeenCalledTimes(1);
  expect(()=>callback!(event())).toThrow();controller.abort();expect(()=>callback!(event('101'))).toThrow();expect(seen).toHaveBeenCalledTimes(1);
 });
});
