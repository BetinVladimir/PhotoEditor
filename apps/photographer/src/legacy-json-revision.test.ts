import {describe,it,expect,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {EditorDraft,Collection} from './catalog-types';
import {legacyJSONRevision} from './legacy-json-revision';
const id='11111111-1111-4111-8111-111111111111';
describe('legacy integer CAS contracts',()=>{
 it('accepts zero and safe boundary exactly, refuses rounded/malformed tokens',()=>{
  expect(legacyJSONRevision('0')).toBe(0);expect(legacyJSONRevision('9007199254740991')).toBe(Number.MAX_SAFE_INTEGER);
  for(const value of ['9007199254740992','9007199254740993','9223372036854775807','01','1.5','-1','1e2',1,undefined])expect(()=>legacyJSONRevision(value)).toThrow();
 });
 it('never emits silently rounded editor or collection mutations',async()=>{
  const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id);
  const draft={asset_version_id:id,revision:'9007199254740993',params:{},mask_refs:[],source_sha256:'a'.repeat(64)} as EditorDraft;
  await expect(api.saveDraft(id,draft)).rejects.toThrow();await expect(api.preview(draft)).rejects.toThrow();await expect(api.generate(draft,{} as never,'prompt',1)).rejects.toThrow();
  await expect(api.saveEdits({id,name:'A',revision:draft.revision} as Collection,draft,id,id)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 });
});
