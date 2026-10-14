import {it,expect} from 'vitest';
import {handoffPerspectiveLines} from './perspective-lines-handoff';
import {PerspectiveLinesAttempt} from './perspective-lines-attempt';
import type {PerspectiveLinesIntent} from './perspective-lines';
import type {PerspectiveAttemptRecord} from './perspective-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body:PerspectiveLinesIntent={schema_version:1,snapshot_id:id(1),base:{asset_version_id:id(2),source:{artifact_id:id(3),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},line_segments:{schema_version:1,lines:[{a:{x:0,y:.2},b:{x:1,y:.2},family:'horizontal'},{a:{x:0,y:.8},b:{x:1,y:.8},family:'horizontal'},{a:{x:.2,y:0},b:{x:.2,y:1},family:'vertical'},{a:{x:.8,y:0},b:{x:.8,y:1},family:'vertical'}]}};
it('persists an auto job before clearing line publication and reuses it after interrupted handoff',async()=>{
 const lineAttempt=new PerspectiveLinesAttempt(id(4),id(5),'line-key',body,{write:async()=>{}});
 await lineAttempt.submit(async()=>({schema_version:1,tenant_id:id(4),project_id:id(5),snapshot_id:body.snapshot_id,base:body.base,lines:{artifact_id:id(6),sha256:'c'.repeat(64)}}));
 let saved:PerspectiveAttemptRecord|null=null;const events:string[]=[];
 const jobs={read:async()=>saved,write:async(record:PerspectiveAttemptRecord)=>{events.push('save');saved=record;}};
 await expect(handoffPerspectiveLines(lineAttempt.snapshot,jobs,{clear:async()=>{events.push('clear');throw new Error('interrupted');}},.5,.2,new AbortController().signal)).rejects.toThrow('interrupted');
 expect(events).toEqual(['save','clear']);const key=saved!.key;
 const restored=await handoffPerspectiveLines(lineAttempt.snapshot,jobs,{clear:async()=>{events.push('clear');}},1,.5,new AbortController().signal);
 expect(restored.snapshot.key).toBe(key);expect(restored.snapshot.body.params.strength).toBe(.5);expect(events).toEqual(['save','clear','clear']);
});
it('refuses unresolved publication without touching job storage',async()=>{
 const record=new PerspectiveLinesAttempt(id(4),id(5),'line-key',body,{write:async()=>{}}).snapshot;
 await expect(handoffPerspectiveLines(record,{read:async()=>{throw new Error('unexpected IO');},write:async()=>{}},{clear:async()=>{}},1,.5,new AbortController().signal)).rejects.toThrow('UNRESOLVED');
});
