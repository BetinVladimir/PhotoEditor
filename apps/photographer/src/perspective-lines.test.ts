import {describe,it,expect} from 'vitest';
import {canPublishPerspectiveLines,freezePerspectiveLinesIntent,readPerspectiveLinesReceipt,perspectiveAutoIntent,type PerspectiveLinesIntent} from './perspective-lines';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const fixture=():PerspectiveLinesIntent=>({schema_version:1,snapshot_id:id(1),base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},line_segments:{schema_version:1,lines:[{a:{x:0,y:.2},b:{x:1,y:.2},family:'horizontal'},{a:{x:0,y:.8},b:{x:1,y:.8},family:'horizontal'},{a:{x:.2,y:0},b:{x:.2,y:1},family:'vertical'},{a:{x:.8,y:0},b:{x:.8,y:1},family:'vertical'}]}});
describe('perspective line authoring',()=>{
 it('does not publish untouched guide lines, but allows authored lines and exact-body retries',()=>{
  expect(canPublishPerspectiveLines(false,false)).toBe(false);
  expect(canPublishPerspectiveLines(true,false)).toBe(true);
  expect(canPublishPerspectiveLines(false,true)).toBe(true);
 });
 it('owns authored endpoints and binds the receipt to the original source',()=>{
 const original=fixture(),owned=freezePerspectiveLinesIntent(original);original.line_segments.lines[0].a.x=.3;expect(owned.line_segments.lines[0].a.x).toBe(0);
 const receipt=readPerspectiveLinesReceipt({schema_version:1,tenant_id:id(4),project_id:id(5),snapshot_id:owned.snapshot_id,base:owned.base,lines:{artifact_id:id(6),sha256:'c'.repeat(64)}},id(4),id(5),owned);
 expect(perspectiveAutoIntent(receipt,.5,.2).params).toMatchObject({mode:'auto',corners:null,strength:.5,max_crop_fraction:.2});
 expect(()=>readPerspectiveLinesReceipt({...receipt,snapshot_id:id(7)},id(4),id(5),owned)).toThrow();
 expect(()=>readPerspectiveLinesReceipt({...receipt,lines:owned.base.source},id(4),id(5),owned)).toThrow();
 });
 it('rejects malformed line families, endpoints and authority fields',()=>{
 const empty=fixture();empty.line_segments.lines[0].b={...empty.line_segments.lines[0].a};expect(()=>freezePerspectiveLinesIntent(empty)).toThrow();
 const invalid=fixture();invalid.line_segments.lines[0].a.x=NaN;expect(()=>freezePerspectiveLinesIntent(invalid)).toThrow();
 const families=fixture();families.line_segments.lines[0].family='vertical';expect(()=>freezePerspectiveLinesIntent(families)).toThrow();
 expect(()=>freezePerspectiveLinesIntent({...fixture(),object_key:'foreign'} as PerspectiveLinesIntent)).toThrow();
 });
});
