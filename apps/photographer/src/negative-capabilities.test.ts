import {expect,it} from 'vitest';
import {readNegativeCapabilities} from './negative-capabilities';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const make=()=>({schema_version:1,tenant_id:id(1),project_id:id(2),calibration:{artifact_id:id(3),sha256:'a'.repeat(64)},max_pixels:200000000});
it('owns the project-scoped calibration reference',()=>{
 const raw=make(),got=readNegativeCapabilities(raw,id(1),id(2));raw.calibration.sha256='b'.repeat(64);expect(got.calibration.sha256).toBe('a'.repeat(64));
});
it.each(['tenant','project','calibration','fraction','overflow','extra'])('rejects invalid capabilities: %s',fault=>{
 const raw:Record<string,unknown>=make();
 if(fault==='tenant')raw.tenant_id=id(9);if(fault==='project')raw.project_id=id(9);
 if(fault==='calibration')raw.calibration={artifact_id:id(3),sha256:'bad'};
 if(fault==='fraction')raw.max_pixels=1.5;if(fault==='overflow')raw.max_pixels=200000001;if(fault==='extra')raw.policy={};
 expect(()=>readNegativeCapabilities(raw,id(1),id(2))).toThrow('NEGATIVE_CAPABILITIES_INVALID');
});
