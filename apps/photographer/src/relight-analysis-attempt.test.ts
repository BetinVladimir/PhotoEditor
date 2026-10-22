import {expect,it} from 'vitest';
import {RelightAnalysisAttempt,type RelightAnalysisAttemptRecord} from './relight-analysis-attempt';
import {RelightAnalysisAttemptStore,clearRelightAnalysisAttempts} from './relight-analysis-attempt-store';
import type {RelightAnalysisIntent} from './relight-analysis';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const body=():RelightAnalysisIntent=>({schema_version:1,snapshot_id:id,operation:'depth.predict',base:{asset_version_id:id,source:{artifact_id:id,sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}});
const ticket={run_id:id,workflow_id:other,state:'queued' as const};
it('persists before network and retries unknown body/key unchanged',async()=>{
 const events:string[]=[],records:RelightAnalysisAttemptRecord[]=[];
 const attempt=new RelightAnalysisAttempt(id,'stable-key',body(),{write:async record=>{events.push('write');records.push(structuredClone(record));}});
 const sent:unknown[]=[];
 await expect(attempt.submit(async(project,request,key)=>{events.push('send');sent.push({project,request,key});throw new Error('lost acknowledgement');})).rejects.toThrow();
 expect(events).toEqual(['write','send']);expect(attempt.state).toBe('unknown');
 await attempt.submit(async(project,request,key)=>{sent.push({project,request,key});return ticket;});
 expect(sent[0]).toEqual(sent[1]);expect(records.at(-1)?.ticket).toEqual(ticket);expect(attempt.state).toBe('accepted');
 const cancelled=new AbortController();cancelled.abort();
 await expect(attempt.submit(async()=>{throw new Error('cancelled send');},cancelled.signal)).rejects.toThrow('RELIGHT_ANALYSIS_ATTEMPT_CANCELLED');
});
it('stops before network on journal failure',async()=>{
 let sends=0;const attempt=new RelightAnalysisAttempt(id,'key',body(),{write:async()=>{throw new Error('storage failed');}});
 await expect(attempt.submit(async()=>{sends++;return ticket;})).rejects.toThrow('storage failed');expect(sends).toBe(0);
});
it('separates operations and fences logout journals',async()=>{
 const values=new Map<string,string>();
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};
 const lock=async<T,>(_:string,action:()=>Promise<T>)=>action();
 const depth=new RelightAnalysisAttemptStore(storage,'https://example.test',id,id,id,id,'depth.predict',lock);
 const person=new RelightAnalysisAttemptStore(storage,'https://example.test',id,id,id,id,'mask.person',lock);
 const record:RelightAnalysisAttemptRecord={schema_version:1,project:id,key:'key',body:body(),ticket:null};
 await depth.write(record);expect(await person.read()).toBeNull();
 await expect(async()=>person.write(record)).rejects.toThrow('SCOPE_INVALID');
 await expect(async()=>depth.write({...record,key:'changed'})).rejects.toThrow('CONFLICT');
 await clearRelightAnalysisAttempts(storage,'https://example.test',lock);
 await expect(depth.read()).rejects.toThrow('EXPIRED');expect(values.has(depth.key)).toBe(false);
});
it('keeps the accepted analysis ticket after caller cancellation',async()=>{
 const signal=new AbortController(),writes:unknown[]=[];
 const attempt=new RelightAnalysisAttempt(id,'key',body(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('RELIGHT_ANALYSIS_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
