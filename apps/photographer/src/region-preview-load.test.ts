import {it,expect,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {loadRegionPreview} from './region-preview-load';
import type {RunStatus,RunOutput} from './catalog-types';
import type {RegionManifestBinding} from './region-preview-manifest';
it('never requests image bytes after an invalid manifest',async()=>{
 const id='11111111-1111-4111-8111-111111111111',body=new TextEncoder().encode('{}');
 const base={sha256:'a'.repeat(64),bytes:100,url:'https://photo.test/image?versionId=image-v1',expires_at:'2099-01-01'};
 const items:RunOutput[]=[{...base,artifact_id:id,role:'document',media_type:'application/json',bytes:2,sha256:bytesToHex(sha256(body)),url:'https://photo.test/document?versionId=manifest-v1'},{...base,artifact_id:'22222222-2222-4222-8222-222222222222',role:'derivative',media_type:'image/jpeg'}];
 const reader={run:vi.fn().mockResolvedValue({id,status:'completed'} as RunStatus),runOutputs:vi.fn().mockResolvedValue({items})};
 const binding:RegionManifestBinding={request:{version_id:id,recipe:{artifact_id:id,sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:1024},source:{artifact_id:id,sha256:'b'.repeat(64)},canvas:{width:100,height:100},paramsSha256:'c'.repeat(64),iccSha256:'d'.repeat(64),alpha:false};
 const transport=vi.fn().mockResolvedValue(new Response(body));await expect(loadRegionPreview(reader,{run_id:id,workflow_id:id,state:'queued'},binding,new AbortController().signal,transport)).rejects.toThrow('REGION_MANIFEST_INVALID');expect(transport).toHaveBeenCalledTimes(1);expect(transport.mock.calls[0][0]).toBe('https://photo.test/document?versionId=manifest-v1');
});
