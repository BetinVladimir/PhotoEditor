import {useEffect,useMemo,useRef,useState} from "react";
import {router,useLocalSearchParams} from "expo-router";
import {ActivityIndicator,FlatList,Image,Pressable,SafeAreaView,StyleSheet,Text,View} from "react-native";
import {useLocale} from "../../../src/locale-context";
import {useAuth} from "../../../src/auth-context";
import type {Collection,CollectionItem,Page} from "../../../src/catalog-types";
import {OwnerAPI} from "../../../src/owner-api";
import {loadCollectionPages} from "../../../src/collection-pages";
import {saveCompareDecisions} from "../../../src/compare-decisions-save";
import {GalleryComparison} from "../../../src/gallery-comparison";
import {catalogStyle as s} from "../../../src/catalog-style";

async function bounded<T>(items:T[],count:number,task:(item:T)=>Promise<void>){
  let next=0;
  await Promise.all(Array.from({length:Math.min(count,items.length)},async()=>{
    while(next<items.length)await task(items[next++]);
  }));
}

export default function CollectionDetail(){
  const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
  const auth=useAuth();
  const client=useMemo(()=>auth.client(),[auth]);
  return <CollectionScope key={JSON.stringify([tenant,id])} id={id} tenant={tenant} client={client}/>;
}

