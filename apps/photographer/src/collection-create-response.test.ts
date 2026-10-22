import {expect,it} from 'vitest';
import {readCreatedCollection,readCollectionCreatedEnvelope} from './collection-create-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(3),tenant_id:id(1),project_id:id(2),name:'Empty gallery',description:'',state:'published',revision:'2',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'});
it('requires the exact created envelope before acknowledging a durable create or abandon receipt',()=>{
 const raw={collection:row()},owned=readCollectionCreatedEnvelope(raw);raw.collection.name='late';expect((owned.collection as {name:string}).name).toBe('Empty gallery');
 for(const value of [null,[],{}, {collection:row(),initial_share:null},{collection:row(),secret:'leak'},{collection:row(),status:'abandoned'}])expect(()=>readCollectionCreatedEnvelope(value)).toThrow();
 expect(readCollectionCreatedEnvelope({collection:row(),initial_share:{share:'checked separately'}}).initial_share).toEqual({share:'checked separately'});
});
it('accepts actual complete Go projection including readable deleting current state',()=>{expect(readCreatedCollection(row(),id(1),id(2))).toEqual(row());expect(readCreatedCollection({...row(),state:'deleting',revision:'9223372036854775807'},id(1),id(2)).state).toBe('deleting');});
it('refuses forged scope, malformed current projection and inexact revision',()=>{for(const v of [{...row(),tenant_id:id(8)},{...row(),project_id:id(8)},{...row(),id:'invalid'},{...row(),created_at:'invalid'},{...row(),revision:'9223372036854775808'},{...row(),state:'unknown'},{...row(),sort_mode:'unknown'},{...row(),secret:'leak'},{...row(),source_revision:9007199254740992}])expect(()=>readCreatedCollection(v,id(1),id(2))).toThrow();});
it('refuses JSON arrays masquerading as enum strings and non-wire date shortcuts',()=>{
 for(const value of [{...row(),state:['draft']},{...row(),sort_mode:['manual']},{...row(),created_at:'1'},{...row(),created_at:'2026-10-17'}])expect(()=>readCreatedCollection(value,id(1),id(2))).toThrow('COLLECTION_INVALID');
});
it('preserves Go timestamp precision and explicit offsets without coercing fields',()=>{
 for(const created_at of ['2026-10-17T10:00:00.123456789Z','2026-10-17T13:00:00+03:00'])expect(readCreatedCollection({...row(),created_at},id(1),id(2)).created_at).toBe(created_at);
});
