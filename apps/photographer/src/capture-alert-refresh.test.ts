import {describe,it,expect,vi} from 'vitest';
import {readCaptureAlertDelta} from './capture-alert-refresh';
import type {CaptureAlert} from './capture-types';
const alert=(id:string)=>({id} as CaptureAlert);
describe('Capture delta pagination',()=>{
 it('fetches an entire burst through the previous window',async()=>{
  const read=vi.fn().mockResolvedValueOnce({items:[alert('new2')],next_cursor:'a'})
   .mockResolvedValueOnce({items:[alert('new1')],next_cursor:'b'})
   .mockResolvedValueOnce({items:[alert('known')],next_cursor:'old-history'});
  const result=await readCaptureAlertDelta(read,[alert('known')],new AbortController().signal);
  expect(result.items.map(x=>x.id)).toEqual(['new2','new1','known']);
  expect(read.mock.calls.map(x=>x[0])).toEqual([undefined,'a','b']);
 });
 it('leaves initial history for explicit pagination',async()=>{
  const read=vi.fn().mockResolvedValue({items:[alert('new')],next_cursor:'history'});
  expect(await readCaptureAlertDelta(read,[],new AbortController().signal)).toEqual({items:[alert('new')],initialCursor:'history'});
  expect(read).toHaveBeenCalledTimes(1);
 });
 it('rejects a looping cursor instead of endlessly fetching',async()=>{
  const read=vi.fn().mockResolvedValue({items:[],next_cursor:'same'});
  await expect(readCaptureAlertDelta(read,[alert('known')],new AbortController().signal)).rejects.toThrow('Repeated capture cursor');
  expect(read).toHaveBeenCalledTimes(2);
 });
 it('discards a late response after session cancellation',async()=>{
  const controller=new AbortController();
  const read=vi.fn().mockImplementation(async()=>{controller.abort();return {items:[alert('late')],next_cursor:'next'}});
  await expect(readCaptureAlertDelta(read,[alert('known')],controller.signal)).rejects.toThrow();
  expect(read).toHaveBeenCalledTimes(1);
 });
 it('reads updates through the oldest displayed alert instead of stopping at the first overlap',async()=>{
  const newer={...alert('newer'),created_at:'2026-01-02'};
  const older={...alert('older'),created_at:'2026-01-01'};
  const updated={...older,confirmation_id:'confirmed'};
  const read=vi.fn().mockResolvedValueOnce({items:[newer],next_cursor:'second'})
   .mockResolvedValueOnce({items:[updated],next_cursor:'unloaded-history'});
  const result=await readCaptureAlertDelta(read,[older,newer],new AbortController().signal);
  expect(result.items).toEqual([newer,updated]);
  expect(read.mock.calls.map(call=>call[0])).toEqual([undefined,'second']);
 });

});
