import type {V2VersionBinding} from './catalog-types';
import {perspectiveProposalScope} from './perspective-proposal';
export type PerspectiveLinesSubmissionIntent={schema_version:1;snapshot_id:string;base:V2VersionBinding};
export type PerspectiveLinesSubmissionReceipt={run_id:string;workflow_id:string;task_id:string;state:'queued'|'running'|'waiting_review'|'completed'};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
export function freezePerspectiveLinesSubmission(tenant:string,project:string,value:PerspectiveLinesSubmissionIntent):PerspectiveLinesSubmissionIntent{
 if(!fields(value,['schema_version','snapshot_id','base'])||value.schema_version!==1||!uuid(value.snapshot_id))throw new Error('PERSPECTIVE_DETECTION_INTENT_INVALID');
 const scope=perspectiveProposalScope(tenant,project,value.snapshot_id,value.base);
 return {schema_version:1,snapshot_id:value.snapshot_id,base:structuredClone(scope.base)};
}
export function readPerspectiveLinesSubmissionReceipt(value:unknown):PerspectiveLinesSubmissionReceipt{
 if(!fields(value,['run_id','workflow_id','task_id','state'])||!uuid(value.run_id)||!uuid(value.workflow_id)||!uuid(value.task_id)||!['queued','running','waiting_review','completed'].includes(String(value.state)))throw new Error('PERSPECTIVE_DETECTION_RECEIPT_INVALID');
 if(value.run_id===value.workflow_id||value.run_id===value.task_id||value.workflow_id===value.task_id)throw new Error('PERSPECTIVE_DETECTION_RECEIPT_INVALID');
 return structuredClone(value) as PerspectiveLinesSubmissionReceipt;
}
