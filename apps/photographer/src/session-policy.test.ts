import {expect,it} from 'vitest';
import {editSessionPolicy} from './session-policy';
import type {ShootSessionPolicy} from './capture-types';
const policy:ShootSessionPolicy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[{camera_hash:'a'.repeat(64),seconds:2}],calibration:{artifact_id:'calibration',sha256:'b'.repeat(64)}};
it('updates grouping controls while preserving quality/calibration settings',()=>{const updated=editSessionPolicy(policy,'7200','64');expect(updated).toEqual({...policy,time_gap_seconds:7200,neighbor_count:64});updated.camera_offsets[0].seconds=4;expect(policy.camera_offsets[0].seconds).toBe(2);});
it('rejects malformed and out-of-range grouping values',()=>{for(const [gap,count] of [['29','4'],['7201','4'],['60','0'],['60','65'],['1e3','4'],['60','1.5']])expect(()=>editSessionPolicy(policy,gap,count)).toThrow();});
