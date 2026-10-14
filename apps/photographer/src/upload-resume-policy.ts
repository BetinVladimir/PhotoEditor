import {readRAWDevelopment, type RAWDevelopment, type UploadRecord, type UploadSource, uploadRequiresRestart} from '@photoeditor/api-client';

/** A picked file may resume only the immutable development choice admitted at Begin. */
export function findResumableUpload(records:readonly UploadRecord[], source:UploadSource, selected?:RAWDevelopment):UploadRecord|undefined {
 const sameFile=records.filter(x=>(x.state==='paused'||x.state==='error')&&!uploadRequiresRestart(x)&&x.name===source.name&&x.size===source.size&&x.lastModified===source.lastModified&&x.sha256===source.sha256);
 const policy=/\.(dng|cr2|nef|arw|raf|orf|rw2)$/i.test(source.name)?selected:undefined;
 const frozen=policy===undefined?undefined:readRAWDevelopment(policy);
 const matches=sameFile.filter(x=>{
  const prior=x.rawDevelopment===undefined?undefined:readRAWDevelopment(x.rawDevelopment);
  return prior===undefined?frozen===undefined:frozen!==undefined&&prior.schema_version===frozen.schema_version&&prior.demosaic===frozen.demosaic&&prior.wb_source===frozen.wb_source&&prior.exposure_ev===frozen.exposure_ev&&prior.highlight_policy===frozen.highlight_policy&&prior.decoder_version===frozen.decoder_version;
 });
 if(matches.length>1)throw new Error('UPLOAD_RESUME_AMBIGUOUS');
 if(matches.length===1)return matches[0];
 if(sameFile.length)throw new Error('UPLOAD_RAW_POLICY_CHANGED');
 return undefined;
}

export function uploadPickerErrorKey(error:unknown):'import_raw_policy_changed'|'import_resume_ambiguous'|'import_add_failed' {
 if(error instanceof Error&&error.message==='UPLOAD_RAW_POLICY_CHANGED')return 'import_raw_policy_changed';
 if(error instanceof Error&&error.message==='UPLOAD_RESUME_AMBIGUOUS')return 'import_resume_ambiguous';
 return 'import_add_failed';
}
