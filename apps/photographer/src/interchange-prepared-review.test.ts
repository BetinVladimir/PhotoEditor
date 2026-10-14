import {describe,expect,it} from 'vitest';
import {freezePreparedReviewDecision,freezeSavedPreparedRevoke,projectDustCandidateToCrop,readPreparedReviewReceipt,readPreparedReviews,readSavedPreparedReviews} from './interchange-prepared-review';

const id=(n:number)=>`00000000-0000-0000-0000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:String(n).padStart(64,'a')});
const candidate={id:'f'.repeat(64),decision:'pending',bbox:{x:10,y:20,width:5,height:6},score:0.9};
const item={entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),document:{version:'defect_candidates_v1',image:ref(4),label_map:ref(5),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0};
const dust={entry_id:id(1),target_version_id:id(2),kind:'dust',proposal:ref(30),base:ref(4),document:{algorithm:'sensor_dust_log_v1',camera_id:`raw-${'f'.repeat(64)}`,sensor_width:420,sensor_height:320,raw_crop:{x:10,y:10,width:400,height:300},frames:[ref(4),ref(5),ref(6),ref(7),ref(8)],candidates:[{id:'e'.repeat(64),decision:'pending',sensor_x:12.5,sensor_y:18.5,radius_sensor_px:2,observed_frames:5,eligible_frames:5,recurrence:1}],warnings:[]},decision_epoch:0};

describe('prepared import review contract',()=>{
 it('binds displayed proposal and candidate ids to a CAS grant',()=>{
  const [prepared]=readPreparedReviews({workflow_id:id(9),items:[item]},id(9));
  const request=freezePreparedReviewDecision(prepared,'grant','accept',[candidate.id],[]);
  expect(request).toMatchObject({action:'grant',proposal:ref(3),base:ref(4),label_map:ref(5),expected_epoch:0,accepted_candidate_ids:[candidate.id]});
  expect(readPreparedReviewReceipt({workflow_id:id(9),entry_id:id(1),kind:'defects',target_version_id:id(2),proposal_id:id(3),status:'granted',epoch:1},id(9),request).epoch).toBe(1);
 });
 it('rejects foreign proposal data and candidate ids',()=>{
  expect(()=>readPreparedReviews({workflow_id:id(9),items:[{...item,document:{...item.document,image:ref(8)}}]},id(9))).toThrow();
  const [prepared]=readPreparedReviews({workflow_id:id(9),items:[item]},id(9));
  expect(()=>freezePreparedReviewDecision(prepared,'grant','accept',['e'.repeat(64)],[])).toThrow();
  expect(()=>freezePreparedReviewDecision(prepared,'revoke')).toThrow();
 });
 it('builds a revoke from stored refs after proposal bytes are unavailable',()=>{
  const saved={entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),label_map:ref(5),decision:'accept',accepted_candidate_ids:[candidate.id],rejected_candidate_ids:[],status:'granted',epoch:3};
  const [row]=readSavedPreparedReviews({workflow_id:id(9),items:[saved]},id(9));
  expect(freezeSavedPreparedRevoke(row)).toEqual({action:'revoke',kind:'defects',entry_id:id(1),target_version_id:id(2),proposal:ref(3),base:ref(4),label_map:ref(5),expected_epoch:3});
  expect(()=>readSavedPreparedReviews({workflow_id:id(9),items:[{...saved,label_map:ref(8),kind:'dust'}]},id(9))).toThrow();
 });
 it('requires the complete bounded dust series before offering candidate decisions',()=>{
  expect(readPreparedReviews({workflow_id:id(9),items:[dust]},id(9))[0].document.candidates).toHaveLength(1);
  for(const change of [
   (d:typeof dust)=>{d.document.camera_id='raw-invalid';},
   (d:typeof dust)=>{d.document.sensor_width=100;},
   (d:typeof dust)=>{d.document.frames[1]=d.document.frames[0];},
   (d:typeof dust)=>{d.document.frames.pop();},
   (d:typeof dust)=>{d.document.candidates[0].radius_sensor_px=33;},
   (d:typeof dust)=>{d.document.candidates[0].observed_frames=4;},
   (d:typeof dust)=>{d.document.warnings=['no_repeated_dust'];},
  ]){
   const changed=JSON.parse(JSON.stringify(dust)) as typeof dust;
   change(changed);
   expect(()=>readPreparedReviews({workflow_id:id(9),items:[changed]},id(9))).toThrow();
  }
 });
 it('projects sensor circles only into their own RAW crop and clips edges',()=>{
  const crop={x:10,y:20,width:400,height:300};
  const middle=projectDustCandidateToCrop(crop,{id:'f'.repeat(64),decision:'pending',sensor_x:210,sensor_y:170,radius_sensor_px:10});
  expect(middle.left).toBe(47.5);expect(middle.top).toBeCloseTo(140/3);expect(middle.width).toBe(5);expect(middle.height).toBeCloseTo(20/3);
  const edge=projectDustCandidateToCrop(crop,{id:'f'.repeat(64),decision:'pending',sensor_x:11,sensor_y:21,radius_sensor_px:10});
  expect(edge.left).toBe(0);expect(edge.top).toBe(0);expect(edge.width).toBe(2.75);expect(edge.height).toBeCloseTo(11/3);
  expect(()=>projectDustCandidateToCrop(crop,{id:'f'.repeat(64),decision:'pending',sensor_x:411,sensor_y:21,radius_sensor_px:1})).toThrow();
 });
});
