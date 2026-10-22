import {expect,it} from 'vitest';
import {NegativeAttempt} from './negative-attempt';
import {freezeNegativeIntent,type NegativeIntent} from './negative-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const body=():NegativeIntent=>({schema_version:1,snapshot_id:id(1),params:{schema_version:1,base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},strength:1,film_base_rgb:[1,.8,.6],calibration:{artifact_id:id(4),sha256:'c'.repeat(64)},exposure_ev:0}});
it('owns original intent and preserves key/body after an ambiguous response',async()=>{
 const input=body(),writes:unknown[]=[],calls:unknown[]=[];
 const attempt=new NegativeAttempt(id(5),'negative-stable-key',input,{write:async r=>{writes.push(structuredClone(r))}});
 input.params.film_base_rgb[0]=.1;
 await expect(attempt.submit(async(project,b,key)=>{calls.push({project,b,key});b.params.strength=0;throw new Error('response lost')})).rejects.toThrow('response lost');
 expect(attempt.state).toBe('unknown');expect(attempt.snapshot.body.params.film_base_rgb[0]).toBe(1);
 const ticket={run_id:id(6),workflow_id:id(7),state:'queued' as const};
 await attempt.submit(async(project,b,key)=>{expect(writes.length).toBe(2);calls.push({project,b,key});return ticket});
 expect(attempt.state).toBe('accepted');expect(attempt.snapshot.key).toBe('negative-stable-key');
 expect(await attempt.submit(async()=>{throw new Error('must not resubmit')})).toEqual(ticket);
 const cancelled=new AbortController();cancelled.abort();
 await expect(attempt.submit(async()=>{throw new Error('cancelled send');},cancelled.signal)).rejects.toThrow('NEGATIVE_ATTEMPT_CANCELLED');
 expect((calls[1] as {b:NegativeIntent}).b.params.strength).toBe(1);
});
it('restores a persisted ambiguous request without changing its identity',()=>{
 const attempt=new NegativeAttempt(id(5),'negative-stable-key',body(),{write:async()=>{}});
 const restored=NegativeAttempt.restore(attempt.snapshot,{write:async()=>{}});
 expect(restored.state).toBe('unknown');expect(restored.snapshot).toEqual(attempt.snapshot);
});
it.each(['strength','exposure','film','calibration','extra'])('rejects invalid negative intent: %s',fault=>{
 const input=body();
 if(fault==='strength')input.params.strength=NaN;
 if(fault==='exposure')input.params.exposure_ev=3.01;
 if(fault==='film')input.params.film_base_rgb[1]=0;
 if(fault==='calibration')input.params.calibration.sha256='bad';
 if(fault==='extra')Object.assign(input,{capacity:{}});
 expect(()=>freezeNegativeIntent(input)).toThrow('NEGATIVE_INTENT_INVALID');
});
it('never sends before durable persistence succeeds',async()=>{
 let sent=false;const attempt=new NegativeAttempt(id(5),'negative-stable-key',body(),{write:async()=>{throw new Error('storage unavailable')}});
 await expect(attempt.submit(async()=>{sent=true;return {run_id:id(6),workflow_id:id(7),state:'queued'}})).rejects.toThrow('storage unavailable');expect(sent).toBe(false);
});
it('keeps the accepted ticket but hides it from an aborted caller',async()=>{
 const signal=new AbortController(),ticket={run_id:id(6),workflow_id:id(7),state:'queued' as const},writes:unknown[]=[];
 const attempt=new NegativeAttempt(id(5),'negative-stable-key',body(),{write:async record=>{writes.push(record);}});
 await expect(attempt.submit(async()=>{signal.abort();return ticket;},signal.signal)).rejects.toThrow('NEGATIVE_ATTEMPT_CANCELLED');
 expect(attempt.state).toBe('accepted');expect((writes.at(-1) as {ticket:unknown}).ticket).toEqual(ticket);
 await expect(attempt.submit(async()=>{throw new Error('resubmitted');})).resolves.toEqual(ticket);
});
it('owns the accepted ticket across a mutating persistence callback',async()=>{
 const ticket={run_id:id(6),workflow_id:id(7),state:'queued' as const};
 const attempt=new NegativeAttempt(id(5),'negative-stable-key',body(),{write:async record=>{if(record.ticket)record.ticket.run_id=id(99);}});
 await expect(attempt.submit(async()=>ticket)).resolves.toEqual(ticket);
 expect(attempt.snapshot.ticket).toEqual(ticket);
});

it('admits recovery only after immutable persistence and keeps known acceptance if its write fails',async()=>{
 const receipt={run_id:id(6),workflow_id:id(7),state:'queued' as const};let writes=0,sends=0;
 const attempt=new NegativeAttempt(id(5),'recovery:proof',body(),{write:async()=>{if(++writes===1)throw new Error('initial storage failed');if(writes===3)throw new Error('accepted storage failed');}});
 expect(attempt.canReplay).toBe(false);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('initial storage failed');
 expect(attempt.canReplay).toBe(false);expect(sends).toBe(0);
 await expect(attempt.submit(async()=>{sends++;return receipt;})).rejects.toThrow('accepted storage failed');
 expect(attempt.canReplay).toBe(true);expect(attempt.state).toBe('accepted');
 await expect(attempt.submit(async()=>{throw new Error('must not repeat accepted POST');})).resolves.toEqual(receipt);
 expect(sends).toBe(1);
 const restored=NegativeAttempt.restore({...attempt.snapshot,ticket:null},{write:async()=>{}});
 expect(restored.state).toBe('unknown');expect(restored.canReplay).toBe(true);
});
