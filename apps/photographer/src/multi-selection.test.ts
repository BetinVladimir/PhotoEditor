import {describe,it,expect} from 'vitest';
import {MultiSelection} from './multi-selection';
import {readMultiCapabilities,type MultiCapabilities} from './multi-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const frame=(n:number)=>({asset_version_id:id(n),source:{artifact_id:id(n+1),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)});
const capabilities=():MultiCapabilities=>({schema_version:1,tenant_id:id(1),project_id:id(2),operation:'optical.panorama',calibration:{artifact_id:id(3),sha256:'c'.repeat(64)},max_pixels:200_000_000,frame_limit:20});
describe('multi ordered selection',()=>{
 it('keeps reference identity when frames move and owns bindings',()=>{
  const caps=capabilities(),selection=new MultiSelection(caps),first=frame(4);
  selection.add(first);selection.add(frame(6));selection.selectReference(id(6));selection.move(id(6),0);
  caps.calibration.sha256='d'.repeat(64);first.source.sha256='e'.repeat(64);
  const intent=selection.build(id(10));expect(intent.params.reference_ordinal).toBe(0);expect(intent.params.frames.map(f=>f.asset_version_id)).toEqual([id(6),id(4)]);expect(intent.params.frames[1].source.sha256).toBe('a'.repeat(64));expect(intent.params.calibration.sha256).toBe('c'.repeat(64));
  intent.params.frames.reverse();expect(selection.snapshot.frames[0].asset_version_id).toBe(id(6));
  selection.remove(id(6));expect(selection.snapshot.reference_version).toBe(id(4));expect(()=>selection.build(id(10))).toThrow();
 });
 it('enforces operation options, frame caps and unique versions',()=>{
  const selection=new MultiSelection(capabilities());for(let n=0;n<20;n++)selection.add(frame(20+n*2));
  expect(()=>selection.add(frame(80))).toThrow();expect(()=>selection.add(frame(20))).toThrow();expect(()=>selection.selectReference(id(99))).toThrow();expect(()=>selection.move(id(20),20)).toThrow();expect(()=>selection.setMotionPolicy('reject')).toThrow();
  const caps=capabilities();caps.operation='optical.focus_stack';caps.frame_limit=30;
  const focus=new MultiSelection(caps);focus.add(frame(4));focus.add(frame(6));focus.setMotionPolicy('reject');expect(focus.build(id(10)).params).toHaveProperty('motion_policy','reject');expect(()=>focus.setProjection('planar')).toThrow();
 });
 it('rejects foreign capabilities scope and unsupported limits',()=>{
  const caps=capabilities();expect(()=>readMultiCapabilities(caps,id(9),id(2),'optical.panorama')).toThrow();
  caps.frame_limit=30;expect(()=>new MultiSelection(caps)).toThrow();
 });
});
