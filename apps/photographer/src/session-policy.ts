import type {ShootSessionPolicy} from './capture-types';
/** Change only grouping controls; retain calibration, manual-pick protection and camera offsets. */
export function editSessionPolicy(policy:ShootSessionPolicy,timeGap:string,neighbors:string,protectManualPicks?:boolean,cameraOffsets?:readonly {camera_hash:string;seconds:string}[]):ShootSessionPolicy{
 if((protectManualPicks!==undefined&&typeof protectManualPicks!=='boolean')||!/^[0-9]+$/.test(timeGap)||!/^[0-9]+$/.test(neighbors))throw new Error('SESSION_POLICY_INVALID');
 const gap=Number(timeGap),count=Number(neighbors);
 if(!Number.isSafeInteger(gap)||gap<30||gap>7200||!Number.isSafeInteger(count)||count<1||count>64)throw new Error('SESSION_POLICY_INVALID');
 if(cameraOffsets!==undefined&&(!Array.isArray(cameraOffsets)||cameraOffsets.length>32))throw new Error('SESSION_POLICY_INVALID');
 const offsets=cameraOffsets?.map(value=>{if(!value||typeof value.camera_hash!=='string'||!/^[0-9a-f]{64}$/.test(value.camera_hash)||typeof value.seconds!=='string'||! /^-?(0|[1-9][0-9]{0,4})$/.test(value.seconds)||Math.abs(Number(value.seconds))>86400)throw new Error('SESSION_POLICY_INVALID');return {camera_hash:value.camera_hash,seconds:Number(value.seconds)};});
 if(offsets&&(offsets.length>32||new Set(offsets.map(value=>value.camera_hash)).size!==offsets.length))throw new Error('SESSION_POLICY_INVALID');
 return {...structuredClone(policy),time_gap_seconds:gap,neighbor_count:count,...(protectManualPicks===undefined?{}:{protect_manual_picks:protectManualPicks}),...(offsets===undefined?{}:{camera_offsets:offsets})};
}

/** Same 1..200 Unicode-code-point bound as the server; reject lone surrogates. */
export function sessionName(value:string):string{
 if(typeof value!=='string'||value.length<1||value.length>400)throw new Error('SESSION_NAME_INVALID');
 const chars=Array.from(value);if(chars.length>200||chars.some(char=>char.length===1&&char.charCodeAt(0)>=0xd800&&char.charCodeAt(0)<=0xdfff))throw new Error('SESSION_NAME_INVALID');
 return value;
}
