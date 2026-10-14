import {it,expect} from 'vitest';
import {canLoadDetectedPerspectiveLines,observePerspectiveDetection} from './perspective-detection-status';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body={schema_version:1 as const,snapshot_id:id(3),base:{asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)}};
const ticket={run_id:id(6),workflow_id:id(7),task_id:id(8),state:'queued' as const};
it('offers detected lines only after an owned task is available',()=>{
 for(const status of [null,'pending','paused','unsupported','failed','cancelled','draft_expired'] as const)
  expect(canLoadDetectedPerspectiveLines(ticket.task_id,ticket,status,undefined)).toBe(false);
 expect(canLoadDetectedPerspectiveLines(ticket.task_id,ticket,'available',undefined)).toBe(true);
 expect(canLoadDetectedPerspectiveLines(id(9),ticket,'available',id(9))).toBe(false);
 expect(canLoadDetectedPerspectiveLines(ticket.task_id,null,null,ticket.task_id)).toBe(true);
 expect(canLoadDetectedPerspectiveLines(ticket.task_id,null,null,id(9))).toBe(false);
 expect(canLoadDetectedPerspectiveLines(undefined,ticket,'available',undefined)).toBe(false);
});
it('does not read result pixels/descriptor for pending or failed workflows',async()=>{
 for(const [state,expected] of [['queued','pending'],['running','pending'],['failed','failed'],['cancelled','cancelled']]){
 const api={get:async()=>({run_id:id(6),workflow_id:id(7),state}),perspectiveDetectedLines:async()=>{throw new Error('unexpected result read');}} as unknown as Pick<OwnerAPI,'get'|'perspectiveDetectedLines'>;
 expect(await observePerspectiveDetection(api,id(1),id(2),body,ticket,new AbortController().signal)).toBe(expected);
 }
});
it('rejects foreign workflow scope and late cancellation',async()=>{
 const c=new AbortController();const api={get:async()=>{c.abort();return {run_id:id(6),workflow_id:id(7),state:'queued'};}} as unknown as Pick<OwnerAPI,'get'|'perspectiveDetectedLines'>;
 await expect(observePerspectiveDetection(api,id(1),id(2),body,ticket,c.signal)).rejects.toThrow();
 const foreign={get:async()=>({run_id:id(9),workflow_id:id(7),state:'completed'})} as unknown as Pick<OwnerAPI,'get'|'perspectiveDetectedLines'>;
 await expect(observePerspectiveDetection(foreign,id(1),id(2),body,ticket,new AbortController().signal)).rejects.toThrow();
});
