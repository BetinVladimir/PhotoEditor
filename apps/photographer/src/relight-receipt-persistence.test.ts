import {it,expect,vi} from 'vitest';
import {RelightAttempt} from './relight-attempt';
import {RelightAnalysisAttempt} from './relight-analysis-attempt';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const ref={artifact_id:id,sha256:'a'.repeat(64)},base={asset_version_id:id,source:ref,canvas_hash:'b'.repeat(64)};
const ticket={run_id:id,workflow_id:other,state:'queued' as const};
function make(kind:string,write:(record:unknown)=>Promise<void>){
 return kind==='analysis'?new RelightAnalysisAttempt(id,'original-key',{schema_version:1,snapshot_id:id,operation:'depth.predict',base},{write})
 :new RelightAttempt(id,'original-key',{schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',params:{schema_version:1,base,strength:.5,depth:ref,subject:ref,protected:ref,calibration:ref,lights:[{x:.5,y:.5,radius:.3,ev:0}]}},{write});
}
it.each(['analysis','relight'])('%s retains accepted receipt after acknowledgement storage failure',async kind=>{
 let writes=0;const records:unknown[]=[],write=vi.fn(async(record:unknown)=>{records.push(structuredClone(record));if(++writes===2)throw new Error('disk full')});
 const attempt=make(kind,write),response={...ticket},send=vi.fn().mockResolvedValue(response);
 await expect(attempt.submit(send)).rejects.toThrow('disk full');expect(attempt.state).toBe('accepted');expect(attempt.snapshot.ticket).toEqual(ticket);
 response.run_id=other;const snapshot=attempt.snapshot;snapshot.ticket!.run_id=other;
 await expect(attempt.submit(send)).resolves.toEqual(ticket);expect(send).toHaveBeenCalledOnce();expect(write).toHaveBeenCalledTimes(3);expect((records.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
});
it.each(['analysis','relight'])('%s keeps accepted receipt through repeated persistence failures',async kind=>{
 let writes=0;const attempt=make(kind,async()=>{if(++writes===2||writes===3)throw new Error('disk full')}),send=vi.fn().mockResolvedValue(ticket);
 await expect(attempt.submit(send)).rejects.toThrow('disk full');await expect(attempt.submit(send)).rejects.toThrow('disk full');expect(attempt.state).toBe('accepted');
 await expect(attempt.submit(send)).resolves.toEqual(ticket);expect(send).toHaveBeenCalledOnce();expect(writes).toBe(4);
});
it.each(['analysis','relight'])('%s preserves accepted receipt when cancellation and failed storage overlap',async kind=>{
 let writes=0;const controller=new AbortController(),attempt=make(kind,async()=>{if(++writes===2)throw new Error('disk full')}),send=vi.fn(async()=>{controller.abort();return ticket});
 await expect(attempt.submit(send,controller.signal)).rejects.toThrow('disk full');expect(attempt.state).toBe('accepted');await expect(attempt.submit(send)).resolves.toEqual(ticket);expect(send).toHaveBeenCalledOnce();
});
it.each(['analysis','relight'])('%s rejects concurrent persistence retry without repeating transport',async kind=>{
 let writes=0,finish!:()=>void;const attempt=make(kind,async()=>{writes++;if(writes===2)throw new Error('disk full');if(writes===3)await new Promise<void>(resolve=>{finish=resolve})}),send=vi.fn().mockResolvedValue(ticket);
 await expect(attempt.submit(send)).rejects.toThrow('disk full');const retry=attempt.submit(send);
 await expect(attempt.submit(send)).rejects.toThrow('ATTEMPT_BUSY');finish();await expect(retry).resolves.toEqual(ticket);expect(send).toHaveBeenCalledOnce();
});
