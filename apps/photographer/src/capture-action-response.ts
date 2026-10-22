import type {CaptureAlert,CaptureFrameConfirmation} from './capture-types';
import {readCaptureAlertPage} from './capture-alert-response';

const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const stamp=(v:unknown)=>typeof v==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v)&&Number.isFinite(Date.parse(v));
export function captureActionKey(key:string){if(typeof key!=='string'||! /^[!-~]{1,128}$/.test(key))throw Error('CAPTURE_ACTION_KEY_INVALID');}
export function captureConfirmationScope(receipt:string,alert:string){if(!uuid(receipt)||!uuid(alert))throw Error('CAPTURE_CONFIRMATION_SCOPE_INVALID');}
export function freezeCaptureAcknowledgement(alert:CaptureAlert):CaptureAlert{
 if(!uuid(alert?.session_id))throw Error('CAPTURE_ACK_SCOPE_INVALID');
 const frozen=readCaptureAlertPage({items:[alert],next_cursor:null},alert.session_id).items[0];
 if(frozen.acknowledged_at!==null||frozen.acknowledged_by!==null)throw Error('CAPTURE_ACK_ALREADY_ACKNOWLEDGED');
 return frozen;
}
export function readCaptureAcknowledgement(value:unknown,tenant:string,expected:CaptureAlert):CaptureAlert{
 const fields=['resource_type','id','tenant_id','project_id','created_at','updated_at','revision','session_id','asset_version_id','report_artifact_id','severity','receipt_id','qa_stale','acknowledged_by','acknowledged_at'];
 if(!object(value)||Object.keys(value).sort().join(',')!==fields.sort().join(',')||value.resource_type!=='capture_alerts'||value.tenant_id!==tenant||!uuid(value.project_id)||!stamp(value.updated_at))throw Error('CAPTURE_ACK_RECEIPT_INVALID');
 const row=readCaptureAlertPage({items:[value],next_cursor:null},expected.session_id).items[0];
 if(row.id!==expected.id||row.asset_version_id!==expected.asset_version_id||row.report_artifact_id!==expected.report_artifact_id||row.receipt_id!==expected.receipt_id||row.severity!==expected.severity||row.qa_stale!==expected.qa_stale||row.created_at!==expected.created_at||row.acknowledged_by===null||row.acknowledged_at===null||BigInt(row.revision)!==BigInt(expected.revision)+1n)throw Error('CAPTURE_ACK_RECEIPT_INVALID');
 return row;
}
export function readCaptureConfirmation(value:unknown,receipt:string,alert:string):CaptureFrameConfirmation{
 captureConfirmationScope(receipt,alert);
 if(!object(value)||Object.keys(value).sort().join(',')!=='alert_id,confirmation_id,receipt_id'||!uuid(value.confirmation_id)||value.receipt_id!==receipt||value.alert_id!==alert)throw Error('CAPTURE_CONFIRMATION_RECEIPT_INVALID');
 return {confirmation_id:value.confirmation_id,receipt_id:receipt,alert_id:alert};
}
