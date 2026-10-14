import {expect,it} from 'vitest';
import {readOwnerUploadBegin,readOwnerUploadParts} from './owner-upload-response';
const id='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222';
it('binds owner Begin response to exact file and project',()=>{
 const record={projectId:project,name:'frame.nef',size:1024};const value={id,project_id:project,filename:record.name,expected_size:1024,part_bytes:16777216,parts_total:1,state:'uploading'};
 expect(readOwnerUploadBegin(value,record).id).toBe(id);
 for(const changed of [{project_id:id},{filename:'foreign.nef'},{collection_id:id},{expected_size:2048},{parts_total:2},{part_bytes:4},{id:'invalid'}])expect(()=>readOwnerUploadBegin({...value,...changed},record)).toThrow();
});
it('binds signed batch identity, membership, URL policy and expiry',()=>{
 const now=Date.now();const item={part_no:1,url:'https://storage.test/part?signature=fixture',expires_at:new Date(now+600000).toISOString()};const value={upload_id:id,items:[item]};
 expect(readOwnerUploadParts(value,id,[1],'https://api.test',now)[0].url).toBe(item.url);
 for(const changed of [{url:'https://user:secret@storage.test/part'},{url:'https://storage.test/part#fragment'},{url:'http://storage.test/part'},{part_no:2},{expires_at:new Date(now-1).toISOString()}])expect(()=>readOwnerUploadParts({...value,items:[{...item,...changed}]},id,[1],'https://api.test',now)).toThrow();
 expect(()=>readOwnerUploadParts({...value,upload_id:project},id,[1],'https://api.test',now)).toThrow();
 expect(()=>readOwnerUploadParts({upload_id:id,items:[item,item]},id,[1,2],'https://api.test',now)).toThrow();
 expect(readOwnerUploadParts({upload_id:id,items:[{...item,url:'http://127.0.0.1:9000/part'}]},id,[1],'http://localhost:8080',now)).toHaveLength(1);
});
