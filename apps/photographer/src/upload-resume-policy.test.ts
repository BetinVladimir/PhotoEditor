import {describe,expect,it} from 'vitest';
import type {UploadRecord,UploadSource} from '@photoeditor/api-client';
import {findResumableUpload,uploadPickerErrorKey} from './upload-resume-policy';

const policy={schema_version:1 as const,demosaic:'ahd' as const,wb_source:'camera' as const,exposure_ev:1,highlight_policy:'clip' as const,decoder_version:'rawpy-0.27.1/libraw-0.21.4'};
const source:UploadSource={name:'frame.nef',size:10,lastModified:5,sha256:'a'.repeat(64),read:async()=>new Uint8Array()};
const record=(changes:Partial<UploadRecord>={}):UploadRecord=>({localId:'one',tenant:'t',projectId:'p',name:source.name,size:source.size,lastModified:source.lastModified,sha256:source.sha256,parts:[],state:'paused',rawDevelopment:policy,...changes});

describe('resume immutable RAW policy',()=>{
 it('resumes only an exact previous choice',()=>expect(findResumableUpload([record()],source,policy)?.localId).toBe('one'));
 it('refuses a changed exposure or an implicit legacy choice',()=>{
  expect(()=>findResumableUpload([record()],source,{...policy,exposure_ev:2})).toThrow('UPLOAD_RAW_POLICY_CHANGED');
  expect(()=>findResumableUpload([record()],source)).toThrow('UPLOAD_RAW_POLICY_CHANGED');
  expect(()=>findResumableUpload([record({rawDevelopment:undefined})],source,policy)).toThrow('UPLOAD_RAW_POLICY_CHANGED');
 });
 it('does not compare policy for raster files',()=>{
  const raster={...source,name:'frame.jpg'};
  expect(findResumableUpload([record({name:'frame.jpg',rawDevelopment:undefined})],raster,policy)?.localId).toBe('one');
 });
 it('refuses duplicate resumable records',()=>expect(()=>findResumableUpload([record(),record({localId:'two'})],source,policy)).toThrow('UPLOAD_RESUME_AMBIGUOUS'));
 it('provides a specific recovery message only for known conflicts',()=>{
  expect(uploadPickerErrorKey(new Error('UPLOAD_RAW_POLICY_CHANGED'))).toBe('import_raw_policy_changed');
  expect(uploadPickerErrorKey(new Error('UPLOAD_RESUME_AMBIGUOUS'))).toBe('import_resume_ambiguous');
  expect(uploadPickerErrorKey(new Error('UPLOAD_FILE_MISMATCH'))).toBe('import_add_failed');
 });
});

describe('resume immutable missing-profile policy',()=>{
 const raster={...source,name:'frame.jpg'};
 const prior=(assumeProfile?:UploadRecord['assumeProfile'])=>record({name:raster.name,rawDevelopment:undefined,assumeProfile});
 it.each(['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'] as const)('resumes the exact explicit %s choice',profile=>expect(findResumableUpload([prior(profile)],raster,undefined,profile)?.localId).toBe('one'));
 it('refuses profile changes in both directions and between explicit profiles',()=>{
  for(const [before,after] of [[undefined,'srgb_v1'],['srgb_v1',undefined],['srgb_v1','linear_srgb_v1']] as const)expect(()=>findResumableUpload([prior(before)],raster,undefined,after)).toThrow('UPLOAD_PROFILE_CHANGED');
  expect(uploadPickerErrorKey(new Error('UPLOAD_PROFILE_CHANGED'))).toBe('import_profile_changed');
 });
});
