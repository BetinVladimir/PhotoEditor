export type ImportPersonMaskDecision = {
  action:'grant'|'revoke';
  entry_id:string;
  target_version_id:string;
  override_artifact_id:string;
  mask_artifact_id:string;
  parsed_jcs?:string;
  recipe_artifact_id?:string;
  recipe_jcs?:string;
  override_jcs?:string;
  mask_jcs?:string;
};

const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';

export function freezeImportPersonMaskDecision(input:ImportPersonMaskDecision):ImportPersonMaskDecision{
  if(!uuid(input.entry_id)||!uuid(input.target_version_id)||!uuid(input.override_artifact_id)||!uuid(input.mask_artifact_id))throw new Error('INTERCHANGE_MASK_DECISION_INVALID');
  if(input.action==='revoke'){
    if(input.parsed_jcs!==undefined||input.recipe_artifact_id!==undefined||input.recipe_jcs!==undefined||input.override_jcs!==undefined||input.mask_jcs!==undefined)throw new Error('INTERCHANGE_MASK_DECISION_INVALID');
    return {action:'revoke',entry_id:input.entry_id,target_version_id:input.target_version_id,override_artifact_id:input.override_artifact_id,mask_artifact_id:input.mask_artifact_id};
  }
  if(input.action!=='grant'||!uuid(input.recipe_artifact_id)||typeof input.parsed_jcs!=='string'||typeof input.recipe_jcs!=='string'||typeof input.override_jcs!=='string'||typeof input.mask_jcs!=='string'||
    input.parsed_jcs.length<2||input.parsed_jcs.length>(8<<20)||input.recipe_jcs.length<2||input.recipe_jcs.length>(1<<20)||input.override_jcs.length<2||input.override_jcs.length>(8<<20)||input.mask_jcs.length<2||input.mask_jcs.length>(1<<20))throw new Error('INTERCHANGE_MASK_DECISION_INVALID');
  return {action:'grant',entry_id:input.entry_id,target_version_id:input.target_version_id,override_artifact_id:input.override_artifact_id,mask_artifact_id:input.mask_artifact_id,
    parsed_jcs:input.parsed_jcs,recipe_artifact_id:input.recipe_artifact_id,recipe_jcs:input.recipe_jcs,override_jcs:input.override_jcs,mask_jcs:input.mask_jcs};
}

export function readImportPersonMaskDecision(value:unknown,workflow:string,request:ImportPersonMaskDecision){
  if(!uuid(workflow)||!value||typeof value!=='object'||Array.isArray(value))throw new Error('INTERCHANGE_MASK_DECISION_RECEIPT_INVALID');
  const body=value as Record<string,unknown>;
  if(Object.keys(body).length!==4||body.workflow_id!==workflow||body.entry_id!==request.entry_id||body.status!==(request.action==='grant'?'granted':'revoked')||
    typeof body.epoch!=='number'||!Number.isSafeInteger(body.epoch)||body.epoch<1)throw new Error('INTERCHANGE_MASK_DECISION_RECEIPT_INVALID');
  return {workflow_id:workflow,entry_id:request.entry_id,status:body.status as 'granted'|'revoked',epoch:body.epoch as number};
}
