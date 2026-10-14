import {expect,it,vi} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {freezeInterchangeSnapshot,readInterchangeSnapshotReceipt} from './interchange-export-snapshot';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const hash=(value:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(value))));
const request=()=>({items:[{ordinal:0,binding:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},working_image:{artifact_id:id(4),sha256:'a'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)},captured_at:null,camera_hash:null}]});
function receipt(){const unsigned={schema_version:1,snapshot_id:id(6),items:request().items},digest=hash(unsigned);return {id:id(6),digest,item_count:1,artifact:{artifact_id:id(7),sha256:hash({...unsigned,snapshot_digest:digest})}};}
it('owns nested source, recipe and returned receipt',()=>{
 const input=request(),owned=freezeInterchangeSnapshot(input,'key');input.items[0].recipe.sha256='d'.repeat(64);input.items[0].binding.source.sha256='e'.repeat(64);expect(owned).toEqual(request());
 const value=receipt(),result=readInterchangeSnapshotReceipt(value,owned);value.artifact.sha256='f'.repeat(64);expect(result).toEqual(receipt());
});
it.each(['ordinal','duplicate','source','recipe','unknown','key'])('rejects malformed snapshot before transport: %s',async mode=>{
 const input:any=request();let key='key';
 if(mode==='ordinal')input.items[0].ordinal=1;if(mode==='duplicate')input.items.push({...input.items[0],ordinal:1});
 if(mode==='source')input.items[0].working_image.sha256='d'.repeat(64);if(mode==='recipe')input.items[0].recipe=null;
 if(mode==='unknown')input.items[0].extra=true;if(mode==='key')key='bad key';
 const transport=vi.fn();await expect(new OwnerAPI({request:transport} as never,id(1)).prepareInterchangeSnapshot(id(2),input,key)).rejects.toThrow();expect(transport).not.toHaveBeenCalled();
});
it.each(['digest','artifact','count','id'])('rejects a substituted receipt: %s',mode=>{
 const value=receipt();if(mode==='digest')value.digest='d'.repeat(64);if(mode==='artifact')value.artifact.sha256='d'.repeat(64);if(mode==='count')value.item_count=2;if(mode==='id')value.id=id(8);
 expect(()=>readInterchangeSnapshotReceipt(value,request())).toThrow();
});
it('posts exact owned snapshot with original key',async()=>{
 const transport=vi.fn().mockResolvedValue(receipt()),api=new OwnerAPI({request:transport} as never,id(1));
 expect(await api.prepareInterchangeSnapshot(id(2),request(),'original')).toEqual(receipt());
 expect(transport).toHaveBeenCalledWith(`/api/v1/tenants/${id(1)}/projects/${id(2)}/interchange-export-snapshots`,{method:'POST',body:request(),idempotencyKey:'original',signal:undefined});
});
it('ignores a late receipt after cancellation',async()=>{
 const controller=new AbortController(),transport=vi.fn(async()=>{controller.abort();return receipt();});
 await expect(new OwnerAPI({request:transport} as never,id(1)).prepareInterchangeSnapshot(id(2),request(),'key',controller.signal)).rejects.toThrow();
});
