import {archiveUUID} from './interchange-archive-upload';

export type PortraitDetailProducerSelection={
  run_id:string;task_id:string;result_id:string;attempt_id:string
};
export type LearningCorrectionCreate={
  client_event_id:string;session_id:string;asset_version_id:string;
  base_document_id:string;approved_document_id:string;
  reason:'exposure'|'color'|'crop'|'retouch'|'selection'|'other';
  learn:boolean;person_id:string|null;scene_group_id:string|null;
  consent_id:string|null;consent_epoch:string|null;
  portrait_detail_producer?:PortraitDetailProducerSelection;
};
export type LearningCorrectionReceipt={
  id:string;project_id:string;client_event_id:string;asset_version_id:string
};

const keys=['client_event_id','session_id','asset_version_id','base_document_id',
  'approved_document_id','reason','learn','person_id','scene_group_id',
  'consent_id','consent_epoch'] as const;
const producerKeys=['run_id','task_id','result_id','attempt_id'] as const;
const validUUID=(value:unknown):value is string=>archiveUUID(value);

export function freezeLearningCorrection(value:LearningCorrectionCreate):LearningCorrectionCreate{
  if(value===null||typeof value!=='object'||Array.isArray(value)||
      Object.keys(value).some(key=>!([...keys,'portrait_detail_producer'] as string[]).includes(key))||
      keys.some(key=>!Object.hasOwn(value,key)))throw new Error('INVALID_CORRECTION');
  for(const key of ['client_event_id','session_id','asset_version_id',
    'base_document_id','approved_document_id'] as const){
    if(!validUUID(value[key]))throw new Error('INVALID_CORRECTION');
  }
  if(!['exposure','color','crop','retouch','selection','other'].includes(value.reason)||
      typeof value.learn!=='boolean'||(value.person_id!==null&&!validUUID(value.person_id))||
      (value.scene_group_id!==null&&!validUUID(value.scene_group_id))||
      (value.consent_id!==null&&!validUUID(value.consent_id))||
      (value.consent_epoch!==null&&
        (typeof value.consent_epoch!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.consent_epoch)))||
      value.learn!==(value.consent_id!==null&&value.consent_epoch!==null))
    throw new Error('INVALID_CORRECTION');
  const selected=value.portrait_detail_producer;
  if(selected!==undefined){
    if(!value.learn||value.reason!=='retouch'||selected===null||
        typeof selected!=='object'||Array.isArray(selected)||
        Object.keys(selected).length!==4||producerKeys.some(key=>!validUUID(selected[key])))
      throw new Error('INVALID_DETAIL_PRODUCER');
  }
  return {
    client_event_id:value.client_event_id,session_id:value.session_id,
    asset_version_id:value.asset_version_id,base_document_id:value.base_document_id,
    approved_document_id:value.approved_document_id,reason:value.reason,
    learn:value.learn,person_id:value.person_id,scene_group_id:value.scene_group_id,
    consent_id:value.consent_id,consent_epoch:value.consent_epoch,
    ...(selected===undefined?{}:{portrait_detail_producer:{
      run_id:selected.run_id,task_id:selected.task_id,
      result_id:selected.result_id,attempt_id:selected.attempt_id}})
  };
}

export function readLearningCorrectionReceipt(value:unknown,
    request:LearningCorrectionCreate):LearningCorrectionReceipt{
  if(value===null||typeof value!=='object'||Array.isArray(value))
    throw new Error('INVALID_CORRECTION_RECEIPT');
  const row=value as Record<string,unknown>;
  if(!validUUID(row.id)||!validUUID(row.project_id)||
      row.client_event_id!==request.client_event_id||
      row.asset_version_id!==request.asset_version_id)
    throw new Error('INVALID_CORRECTION_RECEIPT');
  return {id:row.id,project_id:row.project_id,
    client_event_id:request.client_event_id,asset_version_id:request.asset_version_id};
}
