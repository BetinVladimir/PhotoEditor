import {expect,it} from 'vitest';
import {readRunStatusResponse} from './run-status-response';
const status={id:'run',kind:'bulk',status:'running',revision:'9007199254740993',stage:'render',progress:0.5,counts:{total:2,succeeded:1,failed:0,skipped:0,review:0},errors:[]};
it('preserves an exact int64 revision and owns the accepted DTO',()=>{
 const value=readRunStatusResponse(status,'run');expect(value.revision).toBe(status.revision);value.counts.total=3;expect(status.counts.total).toBe(2);
});
it.each([
 {...status,id:'other'}, {...status,revision:'9223372036854775808'}, {...status,revision:2},
 {...status,progress:NaN}, {...status,progress:2}, {...status,counts:{...status.counts,total:'2'}},
 {...status,errors:[{}]}, {...status,counts:null}, {...status,status:''},
 {...status,status:'done'}, {...status,output_collection_id:'other/collection'}, {...status,presentation:{total:1,pending:0,ready:1,failed:0,cancelled:0,unavailable:'0'}},
])('refuses malformed or cross-run mutation receipt',value=>{expect(()=>readRunStatusResponse(value,'run')).toThrow('RUN_STATUS_RESPONSE_INVALID');});
