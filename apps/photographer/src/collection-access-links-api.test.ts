import {expect,it,vi} from 'vitest';
import {CollectionAccessLinksAPI,freezeLinkPolicy,readAccessLink,readCreatedAccessLink,type LinkPolicy} from './collection-access-links-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const policy:LinkPolicy={label:'Client',permissions:{view:true,download:false,upload:true},limits:{max_upload_files:10,max_upload_bytes_total:10000,max_file_bytes:1000,allowed_formats:['jpg']},moderation:'owner_review',allowed_presets:[]};
const row=()=>({id:id(3),collection_id:id(2),policy_version:'9223372036854775807',created_at:'2026-10-17T10:00:00Z',...policy,expires_at:'2026-11-17T10:00:00Z'});
it('decodes actual canonical Go object order and nullable empty preset arrays without leaking secret into rows',()=>{
 const raw=JSON.parse(JSON.stringify({...row(),allowed_presets:null,permissions:{download:false,upload:true,view:true},limits:{allowed_formats:['jpg'],max_file_bytes:1000,max_upload_bytes_total:10000,max_upload_files:10},secret:'A'.repeat(43)}));
 const result=readCreatedAccessLink(raw,id(2),policy);expect(result.row.allowed_presets).toEqual([]);expect(result.secret).toBe('A'.repeat(43));expect(result.row).not.toHaveProperty('secret');expect(()=>readAccessLink(raw,id(2))).toThrow();
});
it('refuses forged parent, mismatched accepted policy, malformed or extra secrets',()=>{
 expect(()=>readCreatedAccessLink({...row(),collection_id:id(8)},id(2),policy)).toThrow();expect(()=>readCreatedAccessLink({...row(),label:'Other'},id(2),policy)).toThrow();expect(()=>readCreatedAccessLink({...row(),secret:'invalid'},id(2),policy)).toThrow();expect(()=>readCreatedAccessLink({...row(),secret_hash:'private'},id(2),policy)).toThrow();
});
it('keeps omitted, null and explicit expiry distinct and copies caller slices',()=>{
 const input=structuredClone(policy);const omitted=freezeLinkPolicy(input);input.limits.allowed_formats.push('png');expect(omitted.limits.allowed_formats).toEqual(['jpg']);expect(omitted).not.toHaveProperty('expires_at');expect(freezeLinkPolicy({...policy,expires_at:null})).toHaveProperty('expires_at',null);expect(freezeLinkPolicy({...policy,expires_at:'2026-11-17T10:00:00Z'}).expires_at).toBe('2026-11-17T10:00:00Z');
});
it('sends exact int64 If-Match and refuses preaborted IO for every delegate',async()=>{
 const request=vi.fn().mockResolvedValue(row()),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2));await api.update({...row(),policy_version:'9223372036854775806'},policy,new AbortController().signal);expect(request.mock.calls[0][1].headers).toEqual({'If-Match':'"9223372036854775806"'});request.mockClear();const c=new AbortController();c.abort();for(const call of [()=>api.page(c.signal),()=>api.create(policy,id(4),c.signal),()=>api.update(row(),policy,c.signal),()=>api.revoke(row(),c.signal),()=>api.usage(id(3),c.signal)])await expect(call()).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
it('refuses late results after cancellation and freezes the POST request before transport',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise(done=>resolve=done)),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2)),c=new AbortController(),input=structuredClone(policy);const call=api.create(input,id(4),c.signal);input.label='Changed';input.limits.allowed_formats.push('png');expect(request.mock.calls[0][1].body.label).toBe('Client');expect(request.mock.calls[0][1].body.limits.allowed_formats).toEqual(['jpg']);c.abort();resolve(row());await expect(call).rejects.toThrow();
});
it('validates real usage strings and does not imply completed network transfers',async()=>{
 const usage={share_id:id(3),uploaded_files:'2',uploaded_bytes:'1048576',quota_used_files:'2',quota_used_bytes:'1048576',reserved_files:'1',reserved_bytes:'500',download_authorizations:'3',download_jobs:'1'},request=vi.fn().mockResolvedValue(usage),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2));expect(await api.usage(id(3),new AbortController().signal)).toEqual(usage);request.mockResolvedValue({...usage,guest_session_id:id(9)});await expect(api.usage(id(3),new AbortController().signal)).rejects.toThrow();
});
it('allows independent download permission without silently granting view',()=>{
 const downloadOnly=freezeLinkPolicy({...policy,permissions:{view:false,download:true,upload:false}});expect(downloadOnly.permissions).toEqual({view:false,download:true,upload:false});
});
it('abandons using the exact original body/key and strictly distinguishes the accepted winner',async()=>{
 const request=vi.fn().mockResolvedValue({status:'abandoned',key:id(4),collection_id:id(2)}),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2)),signal=new AbortController().signal;
 expect(await api.abandon(policy,id(4),signal)).toEqual({abandoned:true});expect(request.mock.calls[0][0]).toMatch(/shares\/abandon$/);expect(request.mock.calls[0][1]).toMatchObject({body:policy,idempotencyKey:id(4)});
 request.mockResolvedValue({...row(),policy_version:'1'});expect((await api.abandon(policy,id(4),signal)).abandoned).toBe(false);
 request.mockResolvedValue({...row(),secret:'A'.repeat(43)});await expect(api.abandon(policy,id(4),signal)).rejects.toThrow();
 request.mockResolvedValue({status:'abandoned',key:id(4),collection_id:id(2),project_id:id(8)});await expect(api.abandon(policy,id(4),signal)).rejects.toThrow();
 request.mockResolvedValue({status:'abandoned',key:id(9),collection_id:id(2)});await expect(api.abandon(policy,id(4),signal)).rejects.toThrow();
});

