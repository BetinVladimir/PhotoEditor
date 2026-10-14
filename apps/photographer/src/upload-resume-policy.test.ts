import {describe,expect,it} from 'vitest';
import type {UploadRecord,UploadSource} from '@photoeditor/api-client';
import {findResumableUpload} from './upload-resume-policy';

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
});
