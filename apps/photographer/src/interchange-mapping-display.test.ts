import {expect,it} from 'vitest';
import {xmpMappingDisplay} from './interchange-mapping-display';
const t=()=> 'XMP settings not applied: {count}. Original sidecar files are retained.';
it('shows unsupported count without displaying evidence digest',()=>{
 const hash='a'.repeat(64);
 const text=xmpMappingDisplay({name:'xmp_properties',status:'unsupported',reason:`unsupported_xmp_properties:12:${hash}`},t);
 expect(text).toBe('XMP settings not applied: 12. Original sidecar files are retained.');
 expect(text).not.toContain(hash);expect(text).not.toContain('unsupported_xmp_properties');
});
it.each([null,'unknown','unsupported_xmp_properties:0:'+ 'a'.repeat(64),'unsupported_xmp_properties:1:bad'])('hides malformed evidence reason %s',reason=>{
 expect(xmpMappingDisplay({name:'xmp_properties',status:'unsupported',reason},t)).toBe('XMP settings not applied: ?. Original sidecar files are retained.');
});
it('leaves other mapping fields to existing display',()=>{
 expect(xmpMappingDisplay({name:'recipe',status:'unsupported',reason:'recipe_not_present'},t)).toBeNull();
});
