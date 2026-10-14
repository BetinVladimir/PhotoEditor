import type {SceneBoard} from './scene-board';
export type SceneApplyDecision={plan_sha:string;approved_groups:string[];recipe:{artifact_id:string;sha256:string};expected_revision:string};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const hash=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));}
/** Copy before asynchronous admission; retries must reuse this decision and its key. */
export function freezeSceneApplyDecision(value:SceneApplyDecision):SceneApplyDecision{
 if(!fields(value,['plan_sha','approved_groups','recipe','expected_revision'])||!hash(value.plan_sha)||!fields(value.recipe,['artifact_id','sha256'])||!uuid(value.recipe.artifact_id)||!hash(value.recipe.sha256)||typeof value.expected_revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(value.expected_revision)||BigInt(value.expected_revision)>9223372036854775807n||!Array.isArray(value.approved_groups)||value.approved_groups.length<1||value.approved_groups.length>5000||value.approved_groups.some(id=>!uuid(id))||new Set(value.approved_groups).size!==value.approved_groups.length)throw new Error('SCENE_APPLY_INVALID');
 return {plan_sha:value.plan_sha,approved_groups:[...value.approved_groups],recipe:{...value.recipe},expected_revision:value.expected_revision};
}
/** Omitted groups are rejected. Never infer approval from reference selection. */
export function sceneApplyDecision(board:SceneBoard,approved:readonly string[],recipe:SceneApplyDecision['recipe'],revision:string):SceneApplyDecision{
 const groups=new Set(board.groups.map(group=>group.id));
 if(approved.some(id=>!groups.has(id)))throw new Error('SCENE_APPLY_INVALID');
 return freezeSceneApplyDecision({plan_sha:board.planSha??'',approved_groups:[...approved],recipe,expected_revision:revision});
}
