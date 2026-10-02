import {APIError} from '@photoeditor/api-client';
import {afterEach,expect,it,vi} from 'vitest';
import {watchRun} from './run-watch';
import type {RunStatus} from './catalog-types';
const value=(revision:string,status='running'):RunStatus=>({id:'run',kind:'bulk',status,revision,counts:{total:2,succeeded:0,failed:0,skipped:0,review:0},stage:status,progress:0,errors:[]});
afterEach(()=>vi.useRealTimers());
it('refreshes matching phase events, ignores unrelated runs and refuses older mutation races',async()=>{
 vi.useFakeTimers();const stop=new AbortController(),receive=vi.fn(),failed=vi.fn();
 let event:Parameters<Parameters<typeof watchRun>[0]['stream']>[1]|undefined;
 const load=vi.fn().mockResolvedValue(value('1'));
 const stream=vi.fn((_cursor,callback,signal)=>{event=callback;return new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true}));});
 const watch=watchRun({load,stream},'run',stop.signal,receive,failed);
 await watch.refresh();load.mockClear();receive.mockClear();
 event!({cursor:'4',type:'run.running',payload:{run_id:'other'}});expect(load).not.toHaveBeenCalled();
 load.mockResolvedValue(value('2'));event!({cursor:'5',type:'run.waiting_review',payload:{run_id:'run',status:'failed'}});
 await Promise.resolve();await Promise.resolve();expect(receive.mock.lastCall?.[0].status).toBe('running');
 watch.accept(value('4','completed'));load.mockResolvedValue(value('3'));await watch.refresh();expect(receive.mock.lastCall?.[0].revision).toBe('4');
 stop.abort();await vi.advanceTimersByTimeAsync(4000);expect(failed).not.toHaveBeenCalled();
});
it('polls after transient load failure and reconnects with the last stream cursor',async()=>{
 vi.useFakeTimers();const stop=new AbortController(),receive=vi.fn(),failed=vi.fn();
 const load=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(value('2'));
 const stream=vi.fn().mockImplementationOnce(async(_cursor,callback)=>{callback({cursor:'9',type:'run.running',payload:{run_id:'run'}});throw new Error('disconnect');}).mockRejectedValueOnce(new APIError(410,'EVENTS_EXPIRED')).mockImplementation((_cursor,_callback,signal)=>new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true})));
 watchRun({load,stream},'run',stop.signal,receive,failed);
 await vi.advanceTimersByTimeAsync(2000);
 expect(failed).toHaveBeenCalled();expect(receive).toHaveBeenCalled();expect(stream.mock.calls[1][0]).toBe('9');
 await vi.advanceTimersByTimeAsync(4000);expect(stream.mock.calls[2][0]).toBeUndefined();
 stop.abort();const before=load.mock.calls.length;await vi.advanceTimersByTimeAsync(5000);expect(load.mock.calls.length).toBe(before);
});

it('reduces terminal polling while detecting a retry without streaming',async()=>{
 vi.useFakeTimers();const stop=new AbortController(),receive=vi.fn();
 const load=vi.fn().mockResolvedValue(value('1','failed'));
 const stream=vi.fn((_cursor,_callback,signal)=>new Promise<void>(resolve=>signal.addEventListener('abort',()=>resolve(),{once:true})));
 const watch=watchRun({load,stream},'run',stop.signal,receive,vi.fn());
 await watch.refresh();load.mockClear();
 await vi.advanceTimersByTimeAsync(8000);expect(load).not.toHaveBeenCalled();
 load.mockResolvedValue(value('2','running'));await vi.advanceTimersByTimeAsync(2000);expect(load).toHaveBeenCalledOnce();
 await vi.advanceTimersByTimeAsync(2000);expect(load).toHaveBeenCalledTimes(2);
 stop.abort();
});
it('backs off repeated stream failures and cancels the pending reconnect delay',async()=>{
 vi.useFakeTimers();const stop=new AbortController();
 const load=vi.fn().mockResolvedValue(value('1'));
 const stream=vi.fn().mockRejectedValue(new Error('unsupported stream'));
 watchRun({load,stream},'run',stop.signal,vi.fn(),vi.fn());
 await vi.advanceTimersByTimeAsync(60000);expect(stream).toHaveBeenCalledTimes(6);expect(load).toHaveBeenCalled();
 stop.abort();await vi.advanceTimersByTimeAsync(60000);expect(stream).toHaveBeenCalledTimes(6);
});

it('stops requests on stream access denial even when its failure callback does not abort',async()=>{
 vi.useFakeTimers();const stop=new AbortController(),failed=vi.fn(),receive=vi.fn();
 let resolve!:(run:RunStatus)=>void;const load=vi.fn(()=>new Promise<RunStatus>(done=>resolve=done));
 const stream=vi.fn().mockRejectedValue(new APIError(403,'FORBIDDEN'));
 watchRun({load,stream},'run',stop.signal,receive,failed);
 await vi.advanceTimersByTimeAsync(0);expect(failed).toHaveBeenCalledOnce();
 resolve(value('1'));await vi.advanceTimersByTimeAsync(60000);
 expect(receive).not.toHaveBeenCalled();expect(load).toHaveBeenCalledOnce();expect(stream).toHaveBeenCalledOnce();stop.abort();
});
