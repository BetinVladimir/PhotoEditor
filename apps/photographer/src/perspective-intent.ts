import type {ArtifactRef,V2VersionBinding} from './catalog-types';
export type PerspectivePoint={x:number;y:number};
export type PerspectiveParams={schema_version:1;base:V2VersionBinding;strength:number;mode:'manual'|'auto';corners:PerspectivePoint[]|null;lines:ArtifactRef|null;max_crop_fraction:number};
export type PerspectiveIntent={schema_version:1;snapshot_id:string;params:PerspectiveParams};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const sha=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
const unit=(v:unknown):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=0&&v<=1;
function ref(v:unknown):v is ArtifactRef{return fields(v,['artifact_id','sha256'])&&uuid(v.artifact_id)&&sha(v.sha256);}
export function freezePerspectiveIntent(v:PerspectiveIntent):PerspectiveIntent{
 const invalid=()=>{throw new Error('PERSPECTIVE_INTENT_INVALID');};
 if(!fields(v,['schema_version','snapshot_id','params'])||v.schema_version!==1||!uuid(v.snapshot_id))invalid();
 const p=v.params;
 if(!fields(p,['schema_version','base','strength','mode','corners','lines','max_crop_fraction'])||p.schema_version!==1||!fields(p.base,['asset_version_id','source','canvas_hash'])||!uuid(p.base.asset_version_id)||!ref(p.base.source)||!sha(p.base.canvas_hash)||!unit(p.strength)||!unit(p.max_crop_fraction))invalid();
 if(p.mode==='manual'){
  if(!Array.isArray(p.corners)||p.corners.length!==4||p.lines!==null)invalid();
  const corners=p.corners!;
  for(const point of corners)if(!fields(point,['x','y'])||!unit(point.x)||!unit(point.y))invalid();
  for(let i=0;i<4;i++){const a=corners[i],b=corners[(i+1)%4],c=corners[(i+2)%4];if((b.x-a.x)*(c.y-b.y)-(b.y-a.y)*(c.x-b.x)<=0)invalid();}
 }else if(p.mode==='auto'){if(p.corners!==null||!ref(p.lines))invalid();}else invalid();
 return structuredClone(v);
}
