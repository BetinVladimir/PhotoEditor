import {expect,it} from 'vitest';
import {buildSceneBoard} from './scene-board';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const sha='a'.repeat(64);
function fixture(){const binding={snapshotId:id(1),snapshotDigest:sha,policySha:sha,frames:[0,1,2].map(ordinal=>({ordinal,versionId:id(10+ordinal),sourceSha:sha}))};const group=(n:number,ordinals:number[])=>({id:id(20+n),ordinals,reference_ordinal:ordinals[0],confidence:.8,corrections:{artifact_id:id(30+n),sha256:sha}});return {binding,plan:{schema_version:1,snapshot_id:id(1),snapshot_digest:sha,policy_sha:sha,groups:[group(0,[2]),group(1,[0,1])]}};}
it('builds stable groups and references without claiming unavailable outliers',()=>{
 const {binding,plan}=fixture();const board=buildSceneBoard(plan,binding);expect(board.groups.map(group=>group.ordinals)).toEqual([[0,1],[2]]);expect(board.groups[0].reference.versionId).toBe(id(10));expect(board.groups[0].outliers).toBeNull();binding.frames[0].sourceSha='b'.repeat(64);plan.groups[1].ordinals.push(2);expect(board.groups[0].frames[0].sourceSha).toBe(sha);expect(board.groups[0].ordinals).toEqual([0,1]);
});
it('rejects changed snapshot, incomplete or overlapping partitions and invalid references',()=>{
 const patches=[(x:ReturnType<typeof fixture>)=>{x.plan.snapshot_digest='b'.repeat(64);},(x:ReturnType<typeof fixture>)=>{x.plan.groups.pop();},(x:ReturnType<typeof fixture>)=>{x.plan.groups[0].ordinals=[1,2];},(x:ReturnType<typeof fixture>)=>{x.plan.groups[0].reference_ordinal=0;},(x:ReturnType<typeof fixture>)=>{x.plan.groups[0].confidence=NaN;}];for(const patch of patches){const x=fixture();patch(x);expect(()=>buildSceneBoard(x.plan,x.binding)).toThrow('SCENE_BOARD_INVALID');}
});
it('handles all 5000 frozen frame bindings without fetching image data',()=>{
 const {binding,plan}=fixture();binding.frames=Array.from({length:5000},(_,ordinal)=>({ordinal,versionId:id(100+ordinal),sourceSha:sha}));plan.groups=[{...plan.groups[0],ordinals:binding.frames.map(frame=>frame.ordinal),reference_ordinal:4999}];const board=buildSceneBoard(plan,binding);expect(board.groups[0].frames).toHaveLength(5000);expect(board.groups[0].reference.ordinal).toBe(4999);
});
