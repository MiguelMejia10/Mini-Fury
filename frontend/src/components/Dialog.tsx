import { useEffect, useRef, useId, type ReactNode } from 'react';
import { X } from 'lucide-react';
export function Dialog({ title, close, children, busy = false }: { title: string; close: () => void; children: ReactNode; busy?: boolean }) {
 const ref=useRef<HTMLElement>(null);const titleID=useId();const onClose=useRef(close);onClose.current=close;
 useEffect(()=>{const previous=document.activeElement as HTMLElement|null;const overflow=document.body.style.overflow;document.body.style.overflow='hidden';ref.current?.querySelector<HTMLElement>('button,input')?.focus();return ()=>{document.body.style.overflow=overflow;previous?.focus();};},[]);
 return <div className="modal-backdrop"><section ref={ref} className="detail-modal catalog-modal" role="dialog" aria-modal="true" aria-labelledby={titleID} onKeyDown={e=>{
  if(e.key==='Escape'){e.preventDefault();e.stopPropagation();if(!busy)onClose.current();return;}
  if(e.key!=='Tab')return;const items=ref.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]');if(!items?.length){e.preventDefault();return;}const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}
 }}><header><span className="eyebrow">APPLICATION REGISTRY</span><button className="icon-button" aria-label="Cerrar ventana" disabled={busy} onClick={close}><X size={18}/></button></header><h2 id={titleID}>{title}</h2>{children}</section></div>;
}
