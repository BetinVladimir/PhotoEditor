import {afterEach,expect,it,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {watchRenderResults} from './render-results-poll';
import type {RenderFrameResult} from './editor-render-results';
const frame:RenderFrameResult={run:'11111111-1111-1111-1111-111111111111',status:'completed',images:[]};
afterEach(()=>vi.useRealTimers());
it('keeps observing after Render completion while derivatives arrive and URLs renew',async()=>{
 vi.useFakeTimers();const observe=vi.fn();
 const image={artifact_id:'22222222-2222-2222-2222-222222222222',role:'derivative',sha256:'a'.repeat(64),bytes:42,media_type:'image/jpeg',url:'https://objects.example/one',expires_at:'2030-01-01T00:00:00Z'};
 const read=vi.fn().mockResolvedValueOnce([frame]).mockResolvedValueOnce([{...frame,images:[image]}]).mockResolvedValue([{...frame,images:[{...image,url:'https://objects.example/renewed'}]}]);
 const stop=watchRenderResults({read,observe});
 await vi.advanceTimersByTimeAsync(0);expect(observe).toHaveBeenLastCalledWith([frame]);
 await vi.advanceTimersByTimeAsync(10000);expect(observe.mock.lastCall?.[0][0].images[0].url).toBe(image.url);
 await vi.advanceTimersByTimeAsync(10000);expect(observe.mock.lastCall?.[0][0].images[0].url).toContain('renewed');stop();
});
it('serializes requests and discards late responses after background/scope stop',async()=>{
 vi.useFakeTimers();let resolve!:(v:RenderFrameResult[])=>void;let signal!:AbortSignal;
 const observe=vi.fn(),read=vi.fn((s:AbortSignal)=>{signal=s;return new Promise<RenderFrameResult[]>(r=>{resolve=r;});});
 const stop=watchRenderResults({read,observe});await vi.advanceTimersByTimeAsync(60000);expect(read).toHaveBeenCalledTimes(1);
 stop();expect(signal.aborted).toBe(true);resolve([frame]);await vi.advanceTimersByTimeAsync(60000);expect(observe).not.toHaveBeenCalled();expect(read).toHaveBeenCalledTimes(1);
});
it('backs off outages and removes observations when authority is denied',async()=>{
 vi.useFakeTimers();const observe=vi.fn(),error=vi.fn();
 const read=vi.fn().mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 watchRenderResults({read,observe,error});await vi.advanceTimersByTimeAsync(19999);expect(read).toHaveBeenCalledTimes(1);
 await vi.advanceTimersByTimeAsync(1);expect(observe).toHaveBeenLastCalledWith([]);await vi.advanceTimersByTimeAsync(60000);expect(read).toHaveBeenCalledTimes(2);
});

it('stops empty terminal presentation observations but keeps waiting for pending frames',async()=>{
 vi.useFakeTimers();const observe=vi.fn();
 const read=vi.fn().mockResolvedValueOnce([{...frame,presentation:{total:2,pending:1,ready:0,failed:1,cancelled:0,unavailable:0}}]).mockResolvedValue([{...frame,presentation:{total:2,pending:0,ready:0,failed:1,cancelled:0,unavailable:1}}]);
 watchRenderResults({read,observe});await vi.advanceTimersByTimeAsync(10000);expect(read).toHaveBeenCalledTimes(2);
 await vi.advanceTimersByTimeAsync(60000);expect(read).toHaveBeenCalledTimes(2);expect(observe.mock.lastCall?.[0][0].presentation.unavailable).toBe(1);
});

it('reobserves ready presentation when the separately read output list is empty',async()=>{
 vi.useFakeTimers();const read=vi.fn().mockResolvedValue([{...frame,presentation:{total:1,pending:0,ready:1,failed:0,cancelled:0,unavailable:0}}]);
 const stop=watchRenderResults({read,observe:vi.fn()});await vi.advanceTimersByTimeAsync(20000);
 expect(read).toHaveBeenCalledTimes(3);stop();
});

it('clears authority-denied images before reporting the terminal error',async()=>{
 vi.useFakeTimers();const events:string[]=[];
 const read=vi.fn().mockRejectedValue(new APIError(410,'GONE'));
 watchRenderResults({read,observe:frames=>{expect(frames).toEqual([]);events.push('cleared');},error:()=>events.push('denied')});
 await vi.advanceTimersByTimeAsync(60000);expect(events).toEqual(['cleared','denied']);expect(read).toHaveBeenCalledTimes(1);
});
it('clears a previously displayed candidate on a transient refresh failure',async()=>{
 vi.useFakeTimers();const observed:RenderFrameResult[][]=[];
 const image={artifact_id:'22222222-2222-2222-2222-222222222222',role:'derivative',sha256:'a'.repeat(64),bytes:42,media_type:'image/jpeg',url:'https://objects.example/one',expires_at:'2030-01-01T00:00:00Z'};
 const read=vi.fn().mockResolvedValueOnce([{...frame,images:[image]}]).mockRejectedValueOnce(new Error('offline')).mockResolvedValue([frame]);
 const stop=watchRenderResults({read,observe:frames=>observed.push(frames)});
 await vi.advanceTimersByTimeAsync(0);expect(observed[0][0].images).toHaveLength(1);
 await vi.advanceTimersByTimeAsync(10000);expect(observed[1]).toEqual([]);
 await vi.advanceTimersByTimeAsync(20000);expect(observed[2]).toEqual([frame]);stop();
});
