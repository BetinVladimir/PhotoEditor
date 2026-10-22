import type {EditorDraft} from './catalog-types';
import {scopeUUID} from './workspace-access';
const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
export function readLegacyEditorDraft(value:unknown,expected?:Pick<EditorDraft,'asset_version_id'|'source_sha256'>):EditorDraft{
 if(!record(value))throw new Error('EDITOR_DRAFT_RESPONSE_INVALID');
 const required=['asset_version_id','revision','params','mask_refs','source_sha256'];
 const allowed=[...required,'id','last_preview_run_id'];
 if(required.some(key=>!Object.hasOwn(value,key))||Object.keys(value).some(key=>!allowed.includes(key))||!scopeUUID(value.asset_version_id)||typeof value.revision!=='string'||!/^(0|[1-9][0-9]{0,18})$/.test(value.revision)||BigInt(value.revision)>9223372036854775807n||!record(value.params)||!Array.isArray(value.mask_refs)||typeof value.source_sha256!=='string'||!/^[0-9a-f]{64}$/.test(value.source_sha256)||value.id!==undefined&&!scopeUUID(value.id)||value.last_preview_run_id!==undefined&&!scopeUUID(value.last_preview_run_id)||expected&&(value.asset_version_id!==expected.asset_version_id||value.source_sha256!==expected.source_sha256))throw new Error('EDITOR_DRAFT_RESPONSE_INVALID');
 return structuredClone(value) as EditorDraft;
}
