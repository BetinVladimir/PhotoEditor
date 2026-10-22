import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type NegativeParams={schema_version:1;base:V2VersionBinding;strength:number;film_base_rgb:[number,number,number];calibration:ArtifactRef;exposure_ev:number};
export type NegativeIntent={schema_version:1;snapshot_id:string;params:NegativeParams};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
const range=(v:unknown,lo:number,hi:number):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=lo&&v<=hi;
/** Own the exact retry body before persistence, token refresh or network awaits. */
export function freezeNegativeIntent(v:NegativeIntent):NegativeIntent {
 const invalid=():never=>{throw new Error('NEGATIVE_INTENT_INVALID')};
 if(!fields(v,['schema_version','snapshot_id','params'])||v.schema_version!==1||!uuid(v.snapshot_id))return invalid();
 const p=v.params;
 if(!fields(p,['schema_version','base','strength','film_base_rgb','calibration','exposure_ev'])||p.schema_version!==1||!fields(p.base,['asset_version_id','source','canvas_hash'])||!uuid(p.base.asset_version_id)||!ref(p.base.source)||!sha(p.base.canvas_hash)||!range(p.strength,0,1)||!range(p.exposure_ev,-3,3)||!ref(p.calibration)||!Array.isArray(p.film_base_rgb)||p.film_base_rgb.length!==3||!p.film_base_rgb.every(n=>range(n,0.000001,1)))return invalid();
 return structuredClone(v);
}

export function verifyNegativeRecoverySource(v:NegativeIntent,live:V2VersionBinding):NegativeIntent{
 const frozen=freezeNegativeIntent(v),base=frozen.params.base;
 if(!fields(live,['asset_version_id','source','canvas_hash'])||!uuid(live.asset_version_id)||!ref(live.source)||!sha(live.canvas_hash)||base.asset_version_id!==live.asset_version_id||base.source.artifact_id!==live.source.artifact_id||base.source.sha256!==live.source.sha256||base.canvas_hash!==live.canvas_hash)throw new Error('NEGATIVE_RECOVERY_SOURCE_CHANGED');
 return frozen;
}
