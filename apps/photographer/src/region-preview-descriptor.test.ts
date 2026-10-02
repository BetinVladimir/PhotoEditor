import {expect,it} from 'vitest';
import {readRegionDescriptor} from './region-preview-descriptor';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,sha='a'.repeat(64);
const request={version_id:id(1),recipe:{artifact_id:id(2),sha256:sha},rect:{x0:0,y0:0,x1:.5,y1:.5},max_edge:1024};
const source={artifact_id:id(3),sha256:sha};
const descriptor=()=>({schema_version:1,asset_id:id(4),request:structuredClone(request),source:{...source},canvas:{width:6000,height:4000},params_sha256:sha,icc_sha256:'b'.repeat(64),alpha:false});
it('binds the admitted request and returns copied manifest evidence',()=>{const raw=descriptor(),bound=readRegionDescriptor(raw,id(4),request,source);raw.canvas.width=1;raw.request.rect.x1=1;expect(bound.canvas.width).toBe(6000);expect(bound.request.rect.x1).toBe(.5);});
it('rejects substituted request, source, asset, geometry and unexpected fields',()=>{
 for(const raw of [{...descriptor(),asset_id:id(5)},{...descriptor(),source:{...source,sha256:'c'.repeat(64)}},{...descriptor(),request:{...request,max_edge:512}},{...descriptor(),canvas:{width:1.5,height:4000}},{...descriptor(),bucket:'private'},{...descriptor(),icc_sha256:'invalid'}])expect(()=>readRegionDescriptor(raw,id(4),request,source)).toThrow();
});
