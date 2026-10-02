import type {ShootSessionPolicy} from './capture-types';
/** Change only grouping controls; retain calibration, manual-pick protection and camera offsets. */
export function editSessionPolicy(policy:ShootSessionPolicy,timeGap:string,neighbors:string):ShootSessionPolicy{
 if(!/^[0-9]+$/.test(timeGap)||!/^[0-9]+$/.test(neighbors))throw new Error('SESSION_POLICY_INVALID');
 const gap=Number(timeGap),count=Number(neighbors);
 if(!Number.isSafeInteger(gap)||gap<30||gap>7200||!Number.isSafeInteger(count)||count<1||count>64)throw new Error('SESSION_POLICY_INVALID');
 return {...structuredClone(policy),time_gap_seconds:gap,neighbor_count:count};
}
