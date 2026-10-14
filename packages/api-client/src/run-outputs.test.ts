import {describe,it,expect,vi} from 'vitest';
import {decodeRunOutputs,listRunOutputs} from './run-outputs';
const tenant='11111111-1111-4111-8111-111111111111';
const output={artifact_id:tenant,role:'derivative',sha256:'a'.repeat(64),bytes:123,media_type:'image/webp',url:'https://objects.example/image?signature=fixture',expires_at:'2030-01-01T00:00:00Z'};
describe('run outputs',()=>{
 it('owns validated output data',()=>{const page={items:[{...output}]};const decoded=decodeRunOutputs(page);page.items[0].url='changed';expect(decoded.items[0].url).toBe(output.url)});
 it.each(['javascript:alert(1)','http://remote.example/image','https://user:pass@example/image','https://example/image#fragment'])('rejects unsafe URL %s',url=>expect(()=>decodeRunOutputs({items:[{...output,url}]})).toThrow());
 it('rejects duplicate IDs and unsafe size',()=>{expect(()=>decodeRunOutputs({items:[output,output]})).toThrow();expect(()=>decodeRunOutputs({items:[{...output,bytes:Number.MAX_SAFE_INTEGER+1}]})).toThrow()});
 it('preserves cancellation and validates IDs before IO',async()=>{const signal=new AbortController().signal;const request=vi.fn().mockResolvedValue({items:[output]});await listRunOutputs({request},tenant,tenant,signal);expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${tenant}/runs/${tenant}/outputs`,{signal});request.mockClear();await expect(listRunOutputs({request},'bad',tenant)).rejects.toThrow();expect(request).not.toHaveBeenCalled()});
});
