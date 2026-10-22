import {expect,it,vi} from 'vitest';
import {GlareSourceAttempt,type GlareSourceAttemptRecord} from './glare-source-attempt';
import {freezeGlareSourceIntent,verifyGlareSourceRecovery,type GlareSourceIntent} from './glare-source-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():GlareSourceIntent=>({schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,source_observation_id:id(3),params_proposal:{schema_version:1,mode:'generative',base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},donor:null,donor_faces:null,strength:.5,glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),model_lock_sha:'d'.repeat(64),calibration:ref(14)}});
const ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued' as const};
it('persists source-only intent before POST and retries the identical request after ambiguity',async()=>{
 const writes:GlareSourceAttemptRecord[]=[],store={write:async(r:GlareSourceAttemptRecord)=>{writes.push(structuredClone(r));}};
 const original=body(),attempt=new GlareSourceAttempt(id(20),'source:stable',original,store);original.params_proposal.strength=1;
 const send=vi.fn().mockImplementationOnce(async()=>{expect(writes).toHaveLength(1);throw new Error('network');}).mockResolvedValue(ticket);
 await expect(attempt.submit(send)).rejects.toThrow('network');
 const restored=GlareSourceAttempt.restore(writes[0],store);await expect(restored.submit(send)).resolves.toEqual(ticket);
 expect(send.mock.calls[0][1]).toEqual(send.mock.calls[1][1]);expect(send.mock.calls[0][2]).toBe(send.mock.calls[1][2]);
 expect(writes[0].body.params_proposal.strength).toBe(.5);expect(writes.at(-1)?.ticket).toEqual(ticket);
 await expect(restored.submit(send)).resolves.toEqual(ticket);expect(send).toHaveBeenCalledTimes(2);
});
it('retains a late accepted ticket after cancellation without repeating POST',async()=>{
 const abort=new AbortController(),writes:GlareSourceAttemptRecord[]=[];
 const attempt=new GlareSourceAttempt(id(20),'source:late',body(),{write:async r=>{writes.push(structuredClone(r));}});
 await expect(attempt.submit(async()=>{abort.abort();return ticket;},abort.signal)).rejects.toThrow('GLARE_SOURCE_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect(writes.at(-1)?.ticket).toEqual(ticket);
 const restored=GlareSourceAttempt.restore(writes.at(-1)!,{write:async()=>{throw new Error('unexpected write');}});
 await expect(restored.submit(async()=>{throw new Error('unexpected POST');})).resolves.toEqual(ticket);
});
it('rejects donor authority and changed immutable recovery source',()=>{
 const donor=body();(donor.params_proposal as unknown as {donor:unknown}).donor=donor.params_proposal.base;
 expect(()=>freezeGlareSourceIntent(donor)).toThrow('GLARE_SOURCE_INTENT_INVALID');
 const missing=body();delete (missing.params_proposal as Partial<typeof missing.params_proposal>).donor_faces;
 expect(()=>freezeGlareSourceIntent(missing)).toThrow('GLARE_SOURCE_INTENT_INVALID');
 const original=body(),live=structuredClone(original.params_proposal.base);live.source.sha256='e'.repeat(64);
 expect(()=>verifyGlareSourceRecovery(original,live)).toThrow('GLARE_SOURCE_RECOVERY_CHANGED');
});

it('admits recovery only after immutable persistence and keeps known acceptance if its write fails',async()=>{
 const receipt=ticket;let writes=0,sends=0;
 const attempt=new GlareSourceAttempt(id(20),'recovery:proof',body(),{write:async()=>{if(++writes===1)throw new Error('initial storage failed');if(writes===3)throw new Error('accepted storage failed');}});
 expect(attempt.canReplay).toBe(false);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('initial storage failed');
 expect(attempt.canReplay).toBe(false);expect(sends).toBe(0);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('accepted storage failed');
 expect(attempt.canReplay).toBe(true);expect(attempt.state).toBe('accepted');
 await expect(attempt.submit(async()=>{throw new Error('must not repeat accepted POST');})).resolves.toEqual(receipt);
 expect(sends).toBe(1);
 const restored=GlareSourceAttempt.restore({...attempt.snapshot,ticket:null},{write:async()=>{}});
 expect(restored.state).toBe('unknown');expect(restored.canReplay).toBe(true);
});
