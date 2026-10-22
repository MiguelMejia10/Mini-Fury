import { useCallback, useEffect, useRef, useState } from 'react';
import { listApplications } from '../services/applications';
import { emptyFilters, type CatalogFilters, type RegisteredApplication } from '../types/catalog';
export function useApplicationCatalog(environment: string) {
 const [applications,setApplications]=useState<RegisteredApplication[]>([]);const [loading,setLoading]=useState(true);const [error,setError]=useState('');const [hasMore,setHasMore]=useState(false);const [filters,setFilters]=useState<CatalogFilters>(emptyFilters);const nextOffset=useRef(0);const controller=useRef<AbortController|null>(null);
 const load=useCallback(async(append:boolean)=>{
  controller.current?.abort();const current=new AbortController();controller.current=current;setLoading(true);setError('');
  if(!append){setApplications([]);setHasMore(false);}
  try{const data=await listApplications(AbortSignal.any([current.signal,AbortSignal.timeout(12000)]),append?nextOffset.current:0,filters,environment);if(current.signal.aborted)return;setApplications(old=>append?[...old,...data.applications.filter(a=>!old.some(existing=>existing.id===a.id))]:data.applications);setHasMore(data.hasMore);nextOffset.current=data.nextOffset;}
  catch(err){if(current.signal.aborted)return;setError(err instanceof Error?err.message:'No se pudo cargar el catálogo.');}
  finally{if(controller.current===current)setLoading(false);}
 },[filters,environment]);
 const refresh=useCallback(()=>load(false),[load]);const loadMore=useCallback(()=>load(true),[load]);
 useEffect(()=>{void refresh();return ()=>controller.current?.abort();},[refresh]);
 return{applications,loading,error,refresh,hasMore,loadMore,filters,setFilters};
}
