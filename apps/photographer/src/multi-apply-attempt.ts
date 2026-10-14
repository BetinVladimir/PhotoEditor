import {canonicalJSON} from '@photoeditor/api-client';
import type {MultiIntent} from './multi-intent';
import {prepareMultiApply,readMultiApplied,type MultiApplied} from './multi-apply';
import {readMultiQuality,type MultiQuality} from './multi-quality';
import {freezeCollectionVersionIntent,freezeCollectionVersion,type CollectionVersionSelection} from './collection-version';
export type MultiApplyRecord={schema_version:1;intent:MultiIntent;review:MultiQuality;warnings:boolean;selection:Omit<CollectionVersionSelection,'version'>|null;applied:MultiApplied|null;selected:boolean};
export interface MultiApplyJournal{
 write(record:MultiApplyRecord):Promise<void>;
 /** Serializes a full recovery across views/tabs; supplies the freshest durable state. */
 execute?<T>(action:(current:MultiApplyRecord|null)=>Promise<T>):Promise<T>;
 assertCurrent?():Promise<void>;
}
export interface MultiApplyTransport{
 applyMulti(review:MultiQuality,intent:MultiIntent,geometry:boolean,warnings:boolean,signal?:AbortSignal):Promise<MultiApplied>;
 replaceCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<unknown>;
 reconcileCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<'applied'|'unchanged'|'conflict'>;
}
function validate(value:MultiApplyRecord):MultiApplyRecord{
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join(',')!=='applied,intent,review,schema_version,selected,selection,warnings'||value.schema_version!==1||typeof value.warnings!=='boolean'||typeof value.selected!=='boolean')throw new Error('MULTI_APPLY_JOURNAL_INVALID');
 const review=readMultiQuality(value.review,value.review.proposal,value.intent);prepareMultiApply(review,true,value.warnings,value.intent);
 if(value.selection!==null){const s=value.selection;freezeCollectionVersionIntent(s);if(s.expectedVersion!==review.proposal.sources[review.proposal.reference_ordinal].asset_version_id)throw new Error('MULTI_APPLY_JOURNAL_INVALID');}
 if(value.applied!==null)readMultiApplied(value.applied,review);
 if(value.selected&&(value.applied===null||value.selection===null))throw new Error('MULTI_APPLY_JOURNAL_INVALID');
 return structuredClone(value);
}
export class MultiApplyAttempt{
 private busy=false;
 private constructor(private record:MultiApplyRecord,private journal:MultiApplyJournal){}
 static async create(review:MultiQuality,intent:MultiIntent,warnings:boolean,selection:MultiApplyRecord['selection'],journal:MultiApplyJournal){
  const record=validate({schema_version:1,intent,review,warnings,selection,applied:null,selected:false});await journal.write(structuredClone(record));return new MultiApplyAttempt(record,journal);
 }
 static restore(record:MultiApplyRecord,journal:MultiApplyJournal){return new MultiApplyAttempt(validate(record),journal);}
 get snapshot(){return structuredClone(this.record);}
 async resume(api:MultiApplyTransport,signal?:AbortSignal){
  if(this.busy)throw new Error('MULTI_APPLY_BUSY');this.busy=true;
  const run=async(current?:MultiApplyRecord|null)=>{
   if(current!==undefined){
    if(current===null)throw new Error('MULTI_APPLY_JOURNAL_MISSING');
    const next=validate(current);
    for(const key of ['intent','review','warnings','selection'] as const)if(canonicalJSON(next[key])!==canonicalJSON(this.record[key]))throw new Error('MULTI_APPLY_JOURNAL_CONFLICT');
    if(this.record.applied!==null&&canonicalJSON(this.record.applied)!==canonicalJSON(next.applied)||this.record.selected&&!next.selected)throw new Error('MULTI_APPLY_JOURNAL_CONFLICT');
    this.record=next;
   }
   return this.resumeLocked(api,signal);
  };
  try{return await (this.journal.execute?this.journal.execute(run):run());}finally{this.busy=false;}
 }
 private async resumeLocked(api:MultiApplyTransport,signal?:AbortSignal){
  const active=()=>{if(signal?.aborted)throw new Error('MULTI_APPLY_ABORTED');};
  const authority=async()=>{active();await this.journal.assertCurrent?.();active();};
  const persist=async(next:MultiApplyRecord)=>{active();const owned=validate(next);await this.journal.write(structuredClone(owned));active();this.record=owned;};
  await authority();
  if(this.record.applied===null){
   const result=readMultiApplied(await api.applyMulti(structuredClone(this.record.review),structuredClone(this.record.intent),true,this.record.warnings,signal),this.record.review);
   await persist({...this.record,applied:result});
  }
  if(this.record.selection!==null&&!this.record.selected){
   const selection=freezeCollectionVersion({...this.record.selection,version:this.record.applied!.asset_version_id});
   await authority();let state=await api.reconcileCollectionVersion({...selection},signal);active();
   if(!['applied','unchanged','conflict'].includes(state))throw new Error('MULTI_APPLY_RECONCILIATION_INVALID');
   if(state==='conflict')throw new Error('MULTI_APPLY_COLLECTION_CONFLICT');
   if(state==='unchanged'){
    await authority();
    try{await api.replaceCollectionVersion({...selection},signal);}catch(error){await authority();state=await api.reconcileCollectionVersion({...selection},signal);active();if(state!=='applied')throw error;}
   }
   await persist({...this.record,selected:true});
  }
  await authority();return structuredClone(this.record.applied!);
 }
}
