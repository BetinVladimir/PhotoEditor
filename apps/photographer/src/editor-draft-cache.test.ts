import {expect,it} from 'vitest';
import {EditorDraftCache,editorCacheRestoreMode,clearEditorDraftCaches,parseCachedEditorDraft,type CachedEditorDraft} from './editor-draft-cache';
const id='11111111-1111-4111-8111-111111111111';
const other='22222222-2222-4222-8222-222222222222';
const value=():CachedEditorDraft=>({schema_version:1,base:{asset_version_id:id,source_sha256:'a'.repeat(64),revision:'4',params:{schema_version:1,modules:{}},mask_refs:[]},local:{params:{schema_version:1,modules:{strength:.4}},maskRefs:[]},saved_at:Date.now()});
function storage(){const items=new Map<string,string>();return {items,getItem:async(k:string)=>items.get(k)??null,setItem:async(k:string,v:string)=>{items.set(k,v)},removeItem:async(k:string)=>{items.delete(k)},getAllKeys:async()=>[...items.keys()]}}
it('scopes drafts to user tenant server and version',async()=>{
 const s=storage(),a=new EditorDraftCache(s,'https://a',id,id,id);
 await a.write(value());expect((await a.read())?.local.params.modules).toEqual({strength:.4});
 for(const cache of [new EditorDraftCache(s,'https://b',id,id,id),new EditorDraftCache(s,'https://a',other,id,id),new EditorDraftCache(s,'https://a',id,other,id),new EditorDraftCache(s,'https://a',id,id,other)])expect(await cache.read()).toBeNull();
});
it('rejects stale malformed or cross-version data',()=>{
 const v=value();expect(parseCachedEditorDraft(JSON.stringify(v),other)).toBeNull();
 for(const altered of [{...v,saved_at:0},{...v,base:{...v.base,revision:'9007199254740992'}},{...v,local:{...v.local,params:{schema_version:2,modules:{}}}},{...v,base:{...v.base,source_sha256:'wrong'}}])expect(parseCachedEditorDraft(JSON.stringify(altered),id)).toBeNull();
});
it('logout prevents old stores from resurrecting drafts and preserves other servers',async()=>{
 const s=storage(),old=new EditorDraftCache(s,'https://logout',id,id,id),unrelated=new EditorDraftCache(s,'https://else',id,id,id);
 await old.write(value());await unrelated.write(value());
 const clear=clearEditorDraftCaches(s,'https://logout'),late=old.write(value());await Promise.all([clear,late]);
 expect(await old.read()).toBeNull();expect(await unrelated.read()).not.toBeNull();
 expect(s.items.has(old.key)).toBe(false);
 const fresh=new EditorDraftCache(s,'https://logout',id,id,id);await fresh.write(value());expect(await fresh.read()).not.toBeNull();
});

it('restores only the same source and compares a changed server revision',()=>{
 const cached=value();
 expect(editorCacheRestoreMode(cached,cached.base)).toBe('restore');
 expect(editorCacheRestoreMode(cached,{...cached.base,revision:'5'})).toBe('conflict');
 expect(editorCacheRestoreMode(cached,{...cached.base,source_sha256:'b'.repeat(64)})).toBe('source_changed');
 expect(editorCacheRestoreMode(cached,{...cached.base,asset_version_id:other})).toBe('source_changed');
});
