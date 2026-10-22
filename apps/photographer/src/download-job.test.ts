import {expect,it} from 'vitest';
import {readDownloadJob,readDownloadAccepted,downloadJobTerminal} from './download-job';
it('binds archive status to the requested job and rejects duplicate volumes',()=>{
 const value={id:'job',status:'ready',files_done:2,files_total:2,bytes_written:100,volumes:[{number:1,status:'ready',bytes:100,delivery_kind:'zip'}]};
 expect(readDownloadJob(value,'job')).toEqual(value);
 expect(()=>readDownloadJob(value,'foreign')).toThrow();
 expect(()=>readDownloadJob({...value,volumes:[...value.volumes,...value.volumes]},'job')).toThrow();
 expect(downloadJobTerminal('ready')).toBe(true);expect(downloadJobTerminal('cancelling')).toBe(false);
});

it('reads only immutable queued creation receipts',()=>{
 const receipt={download_job_id:'11111111-1111-4111-8111-111111111111',status:'queued'};
 expect(readDownloadAccepted(receipt)).toEqual(receipt);
 for(const changed of [{...receipt,status:'ready'},{...receipt,download_job_id:'bad'},{...receipt,secret:'unexpected'},{status:'queued'}])expect(()=>readDownloadAccepted(changed)).toThrow();
});
it('rejects non-wire status fields and detaches nested failure data',()=>{
 const value={id:'job',status:'failed',files_done:0,files_total:1,bytes_written:0,volumes:[],error:{code:'FAILED',details:{message:'original'}}};
 const decoded=readDownloadJob(value,'job');value.error.details.message='changed';
 expect(decoded.error).toEqual({code:'FAILED',details:{message:'original'}});
 expect(()=>readDownloadJob({...value,internal_secret:'private'},'job')).toThrow();
 expect(()=>readDownloadJob({...value,expires_at:null},'job')).toThrow();
 expect(()=>readDownloadJob({...value,volumes:[{number:1,status:'ready',bytes:1,delivery_kind:'zip',artifact_id:'private'}]},'job')).toThrow();
});