function CollectionScope({id,tenant,client}:{id:string;tenant:string;client:ReturnType<ReturnType<typeof useAuth>["client"]>}){
  const {t}=useLocale();
  const api=useMemo(()=>new OwnerAPI(client,tenant||""),[client,tenant]);
  const lifetime=useRef<AbortController|null>(null);
  const mutation=useRef(false);
  const[collection,setCollection]=useState<Collection>();
  const[items,setItems]=useState<CollectionItem[]>([]);
  const[links,setLinks]=useState<Record<string,string>>({});
  const[moderating,setModerating]=useState<string>();
  const[updatingLabel,setUpdatingLabel]=useState<string>();
  const[savingComparison,setSavingComparison]=useState(false);
  const[creatingSheet,setCreatingSheet]=useState(false);
  const[busy,setBusy]=useState(true);
  const[reloadEpoch,setReloadEpoch]=useState(0);
  const[error,setError]=useState("");

  useEffect(()=>{
    const controller=new AbortController();
    lifetime.current=controller;
    setCollection(undefined);setItems([]);setLinks({});setError("");setBusy(true);
    async function load(){
      if(!id||!tenant){setError("Collection is required");setBusy(false);return;}
      const root=`/api/v1/tenants/${encodeURIComponent(tenant)}/collections/${encodeURIComponent(id)}`;
      try{
        const c=await client.request<Collection>(root,{signal:controller.signal});
        const loaded=await loadCollectionPages(c,cursor=>client.request<Page<CollectionItem>>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:""}`,{signal:controller.signal}),controller.signal);
        if(controller.signal.aborted)return;
        setCollection(c);setItems(loaded);
        await bounded(loaded.slice(0,24),4,async item=>{
          if(controller.signal.aborted)return;
          try{
            const out=await api.mediaLinks(item.asset_id,item.asset_version_id,["thumb640"],controller.signal);
            const url=out.items.find(x=>x.variant==="thumb640")?.url;
            if(url&&!controller.signal.aborted)setLinks(old=>({...old,[item.id]:url}));
          }catch{/* A derivative may still be queued; never expose the master. */}
        });
      }catch{if(!controller.signal.aborted)setError("Unable to load collection");}
      finally{if(!controller.signal.aborted)setBusy(false);}
    }
    void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
  },[id,tenant,client,api,reloadEpoch]);

  async function moderate(item:CollectionItem,decision:"accept"|"reject"){
    if(mutation.current)return;
    setModerating(item.id);
    try{await saveComparison({[item.asset_version_id]:decision});}
    finally{if(lifetime.current&&!lifetime.current.signal.aborted)setModerating(undefined);}
  }
  async function setColorLabel(item:CollectionItem,label:CollectionItem["color_label"]|null){
    const controller=lifetime.current;
    if(!collection||mutation.current||!controller||controller.signal.aborted)return;
    mutation.current=true;setUpdatingLabel(item.id);setError("");
    try{
      await api.setItemColorLabel(collection,item,label,controller.signal);
      if(controller.signal.aborted||lifetime.current!==controller)return;
      setCollection(undefined);setItems([]);setBusy(true);
      setUpdatingLabel(undefined);
      setReloadEpoch(value=>value+1);
    }catch{
      if(!controller.signal.aborted&&lifetime.current===controller){
        setError("Color label could not be saved. Reload the collection before retrying.");
        setCollection(undefined);
      }
    }finally{
      mutation.current=false;
      if(!controller.signal.aborted&&lifetime.current===controller)setUpdatingLabel(undefined);
    }
  }
  async function saveComparison(decisions:Record<string,"accept"|"reject">){
    const controller=lifetime.current;
    if(!collection||mutation.current||!controller||controller.signal.aborted)return;
    mutation.current=true;setSavingComparison(true);setError("");
    try{
      const result=await saveCompareDecisions(collection,items,decisions,(value,item,decision)=>api.moderateApplyItem(value,item,decision),controller.signal);
      if(controller.signal.aborted||lifetime.current!==controller)return;
      setItems(values=>values.map(item=>result.items.find(updated=>updated.id===item.id)||item));
      const incomplete=result.results.some(value=>value.status!=="saved");
      setCollection(incomplete?undefined:result.collection);
      if(incomplete)setError("Some decisions were not saved. Reload the collection before retrying.");
    }catch{if(!controller.signal.aborted&&lifetime.current===controller)setError("Only pending Apply review frames can be saved. Reload before retrying.");}
    finally{mutation.current=false;if(!controller.signal.aborted&&lifetime.current===controller)setSavingComparison(false);}
  }
  async function createContactSheet(){
    const controller=lifetime.current;
    if(!collection||mutation.current||!controller||controller.signal.aborted)return;
    mutation.current=true;setCreatingSheet(true);setError("");
    try{const snapshot=await api.selectionSnapshot(collection);
      if(controller.signal.aborted||lifetime.current!==controller)return;
      const run=await api.contactSheet(snapshot.id,"en",4,5);
      if(controller.signal.aborted||lifetime.current!==controller)return;
      router.push({pathname:"/runs/[id]",params:{id:run.run_id,tenant}})}
    catch{if(!controller.signal.aborted&&lifetime.current===controller)setError("Contact sheet requires ready previews and an available media worker.")}finally{mutation.current=false;if(!controller.signal.aborted&&lifetime.current===controller)setCreatingSheet(false)}
  }

  if(busy&&!collection)return <SafeAreaView style={s.root}><ActivityIndicator style={s.loader}/></SafeAreaView>;
  return <SafeAreaView style={s.root}>
    <View style={s.header}>
      <Text accessibilityRole="header" style={s.title}>{collection?.name||"Collection"}</Text>
      <Pressable accessibilityRole="button" disabled={busy||savingComparison||creatingSheet||!!moderating} onPress={()=>{if(!mutation.current)setReloadEpoch(value=>value+1);}} style={s.button}><Text style={s.buttonText}>Reload collection</Text></Pressable>
      <Text style={s.subtitle}>{items.length} photos · {collection?.state}</Text>
      <Pressable onPress={()=>router.push({pathname:"/collections/[id]/apply",params:{id,tenant}})} style={s.button}><Text style={s.buttonText}>Apply style</Text></Pressable>
      {([['optical.panorama','multi_create_panorama'],['optical.focus_stack','multi_create_focus']] as const).map(([operation,label])=><Pressable key={operation} accessibilityRole="button" disabled={busy||savingComparison||creatingSheet||!!moderating||!collection} onPress={()=>router.push({pathname:'/collections/[id]/multi',params:{id,tenant,operation}})} style={s.button}><Text style={s.buttonText}>{t(label)}</Text></Pressable>)}
      <Pressable disabled={creatingSheet||!collection||items.length===0} onPress={()=>void createContactSheet()} style={s.button}><Text style={s.buttonText}>{creatingSheet?"Creating contact sheet…":"Create contact sheet"}</Text></Pressable>
      {error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}
    </View>
    <FlatList ListHeaderComponent={<GalleryComparison key={reloadEpoch} items={items} api={api} onSave={collection?.source_run_id?decisions=>void saveComparison(decisions):undefined} saving={savingComparison}/>} data={items} numColumns={2} keyExtractor={x=>x.id} contentContainerStyle={grid.list} columnWrapperStyle={grid.columns}
      ListEmptyComponent={<Text style={s.empty}>No photos</Text>}
      renderItem={({item})=><View style={grid.card}>
        <Pressable accessibilityLabel={`Open photo ${item.position+1}`} onPress={()=>router.push({pathname:"/editor/[version]",params:{version:item.asset_version_id,asset:item.asset_id,tenant,collection:id,item:item.id}})}>
          {links[item.id]?<Image source={{uri:links[item.id]}} style={grid.image} resizeMode="cover"/>:<View style={[grid.image,grid.pending]}><Text style={s.meta}>Preview pending</Text></View>}
          <Text numberOfLines={1} style={s.name}>Photo {item.position+1}</Text>
          <Text style={s.meta}>{item.rating?`${item.rating}★`:item.moderation_state}</Text>
          {item.color_label?<Text style={s.meta} accessibilityLabel={`Color label ${item.color_label}`}>{item.color_label}</Text>:null}
        </Pressable>
        <View style={grid.labels}>
          {([null,"Red","Yellow","Green","Blue","Purple"] as const).map(label=><Pressable key={label??"clear"} accessibilityRole="button" accessibilityLabel={label?`Set ${label} color label for photo ${item.position+1}`:`Clear color label for photo ${item.position+1}`} disabled={!collection||!!updatingLabel||mutation.current||item.color_label===(label??undefined)} onPress={()=>void setColorLabel(item,label)} style={[grid.labelButton,item.color_label===(label??undefined)&&grid.labelSelected]}><Text style={grid.labelText}>{label??"×"}</Text></Pressable>)}
        </View>
        {item.moderation_state==="pending_review"&&collection?.source_run_id?<View style={grid.actions}>
          <Pressable disabled={!!moderating||savingComparison||creatingSheet} accessibilityLabel={`Accept photo ${item.position+1}`} onPress={()=>void moderate(item,"accept")} style={[grid.action,grid.accept]}><Text style={grid.actionText}>Accept</Text></Pressable>
          <Pressable disabled={!!moderating||savingComparison||creatingSheet} accessibilityLabel={`Reject photo ${item.position+1}`} onPress={()=>void moderate(item,"reject")} style={[grid.action,grid.reject]}><Text style={grid.actionText}>Reject</Text></Pressable>
        </View>:null}
      </View>}/>
  </SafeAreaView>;
}

const grid=StyleSheet.create({
  list:{padding:12,gap:12},columns:{gap:12},card:{flex:1,minWidth:0,backgroundColor:"#303030",padding:8,borderRadius:10,gap:8},
  image:{width:"100%",aspectRatio:1,borderRadius:7,backgroundColor:"#444"},pending:{alignItems:"center",justifyContent:"center"},
  actions:{flexDirection:"row",gap:6},action:{flex:1,paddingVertical:8,borderRadius:6,alignItems:"center"},accept:{backgroundColor:"#287a45"},reject:{backgroundColor:"#8b3434"},actionText:{color:"white",fontWeight:"700"},
  labels:{flexDirection:"row",flexWrap:"wrap",gap:4},labelButton:{paddingHorizontal:6,paddingVertical:4,borderRadius:5,backgroundColor:"#454545"},labelSelected:{borderWidth:1,borderColor:"white"},labelText:{color:"white",fontSize:11},
});