it('requires actual nullable fields instead of inventing missing empty policy arrays',()=>{
 const missingPresets={...row()} as Record<string,unknown>;delete missingPresets.allowed_presets;expect(()=>readAccessLink(missingPresets,id(2))).toThrow();
 const missingFormats={...row(),limits:{...policy.limits}} as any;delete missingFormats.limits.allowed_formats;expect(()=>readAccessLink(missingFormats,id(2))).toThrow();
 expect(readAccessLink({...row(),allowed_presets:null,limits:{...policy.limits,allowed_formats:null}},id(2)).limits.allowed_formats).toEqual([]);
 for(const created_at of ['1','invalid',null])expect(()=>readAccessLink({...row(),created_at},id(2))).toThrow();
});
it('validates closed list pages and bounded cursor before transport',async()=>{
 const request=vi.fn(),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2)),signal=new AbortController().signal;
 for(const raw of [{items:[]},{items:[],next_cursor:null,revision:'1'},{items:[],next_cursor:'cursor'},{items:[row()],next_cursor:'x'.repeat(4097)}]){request.mockResolvedValue(raw);await expect(api.page(signal)).rejects.toThrow('SHARE_PAGE_INVALID')}
 request.mockClear();await expect(api.page(signal,'')).rejects.toThrow();await expect(api.page(signal,'x'.repeat(4097))).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 request.mockResolvedValue({items:[row()],next_cursor:null});expect((await api.page(signal)).items).toHaveLength(1);
});
it('freezes PATCH row identity and binds the exact revision increment',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise(done=>resolve=done)),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2)),source={...row(),policy_version:'7'};
 const pending=api.update(source,policy,new AbortController().signal);source.id=id(9);source.policy_version='99';resolve({...row(),policy_version:'8'});expect((await pending).id).toBe(id(3));expect(request.mock.calls[0][0]).toMatch(new RegExp(id(3)+'$'));expect(request.mock.calls[0][1].headers['If-Match']).toBe('"7"');
 for(const response of [{...row(),policy_version:'7'},{...row(),policy_version:'9'},{...row(),policy_version:'8',created_at:'2026-10-18T10:00:00Z'},{...row(),policy_version:'8',revoked_at:'2026-10-18T10:00:00Z'}]){request.mockResolvedValue(response);await expect(api.update({...row(),policy_version:'7'},policy,new AbortController().signal)).rejects.toThrow('SHARE_INVALID')}
});
it('fresh create and abandon accepted winner require the initial unrevoked revision',async()=>{
 const request=vi.fn(),api=new CollectionAccessLinksAPI({request} as any,id(1),id(2)),signal=new AbortController().signal;
 for(const response of [{...row(),policy_version:'2'},{...row(),policy_version:'1',revoked_at:'2026-10-18T10:00:00Z'}]){request.mockResolvedValue(response);await expect(api.create(policy,id(4),signal)).rejects.toThrow('SHARE_RECEIPT_INVALID');await expect(api.abandon(policy,id(4),signal)).rejects.toThrow('SHARE_RECEIPT_INVALID')}
});


it('explicit expiry binds exact persisted microseconds rather than milliseconds',()=>{
 const request={...policy,expires_at:'2026-11-17T10:00:00.123456789Z'};
 const accepted={...row(),expires_at:'2026-11-17T10:00:00.123456Z'};
 expect(readCreatedAccessLink(accepted,id(2),request).row.expires_at).toBe(accepted.expires_at);
 expect(()=>readCreatedAccessLink({...accepted,expires_at:'2026-11-17T10:00:00.123457Z'},id(2),request)).toThrow('SHARE_RECEIPT_INVALID');
 expect(readCreatedAccessLink({...accepted,expires_at:'2026-11-17T12:00:00.123456+02:00'},id(2),request).row.id).toBe(accepted.id);
});
