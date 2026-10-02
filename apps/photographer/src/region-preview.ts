export type RegionPreviewRequest={version_id:string;recipe:{artifact_id:string;sha256:string};rect:{x0:number;y0:number;x1:number;y1:number};max_edge:number};
export type RegionJobTicket={run_id:string;workflow_id:string;state:'queued'};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)&&!/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(value);
function fields(value:unknown,keys:string[]):value is Record<string,unknown>{return typeof value==='object'&&value!==null&&!Array.isArray(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.prototype.hasOwnProperty.call(value,key));}

/** Snapshot request data before token refresh/network awaits; caller owns retry key. */
export function freezeRegionRequest(value:RegionPreviewRequest):RegionPreviewRequest {
 if(!fields(value,['version_id','recipe','rect','max_edge'])||!uuid(value.version_id)||!fields(value.recipe,['artifact_id','sha256'])||!uuid(value.recipe.artifact_id)||typeof value.recipe.sha256!=='string'||!/^[0-9a-f]{64}$/.test(value.recipe.sha256)||!fields(value.rect,['x0','y0','x1','y1'])||!Number.isInteger(value.max_edge)||value.max_edge<64||value.max_edge>1024)throw new Error('REGION_PREVIEW_INVALID');
 const {x0,y0,x1,y1}=value.rect;
 if([x0,y0,x1,y1].some(n=>typeof n!=='number'||!Number.isFinite(n)||n<0||n>1)||x1<=x0||y1<=y0)throw new Error('REGION_PREVIEW_INVALID');
 return {version_id:value.version_id,recipe:{artifact_id:value.recipe.artifact_id,sha256:value.recipe.sha256},rect:{x0,y0,x1,y1},max_edge:value.max_edge};
}

export function regionJobTicket(value:unknown):RegionJobTicket {
 if(!fields(value,['run_id','workflow_id','state'])||!uuid(value.run_id)||!uuid(value.workflow_id)||value.state!=='queued')throw new Error('REGION_TICKET_INVALID');
 return {run_id:value.run_id,workflow_id:value.workflow_id,state:'queued'};
}
