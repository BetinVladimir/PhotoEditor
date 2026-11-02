import {expect,it} from 'vitest';
import {loadVector} from './video-test-vectors';
import {snapTime} from './video-timeline-snap';
const v=loadVector<{cases:{id:string;candidate:number;anchors:number[];threshold:number;frame_ticks:number;expect:number}[]}>('snap_cases');
it('TC-T-SNAP-01…06',()=>{expect(v.cases.length).toBe(6);for(const c of v.cases)expect(snapTime(c.candidate,c.anchors,c.threshold,c.frame_ticks),c.id).toBe(c.expect);});
