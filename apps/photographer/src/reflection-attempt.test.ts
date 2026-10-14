import {expect,it,vi} from 'vitest';
import {ReflectionAttempt,type ReflectionAttemptRecord} from './reflection-attempt';
import type {ReflectionIntent} from './reflection-intent';

const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():ReflectionIntent=>({schema_version:1,snapshot_id:id(1),ordinal:0,
 params_proposal:{schema_version:1,base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},
 strength:.5,mask:ref(4),protected:ref(5),model_lock_sha:'c'.repeat(64),calibration:ref(8)},
 target_pixel:ref(6),protected_pixel:ref(7)});

it('persists exact reflection intent before send and replays ambiguous response',async()=>{
 const writes:ReflectionAttemptRecord[]=[],store={write:async(record:ReflectionAttemptRecord)=>{writes.push(structuredClone(record));}};
 const original=body(),attempt=new ReflectionAttempt(id(20),'reflection:stable',original,store);
 original.params_proposal.strength=1;
 const send=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue({run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'});
 await expect(attempt.submit(send)).rejects.toThrow('network');
 expect(writes).toHaveLength(1);expect(writes[0].body.params_proposal.strength).toBe(.5);
 const restored=ReflectionAttempt.restore(writes[0],store);
 const ticket=await restored.submit(send);
 expect(ticket.task_id).toBe(id(23));expect(send.mock.calls[0][1]).toEqual(send.mock.calls[1][1]);
 expect(send.mock.calls[0][2]).toBe(send.mock.calls[1][2]);
 expect(writes.at(-1)?.ticket).toEqual(ticket);
 const cancelled=new AbortController();cancelled.abort();
 await expect(restored.submit(send,cancelled.signal)).rejects.toThrow('REFLECTION_ATTEMPT_CANCELLED');expect(send).toHaveBeenCalledTimes(2);
});
it('keeps the accepted reflection ticket after caller cancellation',async()=>{
 const signal=new AbortController(),ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const},writes:unknown[]=[];
 const attempt=new ReflectionAttempt(id(20),'reflection:stable',body(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('REFLECTION_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
it('owns the accepted reflection ticket across a mutating persistence callback',async()=>{
 const ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const};
 const attempt=new ReflectionAttempt(id(20),'reflection:stable',body(),{write:async record=>{if(record.ticket)record.ticket.run_id=id(99);}});
 await expect(attempt.submit(async()=>ticket)).resolves.toEqual(ticket);
 expect(attempt.snapshot.ticket).toEqual(ticket);
});
