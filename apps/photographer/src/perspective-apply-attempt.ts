import {preparePerspectiveApply,readPerspectiveApplied,type PerspectiveApplied} from './perspective-apply';
import {readPerspectiveQuality,type PerspectiveQuality} from './perspective-quality';
import {freezeCollectionVersion,type CollectionVersionSelection} from './collection-version';
export type PerspectiveApplyRecord={schema_version:1;review:PerspectiveQuality;warnings:boolean;selection:Omit<CollectionVersionSelection,'version'>|null;applied:PerspectiveApplied|null;selected:boolean};
export interface PerspectiveApplyJournal{write(record:PerspectiveApplyRecord):Promise<void>}
export interface PerspectiveApplyTransport{
 applyPerspective(review:PerspectiveQuality,geometry:boolean,warnings:boolean,signal?:AbortSignal):Promise<PerspectiveApplied>;
 replaceCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<unknown>;
 reconcileCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<'applied'|'unchanged'|'conflict'>;
}
function validate(value:PerspectiveApplyRecord):PerspectiveApplyRecord{
 if(!value||value.schema_version!==1||typeof value.warnings!=='boolean'||typeof value.selected!=='boolean')throw new Error('PERSPECTIVE_APPLY_JOURNAL_INVALID');
 const review=readPerspectiveQuality(value.review,value.review.proposal);preparePerspectiveApply(review,true,value.warnings);
 if(value.selection!==null){const s=value.selection;freezeCollectionVersion({...s,version:review.quality_report.artifact_id});if(s.expectedVersion!==review.proposal.base.asset_version_id)throw new Error('PERSPECTIVE_APPLY_JOURNAL_INVALID');}
 if(value.applied!==null)readPerspectiveApplied(value.applied,review);
 if(value.selected&&(value.applied===null||value.selection===null))throw new Error('PERSPECTIVE_APPLY_JOURNAL_INVALID');
 return structuredClone(value);
}
export class PerspectiveApplyAttempt{
 private busy=false;
 private constructor(private record:PerspectiveApplyRecord,private journal:PerspectiveApplyJournal){}
 static async create(review:PerspectiveQuality,warnings:boolean,selection:PerspectiveApplyRecord['selection'],journal:PerspectiveApplyJournal){
  const record=validate({schema_version:1,review,warnings,selection,applied:null,selected:false});await journal.write(record);return new PerspectiveApplyAttempt(record,journal);
 }
 static restore(record:PerspectiveApplyRecord,journal:PerspectiveApplyJournal){return new PerspectiveApplyAttempt(validate(record),journal);}
 get snapshot(){return structuredClone(this.record);}
 async resume(api:PerspectiveApplyTransport,signal?:AbortSignal){
  if(this.busy)throw new Error('PERSPECTIVE_APPLY_BUSY');this.busy=true;
  const active=()=>{if(signal?.aborted)throw new Error('PERSPECTIVE_APPLY_ABORTED');};
  const persist=async(next:PerspectiveApplyRecord)=>{active();const owned=validate(next);await this.journal.write(owned);active();this.record=owned;};
  try{
   active();
   if(this.record.applied===null){
    const result=readPerspectiveApplied(await api.applyPerspective(structuredClone(this.record.review),true,this.record.warnings,signal),this.record.review);
    await persist({...this.record,applied:result});
   }
   if(this.record.selection!==null&&!this.record.selected){
    const selection=freezeCollectionVersion({...this.record.selection,version:this.record.applied!.asset_version_id});
    let state=await api.reconcileCollectionVersion(selection,signal);active();
    if(state==='conflict')throw new Error('PERSPECTIVE_APPLY_COLLECTION_CONFLICT');
    if(state==='unchanged'){
     try{await api.replaceCollectionVersion(selection,signal);}catch(error){active();state=await api.reconcileCollectionVersion(selection,signal);active();if(state!=='applied')throw error;}
    }
    await persist({...this.record,selected:true});
   }
   active();return structuredClone(this.record.applied!);
  }finally{this.busy=false;}
 }
}
