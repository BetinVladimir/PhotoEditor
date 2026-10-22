import {APIError} from '@photoeditor/api-client';
// These codes are emitted before acceptance or by a rolled-back admission.
// Key conflicts, lost replies, dependency and unrecognized errors keep identity.
export function collectionApplyDefinitelyRejected(error:unknown):boolean{
 return error instanceof APIError&&(error.status===422&&['VALIDATION_FAILED','REGION_MASK_LIMIT_EXCEEDED'].includes(error.code)||error.status===409&&['REVISION_CONFLICT','STYLE_NOT_ACCEPTED','SNAPSHOT_STALE','STYLE_MODEL_BINDING_MISSING','PERSON_MASK_CAPABILITY_UNAVAILABLE','RESTORATION_CAPABILITY_UNAVAILABLE','CREATIVE_CAPABILITY_UNAVAILABLE','REGION_MASK_REQUIRED','REGION_MASK_GEOMETRY_UNSUPPORTED','OVERRIDE_MASK_INVALID'].includes(error.code));
}
