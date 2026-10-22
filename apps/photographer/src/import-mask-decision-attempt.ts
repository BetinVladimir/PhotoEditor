import {freezeImportPersonMaskDecision,type ImportPersonMaskDecision} from './interchange-person-mask-decision';
/** Retain an owned CAS body until a validated receipt acknowledges it. */
export class ImportMaskDecisionAttempt {
 private body:ImportPersonMaskDecision|undefined;
 prepare(input:ImportPersonMaskDecision):ImportPersonMaskDecision{
  const next=freezeImportPersonMaskDecision(input);
  if(this.body&&JSON.stringify(this.body)!==JSON.stringify(next))throw new Error('INTERCHANGE_MASK_DECISION_UNRESOLVED');
  this.body=next;return {...next};
 }
 acknowledge(input:ImportPersonMaskDecision){
  const next=freezeImportPersonMaskDecision(input);
  if(!this.body||JSON.stringify(this.body)!==JSON.stringify(next))throw new Error('INTERCHANGE_MASK_DECISION_UNRESOLVED');
  this.body=undefined;
 }
 snapshot():ImportPersonMaskDecision|undefined{return this.body?{...this.body}:undefined;}
}
