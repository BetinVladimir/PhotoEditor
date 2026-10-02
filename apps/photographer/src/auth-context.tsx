import AsyncStorage from '@react-native-async-storage/async-storage';
import {clearCaptureDrafts} from './capture-draft-store';
import {clearEditorDraftCaches} from './editor-draft-cache';
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { OwnerAuth } from "./auth";
import { Platform } from "react-native";
import { ActivityIndicator, View } from "react-native";
const AuthContext=createContext<OwnerAuth|null>(null);
export function AuthProvider({children}:{children:ReactNode}){const auth=useMemo(()=>new OwnerAuth(process.env.EXPO_PUBLIC_API_ORIGIN||"",Platform.OS==="web"?"web":"native",fetch,async()=>{const origin=process.env.EXPO_PUBLIC_API_ORIGIN||"";await Promise.all([clearCaptureDrafts(AsyncStorage,origin),clearEditorDraftCaches(AsyncStorage,origin)])}),[]);const[ready,setReady]=useState(false);useEffect(()=>{let active=true;void auth.restore().finally(()=>{if(active)setReady(true)});return()=>{active=false}},[auth]);if(!ready)return <View style={{flex:1,backgroundColor:"#222",justifyContent:"center"}}><ActivityIndicator/></View>;return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>}
export function useAuth(){const value=useContext(AuthContext);if(!value)throw new Error("AuthProvider missing");return value}
