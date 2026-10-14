import {afterEach,it,expect,vi} from 'vitest';
import {APIError} from '@photoeditor/api-client';
import {watchRenderRun} from './render-poll';
import type {RunStatus} from './catalog-types';
const run='11111111-1111-1111-1111-111111111111';
const wire=(revision='1',status='running'):RunStatus=>({id:run,kind:'bulk',revision,status,counts:{total:1,succeeded:0,failed:0,skipped:0,review:0},stage:status,progress:0,errors:[]});
afterEach(()=>vi.useRealTimers());
it('serializes reads and stops after a terminal observation',async()=>{
 vi.useFakeTimers();let current:RunStatus|null=null;let resolve!:(value:unknown)=>void;
 const read=vi.fn(()=>new Promise<unknown>(r=>{resolve=r;}));
 const stop=watchRenderRun({run,read,current:()=>current,observe:s=>{current=s;},error:vi.fn()});
 await vi.advanceTimersByTimeAsync(10000);expect(read).toHaveBeenCalledTimes(1);
 resolve(wire());await vi.advanceTimersByTimeAsync(0);
 await vi.advanceTimersByTimeAsync(2000);expect(read).toHaveBeenCalledTimes(2);
 resolve(wire('2','completed'));await vi.advanceTimersByTimeAsync(30000);
 expect(read).toHaveBeenCalledTimes(2);expect((current as RunStatus|null)?.status).toBe('completed');stop();
});
it('aborts in-flight work and ignores its late success',async()=>{
 vi.useFakeTimers();let resolve!:(value:unknown)=>void;let signal!:AbortSignal;const observe=vi.fn();
 const stop=watchRenderRun({run,read:(_,s)=>{signal=s;return new Promise(r=>{resolve=r;});},current:()=>null,observe,error:vi.fn()});
 stop();expect(signal.aborted).toBe(true);resolve(wire());await vi.advanceTimersByTimeAsync(30000);expect(observe).not.toHaveBeenCalled();
});
it('backs off transient failures, preserves newer observations and stops on authority denial',async()=>{
 vi.useFakeTimers();let current:RunStatus|null=wire('3');const error=vi.fn();
 const read=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(wire('2')).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 const stop=watchRenderRun({run,read,current:()=>current,observe:s=>{current=s;},error});
 await vi.advanceTimersByTimeAsync(3999);expect(read).toHaveBeenCalledTimes(1);
 await vi.advanceTimersByTimeAsync(1);expect(read).toHaveBeenCalledTimes(2);expect(current?.revision).toBe('3');
 await vi.advanceTimersByTimeAsync(32000);expect(read).toHaveBeenCalledTimes(3);expect(error).toHaveBeenCalledTimes(2);stop();
});

it('keeps review observable and rejects a foreign run without installing it',async()=>{
 vi.useFakeTimers();let current:RunStatus|null=null;const error=vi.fn();
 const read=vi.fn().mockResolvedValueOnce(wire('1','waiting_review')).mockResolvedValueOnce({...wire('2'),id:'22222222-2222-2222-2222-222222222222'}).mockResolvedValueOnce(wire('3','completed'));
 const stop=watchRenderRun({run,read,current:()=>current,observe:s=>{current=s;},error});
 await vi.advanceTimersByTimeAsync(9999);expect(read).toHaveBeenCalledTimes(1);
 await vi.advanceTimersByTimeAsync(1);expect((current as RunStatus|null)?.status).toBe('waiting_review');expect(error).toHaveBeenCalledTimes(1);
 await vi.advanceTimersByTimeAsync(4000);expect((current as RunStatus|null)?.status).toBe('completed');stop();
});
