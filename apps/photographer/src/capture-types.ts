export interface CaptureShotRequirement {
  id: string;
  label: string;
  person_ids: string[];
  min_confirmed_frames: number;
  min_quality: number;
}
export interface CaptureShotListView {
  session_id: string;
  revision: string;
  document: {artifact_id: string; sha256: string} | null;
  requirements: CaptureShotRequirement[];
}
export interface CaptureAlert {
  confirmation_id?: string | null;
  id: string;
  revision: string;
  session_id: string;
  asset_version_id: string;
  report_artifact_id: string;
  receipt_id: string | null;
  severity: 'info' | 'warning' | 'critical';
  qa_stale: boolean;
  created_at: string;
  acknowledged_by: string | null;
  acknowledged_at: string | null;
}
export interface CaptureFrameConfirmation {
  confirmation_id: string;
  receipt_id: string;
  alert_id: string;
}
export interface CapturePerson {id:string;label:string;state:'active'|'revoked';}
export interface CaptureArtifactRef {artifact_id:string;sha256:string;}
export interface ShootSessionPolicy {
 schema_version:1;
 time_gap_seconds:number;
 neighbor_count:number;
 protect_manual_picks:boolean;
 camera_offsets:Array<{camera_hash:string;seconds:number}>;
 calibration:CaptureArtifactRef;
}
export interface ShootCapturePolicy {
 schema_version:1;
 checks:Array<'focus'|'eyes'|'clipping'|'glare'|'duplicate'|'shot_list'>;
 max_queue_age_seconds:30;
 calibration:CaptureArtifactRef;
}
export interface ShootSessionCreate {
 project_id:string;
 collection_id:string;
 name:string;
 policy:ShootSessionPolicy;
 capture_policy:ShootCapturePolicy|null;
}
export interface ShootSession {
 id:string;project_id:string;collection_id:string;name:string;
 state:'open'|'closed'|'archived';revision:string;
 policy:ShootSessionPolicy;capture_policy:ShootCapturePolicy|null;
}
export interface ShootSessionUpdate {
 expected_revision:string;name:string;state:ShootSession['state'];
 policy:ShootSessionPolicy;capture_policy:ShootCapturePolicy|null;
}
