import type {ImportPersonMaskStatus} from './interchange-person-mask-status';
import type {ImportPersonMaskDecision} from './interchange-person-mask-decision';
/** null means no transition exists; never journal an already satisfied action. */
export function importMaskDecisionEpoch(statuses:ImportPersonMaskStatus[]|null,source:ImportPersonMaskDecision,action:'grant'|'revoke'):number|null{
 if(statuses===null)throw new Error('INTERCHANGE_MASK_STATUS_REQUIRED');
 const status=statuses.find(item=>item.entry_id===source.entry_id&&item.override_artifact_id===source.override_artifact_id&&item.mask_artifact_id===source.mask_artifact_id&&item.target_version_id===source.target_version_id);
 if(!status)return action==='grant'?0:null;
 if(status.status===(action==='grant'?'granted':'revoked'))return null;
 if(!Number.isSafeInteger(status.epoch)||status.epoch<1||status.epoch>=Number.MAX_SAFE_INTEGER)throw new Error('INTERCHANGE_MASK_EPOCH_EXHAUSTED');
 return status.epoch;
}
