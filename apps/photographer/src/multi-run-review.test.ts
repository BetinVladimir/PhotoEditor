import {expect,it} from 'vitest';
import {readMultiRunReview} from './multi-run-review';
import {multiWorkflowID,multiProducerTask} from './multi-result';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
it.each(['optical.panorama','optical.focus_stack'] as const)('requires the exact immutable %s history scope',operation=>{
 const workflow=multiWorkflowID(id(3));
 const scope={tenant_id:id(1),project_id:id(2),reference_asset_id:id(9),run_id:id(3),workflow_id:workflow,task_id:multiProducerTask(workflow,operation),operation,intent:{schema_version:1,snapshot_id:id(4),params:{schema_version:1,frames:[{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)}],reference_ordinal:0,max_pixels:10000,calibration:ref(10),...(operation==='optical.panorama'?{projection:'planar'}:{motion_policy:'review'})}}};
 expect(readMultiRunReview({scope},id(1),id(3))).toEqual(scope);
 for(const patch of [{tenant_id:id(99)},{run_id:id(99)},{workflow_id:id(99)},{task_id:id(99)},{reference_asset_id:''},{extra:true},{intent:{schema_version:1}}])expect(()=>readMultiRunReview({scope:{...scope,...patch}},id(1),id(3))).toThrow();
 expect(readMultiRunReview({scope:null},id(1),id(3))).toBeNull();expect(()=>readMultiRunReview({scope,extra:true},id(1),id(3))).toThrow();
});
