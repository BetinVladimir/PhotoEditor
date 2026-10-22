import {describe,it,expect} from 'vitest';
import {readPerspectiveDetectedLines,detectedPerspectiveLinesIntent,type PerspectiveDetectedLines} from './perspective-detected-lines';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture():PerspectiveDetectedLines{return {schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:{artifact_id:id(5),sha256:'a'.repeat(64)},detection:{schema_version:1,base:{asset_version_id:id(6),source:{artifact_id:id(7),sha256:'b'.repeat(64)},canvas_hash:'c'.repeat(64)},status:'available',lines:{schema_version:1,lines:[
 {a:{x:.1,y:.1},b:{x:.9,y:.1},family:'horizontal'},{a:{x:.1,y:.9},b:{x:.9,y:.9},family:'horizontal'},
 {a:{x:.1,y:.1},b:{x:.1,y:.9},family:'vertical'},{a:{x:.9,y:.1},b:{x:.9,y:.9},family:'vertical'}]}}};}
const read=(v:unknown)=>readPerspectiveDetectedLines(v,id(1),id(2),id(3),fixture().detection.base);
describe('detected perspective lines',()=>{
 it('owns lines and builds editable publication intent',()=>{const value=fixture(),owned=read(value);value.detection.lines!.lines[0].a.x=.4;expect(owned.detection.lines!.lines[0].a.x).toBe(.1);const intent=detectedPerspectiveLinesIntent(owned,id(8));expect(intent.snapshot_id).toBe(id(8));expect(intent.base).toEqual(owned.detection.base);intent.line_segments.lines.pop();expect(owned.detection.lines!.lines).toHaveLength(4);});
 it('preserves unsupported without inventing lines',()=>{const value=fixture();value.detection.status='unsupported';value.detection.lines=null;expect(read(value).detection.status).toBe('unsupported');expect(()=>detectedPerspectiveLinesIntent(value,id(8))).toThrow();});
 it('refuses foreign scope/source and output aliases',()=>{for(const mutate of [
  (v:PerspectiveDetectedLines)=>{v.tenant_id=id(9);},(v:PerspectiveDetectedLines)=>{v.project_id=id(9);},
  (v:PerspectiveDetectedLines)=>{v.producer_task_id=id(9);},(v:PerspectiveDetectedLines)=>{v.detection.base.canvas_hash='d'.repeat(64);},
  (v:PerspectiveDetectedLines)=>{v.result.artifact_id=v.detection.base.source.artifact_id;},
 ]){const value=fixture();mutate(value);expect(()=>read(value)).toThrow();}});
 it('refuses status mismatch, missing families and non-separated evidence',()=>{const value=fixture();value.detection.status='unsupported';expect(()=>read(value)).toThrow();const missing=fixture();missing.detection.lines!.lines.pop();expect(()=>read(missing)).toThrow();const narrow=fixture();narrow.detection.lines!.lines[1]={a:{x:.1,y:.11},b:{x:.9,y:.11},family:'horizontal'};expect(()=>read(narrow)).toThrow();});
});

it('refuses a late detected-lines response after caller cancellation',async()=>{
 const {OwnerAPI}=await import('./owner-api');const controller=new AbortController();let resolve!:(value:unknown)=>void;
 const api=new OwnerAPI({request:()=>new Promise<unknown>(done=>{resolve=done;})} as never,id(1));
 const pending=api.perspectiveDetectedLines(id(2),id(3),fixture().detection.base,controller.signal);
 controller.abort();resolve(fixture());await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
