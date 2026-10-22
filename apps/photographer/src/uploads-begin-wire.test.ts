import {expect,it,vi} from 'vitest';
import {ApiClient,canonicalJSON,type UploadRecord} from '@photoeditor/api-client';
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('expo-file-system',()=>({File:class {}}));
import {OwnerMultipartAPI} from './uploads';
it.each([undefined,'33333333-3333-4333-8333-333333333333'])('serializes real upload Begin with optional collection %s',async collection=>{
 const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222',upload='44444444-4444-4444-8444-444444444444';
 const receipt={id:upload,project_id:project,...(collection!==undefined?{collection_id:collection}:{}),filename:'frame.jpg',expected_size:3,part_bytes:16777216,parts_total:1,state:'uploading'};
 const transport=vi.fn().mockResolvedValue(new Response(JSON.stringify(receipt),{status:201}));
 const client=new ApiClient('http://localhost',{principal:()=>({kind:'owner',accessToken:'fixture'}),refresh:async()=>'',clear:()=>{},csrfToken:()=> 'csrf',clientHeaders:()=>({'X-Photo-Client':'native'})},transport);
 const record:UploadRecord={localId:'stable-upload-key',tenant,projectId:project,...(collection!==undefined?{collectionId:collection}:{}),name:'frame.jpg',size:3,lastModified:0,sha256:'a'.repeat(64),parts:[],state:'queued'};
 const signal=new AbortController().signal;
 expect(await new OwnerMultipartAPI(client,tenant).begin(record,signal)).toMatchObject({id:upload,state:'uploading'});
 expect(transport).toHaveBeenCalledTimes(1);
 const [url,options]=transport.mock.calls[0];
 expect(url).toBe(`http://localhost/api/v1/tenants/${tenant}/uploads`);
 const expected={project_id:project,...(collection!==undefined?{collection_id:collection}:{}),filename:'frame.jpg',bytes:3,format:'jpg',sha256:'a'.repeat(64)};
 expect(options.body).toBe(canonicalJSON(expected));
 const body=JSON.parse(options.body);expect(body).toEqual(expected);
 expect(Object.hasOwn(body,'collection_id')).toBe(collection!==undefined);
 expect(options.headers.get('Idempotency-Key')).toBe(record.localId);
 expect(options.signal).toBe(signal);
});

it.each(['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'] as const)('sends explicitly selected %s in the production Begin request',async assumeProfile=>{
 const transport=vi.fn().mockResolvedValue(new Response('{}',{status:500}));
 const client=new ApiClient('http://localhost',{principal:()=>({kind:'owner',accessToken:'fixture'}),refresh:async()=>'',clear:()=>{},csrfToken:()=> 'csrf',clientHeaders:()=>({'X-Photo-Client':'native'})},transport);
 const record:UploadRecord={assumeProfile,localId:'key',tenant:'t',projectId:'p',name:'frame.jpg',size:3,lastModified:0,sha256:'a'.repeat(64),parts:[],state:'queued'};
 await expect(new OwnerMultipartAPI(client,'t').begin(record,new AbortController().signal)).rejects.toThrow();
 expect(JSON.parse(transport.mock.calls[0][1].body).assume_profile).toBe(assumeProfile);
});
