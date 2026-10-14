type MappingField={name:string;status:string;reason:string|null};
export function xmpMappingDisplay(field:MappingField,t:(key:'interchange_xmp_unsupported')=>string):string|null{
 if(field.name!=='xmp_properties')return null;
 const match=field.status==='unsupported'&&field.reason?.match(/^unsupported_xmp_properties:([1-9][0-9]{0,4}):[0-9a-f]{64}$/);
 const count=match?match[1]:'?';
 return t('interchange_xmp_unsupported').replace('{count}',count);
}
