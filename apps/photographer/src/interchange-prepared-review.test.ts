import {describe,expect,it} from 'vitest';
import {freezePreparedReviewDecision,freezeSavedPreparedRevoke,readPreparedReviewReceipt,readPreparedReviews,readSavedPreparedReviews} from './interchange-prepared-review';

const id=(n:number)=>`00000000-0000-0000-0000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:String(n).padStart(64,'a')});
const candidate={id:'f'.repeat(64),decision:'pending',bbox:{x:10,y:20,width:5,height:6},score:0.9};
const item={entry_id:id(1),target_version_id:id(2),kind:'defects',proposal:ref(3),base:ref(4),document:{version:'defect_candidates_v1',image:ref(4),label_map:ref(5),canvas:{width:100,height:100},candidates:[candidate]},decision_epoch:0};

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
});
