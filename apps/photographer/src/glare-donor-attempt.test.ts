import {expect,it,vi} from 'vitest';
import {GlareDonorAttempt,type GlareDonorAttemptRecord} from './glare-donor-attempt';
import type {GlareDonorIntent} from './glare-donor-intent';

const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():GlareDonorIntent=>({schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 source_observation_id:id(3),donor_observation_id:id(4),params_proposal:{schema_version:1,mode:'donor',
 base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},
 donor:{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)},strength:.5,
 glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),
 donor_faces:ref(13),model_lock_sha:'d'.repeat(64),calibration:ref(14)}});

it('persists the same donor glare request before send and replays ambiguous response',async()=>{
 const writes:GlareDonorAttemptRecord[]=[],store={write:async(record:GlareDonorAttemptRecord)=>{writes.push(structuredClone(record));}};
 const original=body(),attempt=new GlareDonorAttempt(id(20),'glare:stable',original,store);
 original.params_proposal.strength=1;
 const send=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue({run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'});
 await expect(attempt.submit(send)).rejects.toThrow('network');
 expect(writes).toHaveLength(1);expect(writes[0].body.params_proposal.strength).toBe(.5);
 const restored=GlareDonorAttempt.restore(writes[0],store);
 const ticket=await restored.submit(send);
 expect(ticket.task_id).toBe(id(23));expect(send.mock.calls[0][1]).toEqual(send.mock.calls[1][1]);
 expect(send.mock.calls[0][2]).toBe(send.mock.calls[1][2]);
 expect(writes.at(-1)?.ticket).toEqual(ticket);
 const cancelled=new AbortController();cancelled.abort();
 await expect(restored.submit(send,cancelled.signal)).rejects.toThrow('GLARE_DONOR_ATTEMPT_CANCELLED');expect(send).toHaveBeenCalledTimes(2);
});
it('keeps the accepted glare ticket after caller cancellation',async()=>{
 const signal=new AbortController(),ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const},writes:unknown[]=[];
 const attempt=new GlareDonorAttempt(id(20),'glare:stable',body(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('GLARE_DONOR_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
