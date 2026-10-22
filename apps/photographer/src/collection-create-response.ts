import type {Collection} from './catalog-types';
import {linkUUID} from './collection-access-links-api';
export function readCollectionCreatedEnvelope(raw:unknown):{collection:unknown;initial_share?:unknown}{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('CREATE_RECEIPT_INVALID');
 const value=raw as Record<string,unknown>;
 if(!Object.hasOwn(value,'collection')||Object.keys(value).some(key=>key!=='collection'&&key!=='initial_share')||Object.hasOwn(value,'initial_share')&&value.initial_share===null)throw Error('CREATE_RECEIPT_INVALID');
 return structuredClone(value) as {collection:unknown;initial_share?:unknown};
}
export function readCreatedCollection(raw:unknown,tenant:string,project:string):Collection {
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('COLLECTION_INVALID');const v=raw as Record<string,unknown>;
 const optional=['cover_item_id','source_collection_id','source_run_id','applied_style_version_id'];
 if(Object.keys(v).some(k=>!['id','tenant_id','project_id','name','description','state','revision','sort_mode','created_at','source_revision',...optional].includes(k))||!linkUUID(v.id)||v.tenant_id!==tenant||v.project_id!==project||typeof v.name!=='string'||!v.name.trim()||Array.from(v.name).length>120||typeof v.description!=='string'||Array.from(v.description).length>2000||typeof v.state!=='string'||!['draft','published','archived','deleting'].includes(v.state)||typeof v.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.revision)||v.revision.length===19&&v.revision>'9223372036854775807'||typeof v.sort_mode!=='string'||!['manual','captured_at','filename'].includes(v.sort_mode)||typeof v.created_at!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(v.created_at)||!Number.isFinite(Date.parse(v.created_at))||optional.some(k=>v[k]!==undefined&&!linkUUID(v[k]))||v.source_revision!==undefined&&(!Number.isSafeInteger(v.source_revision)||Number(v.source_revision)<1))throw Error('COLLECTION_INVALID');
 return {...v} as Collection;
}
