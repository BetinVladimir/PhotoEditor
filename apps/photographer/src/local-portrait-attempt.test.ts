import {expect,it,vi} from 'vitest';
import {LocalPortraitAttempt,type LocalPortraitAttemptRecord} from './local-portrait-attempt';
import type {LocalPortraitSelection} from './local-portrait-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():LocalPortraitSelection=>({schema_version:1,snapshot_id:id(1),ordinal:0,operation:'portrait.hair',params:{schema_version:1,base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},strength:.5,mask:ref(4),protected:ref(5),hue_delta_degrees:0,saturation_factor:1,detail:.3},target_pixel:ref(6),protected_pixel:ref(7)});
it('persists and replays the same local portrait request after an ambiguous response',async()=>{
 const writes:LocalPortraitAttemptRecord[]=[],store={write:async(record:LocalPortraitAttemptRecord)=>{writes.push(structuredClone(record));}};
 const original=body(),attempt=new LocalPortraitAttempt(id(20),'local:stable',original,store);
 original.params.strength=1;
 const send=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue({run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'});
 await expect(attempt.submit(send)).rejects.toThrow('network');
 expect(writes[0].body.params.strength).toBe(.5);
 const restored=LocalPortraitAttempt.restore(writes[0],store);
 const ticket=await restored.submit(send);
 expect(ticket.task_id).toBe(id(23));expect(send.mock.calls[0][1]).toEqual(send.mock.calls[1][1]);expect(send.mock.calls[0][2]).toBe(send.mock.calls[1][2]);
 expect(writes.at(-1)?.ticket).toEqual(ticket);
 const cancelled=new AbortController();cancelled.abort();
 await expect(restored.submit(send,cancelled.signal)).rejects.toThrow('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');expect(send).toHaveBeenCalledTimes(2);
});
it('keeps the accepted local portrait ticket after caller cancellation',async()=>{
 const signal=new AbortController(),ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const},writes:unknown[]=[];
 const attempt=new LocalPortraitAttempt(id(20),'local:stable',body(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('LOCAL_PORTRAIT_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});

it('admits recovery only after immutable persistence and keeps known acceptance if its write fails',async()=>{
 const receipt={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const};let writes=0,sends=0;
 const attempt=new LocalPortraitAttempt(id(20),'recovery:proof',body(),{write:async()=>{if(++writes===1)throw new Error('initial storage failed');if(writes===3)throw new Error('accepted storage failed');}});
 expect(attempt.canReplay).toBe(false);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('initial storage failed');
 expect(attempt.canReplay).toBe(false);expect(sends).toBe(0);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('accepted storage failed');
 expect(attempt.canReplay).toBe(true);expect(attempt.state).toBe('accepted');
 await expect(attempt.submit(async()=>{throw new Error('must not repeat accepted POST');})).resolves.toEqual(receipt);
 expect(sends).toBe(1);
 const restored=LocalPortraitAttempt.restore({...attempt.snapshot,ticket:null},{write:async()=>{}});
 expect(restored.state).toBe('unknown');expect(restored.canReplay).toBe(true);
});
