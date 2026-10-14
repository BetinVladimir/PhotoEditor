import {withPerspectiveDetectionDeadline} from './perspective-detection-deadline';
import type {OwnerAPI} from './owner-api';
import {readPerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionIntent,freezePerspectiveLinesSubmission} from './perspective-lines-submission';
import {readPerspectiveDetectedLines} from './perspective-detected-lines';
export type PerspectiveDetectionStatus='pending'|'available'|'unsupported'|'failed'|'cancelled';
export function canLoadDetectedPerspectiveLines(task:string|undefined,receipt:PerspectiveLinesSubmissionReceipt|null,status:PerspectiveDetectionStatus|'paused'|'draft_expired'|null,externalTask:string|undefined):boolean{
 return !!task&&(receipt!==null?task===receipt.task_id&&status==='available':task===externalTask);
}
export async function observePerspectiveDetection(api:Pick<OwnerAPI,'get'|'perspectiveDetectedLines'>,tenant:string,project:string,request:PerspectiveLinesSubmissionIntent,receipt:PerspectiveLinesSubmissionReceipt,signal:AbortSignal):Promise<PerspectiveDetectionStatus>{
 const body=freezePerspectiveLinesSubmission(tenant,project,request),owned=readPerspectiveLinesSubmissionReceipt(receipt);
 const active=()=>{if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');};active();
 const status=await withPerspectiveDetectionDeadline(signal,15_000,child=>api.get<{run_id:string;workflow_id:string;state:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(owned.workflow_id)}`,child));active();
 if(!status||status.run_id!==owned.run_id||status.workflow_id!==owned.workflow_id)throw new Error('PERSPECTIVE_DETECTION_SCOPE_CHANGED');
 if(['draft','queued','running','cancelling'].includes(status.state))return 'pending';
 if(status.state==='failed')return 'failed';
 if(status.state==='cancelled')return 'cancelled';
 if(status.state!=='waiting_review'&&status.state!=='completed')throw new Error('PERSPECTIVE_DETECTION_STATUS_INVALID');
 const value=await withPerspectiveDetectionDeadline(signal,15_000,child=>api.perspectiveDetectedLines(project,owned.task_id,body.base,child));active();
 const result=readPerspectiveDetectedLines(value,tenant,project,owned.task_id,body.base);
 return result.detection.status;
}
