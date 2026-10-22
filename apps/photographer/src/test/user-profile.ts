import type {UserProfile} from '../catalog-types';
/** Positive authenticated profile wire fixture. Malformed response tests must build their own values. */
export function userProfile(id:string):UserProfile{return {id,email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};}

export function tenantSettings(id:string,role:'owner'|'admin'|'editor'|'viewer'='editor'){return {id,name:'Fixture workspace',role,revision:'1',quotas:{storage_bytes:1073741824,source_files:1000,max_active_gpu_tasks:2},retention:{processing_days:30,exports_days:90,temporary_hours:24}};}
