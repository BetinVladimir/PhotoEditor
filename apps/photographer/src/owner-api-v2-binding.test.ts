import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const binding=()=>({asset_version_id:id(1),source:{artifact_id:id(2),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)});
it('decodes and owns the actual V2 source DTO before building an initial recipe',async()=>{
 const source=binding(),request=vi.fn().mockResolvedValue(source),api=new OwnerAPI({request} as never,id(3));
 const result=await api.v2Binding(id(4));source.source.sha256='c'.repeat(64);expect(result.source.sha256).toBe('a'.repeat(64));
 for(const malformed of [{...binding(),extra:1},{...binding(),source:{artifact_id:'bad',sha256:'a'.repeat(64)}},{...binding(),canvas_hash:'bad'}]){request.mockResolvedValueOnce(malformed);await expect(api.v2Binding(id(4))).rejects.toThrow('V2_SOURCE_BINDING_INVALID');}
});
it('refuses a malformed asset or pre-aborted binding read before transport',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(3)),controller=new AbortController();controller.abort();
 await expect(api.v2Binding('foreign/id')).rejects.toThrow();await expect(api.v2Binding(id(4),controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
});
