import type {OwnerAPI} from './owner-api';
import {freezePerspectiveLinesIntent,type PerspectiveLinesIntent} from './perspective-lines';
import {detectedPerspectiveLinesIntent,readPerspectiveDetectedLines} from './perspective-detected-lines';
export async function loadDetectedPerspectiveLines(api:Pick<OwnerAPI,'perspectiveDetectedLines'>,project:string,producer:string,request:PerspectiveLinesIntent,signal:AbortSignal):Promise<PerspectiveLinesIntent>{
 const owned=freezePerspectiveLinesIntent(request);
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const detection=await api.perspectiveDetectedLines(project,producer,owned.base,signal);
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const checked=readPerspectiveDetectedLines(detection,detection.tenant_id,project,producer,owned.base);
 return detectedPerspectiveLinesIntent(checked,owned.snapshot_id);
}
