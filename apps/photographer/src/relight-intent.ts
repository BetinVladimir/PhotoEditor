import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type RelightLight={x:number;y:number;radius:number;ev:number};
export type RelightParams={schema_version:1;base:V2VersionBinding;strength:number;depth:ArtifactRef;subject:ArtifactRef;protected:ArtifactRef;lights:RelightLight[];calibration:ArtifactRef};
export type RelightIntent={schema_version:1;snapshot_id:string;ordinal:number;subject_kind:'person'|'manual';params:RelightParams};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
const range=(v:unknown,a:number,b:number):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=a&&v<=b;
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
/** Own the exact authored request before async auth/network; never send deployment authority. */
export function freezeRelightIntent(v:RelightIntent):RelightIntent{
 if(!fields(v,['schema_version','snapshot_id','ordinal','subject_kind','params'])||v.schema_version!==1||!uuid(v.snapshot_id)||!Number.isInteger(v.ordinal)||v.ordinal<0||v.ordinal>=5000||!['person','manual'].includes(v.subject_kind))throw new Error('RELIGHT_INTENT_INVALID');
 const p=v.params;
 if(!fields(p,['schema_version','base','strength','depth','subject','protected','lights','calibration'])||p.schema_version!==1||!fields(p.base,['asset_version_id','source','canvas_hash'])||!uuid(p.base.asset_version_id)||!ref(p.base.source)||!sha(p.base.canvas_hash)||!range(p.strength,0,1)||![p.depth,p.subject,p.protected,p.calibration].every(ref)||!Array.isArray(p.lights)||p.lights.length<1||p.lights.length>4)throw new Error('RELIGHT_INTENT_INVALID');
 for(const light of p.lights){if(!fields(light,['x','y','radius','ev'])||!range(light.x,-1,2)||!range(light.y,-1,2)||!range(light.radius,.05,2)||!range(light.ev,-1,1))throw new Error('RELIGHT_INTENT_INVALID');}
 return structuredClone(v);
}
