import {router,useLocalSearchParams} from 'expo-router';
import {InterchangeExportFormatScreen} from '../../src/interchange-export-format-screen';
export default function FormatRoute(){
 const {tenant,project,snapshot}=useLocalSearchParams<{tenant:string;project:string;snapshot:string}>();
 return <InterchangeExportFormatScreen key={JSON.stringify([tenant,project,snapshot])} tenant={tenant||''} project={project||''} snapshot={snapshot||''} onSelect={request=>{
  if(request.direction!=='export')return;
  router.push({pathname:'/interchange/new',params:{tenant,project:request.project_id,direction:'export',format:request.format,snapshot:request.snapshot_id}});
 }}/>;
}
