import { Badge, Button, Dialog, Input, SubtabNav, Switch, buttonVariants } from "@braedonsaunders/appkit-ui";
import { Plus, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ActionMenu } from "../../components/ActionMenu";
import { Drawer } from "../../components/Drawer";
import { DomainPage } from "../../components/layout";
import { t } from "../../i18n";
import { apiFetch, ApiError } from "../../lib/api";
import { PluginMark } from "./marks";

interface Manifest {
  id: string; name: string; version: string; description?: string;
  types?: string[]; resources?: (string | {name: string})[];
  catalogs?: {id:string; type:string; name?:string}[];
}
interface Detail {
  id:string; name:string; version:string; enabled:boolean; installedAt:number;
  manifest:Manifest; permissions:{lines:string[]}; impact:{calls:number;errors:number};
}
const resourceNames = (m:Manifest) => (m.resources ?? []).map(r => typeof r === "string" ? r : r.name);
const category = (m:Manifest) => resourceNames(m).includes("subtitles") ? "subtitles" as const : resourceNames(m).includes("stream") ? "sources" as const : "home" as const;
const reason = (e:unknown) => e instanceof ApiError && e.fields?.length ? e.fields.map(f=>`${f.field}: ${f.message}`).join(" · ") : e instanceof Error ? e.message : String(e);
const request = <T,>(path:string,body:unknown) => apiFetch<T>(path,{method:"POST",body:JSON.stringify(body)});

/** Production API transport, kept separate from the explicitly selected mock prototype. */
export default function LivePlugins() {
  const [items,setItems]=useState<Detail[]>([]);
  const [error,setError]=useState("");
  const [loading,setLoading]=useState(true);
  const [open,setOpen]=useState<string|null>(null);
  const [tab,setTab]=useState("overview");
  const [adding,setAdding]=useState(false);
  const [url,setURL]=useState("");
  const [preview,setPreview]=useState<{manifest:Manifest;permissions:string[]}|null>(null);
  const [importError,setImportError]=useState("");
  const [busy,setBusy]=useState(false);
  const load=async()=>{
    try {
      const list=await apiFetch<{plugins:{id:string}[]}>("/plugins/");
      setItems(await Promise.all(list.plugins.map(p=>apiFetch<Detail>(`/plugins/${encodeURIComponent(p.id)}`))));
      setError("");
    }catch(e){setError(reason(e));}finally{setLoading(false);}
  };
  useEffect(()=>{void load();},[]);
  const selected=items.find(p=>p.id===open);
  const inspect=async()=>{
    setBusy(true);setImportError("");setPreview(null);
    try{setPreview(await request("/plugins/install/preview",{kind:"url",url}));}catch(e){setImportError(reason(e));}finally{setBusy(false);}
  };
  const install=async()=>{
    setBusy(true);setImportError("");
    try{await request("/plugins/install",{kind:"url",url});await load();setOpen(preview!.manifest.id);setTab("overview");setAdding(false);}catch(e){setImportError(reason(e));}finally{setBusy(false);}
  };
  return <DomainPage title={t("Plugins")} base="/plugins" actions={<>
    <Button variant="outline" onClick={()=>{setAdding(true);setPreview(null);setImportError("");}}><Plus size={15}/>{t("Import add-on")}</Button>
    <Link to="/plugins/studio" className={buttonVariants()}><Sparkles size={15}/>{t("Build a plugin")}</Link>
  </>}>
    <div className="grid grid-cols-3 gap-2 sm:gap-3">
      {[{label:t("Installed manifests"),value:items.length},{label:t("Enabled"),value:items.filter(p=>p.enabled).length},{label:t("Resource support"),value:t("Not connected")}].map(x=><div key={x.label} className="rounded-xl border border-border bg-surface px-3 py-2.5 sm:px-4 sm:py-3.5"><p className="text-xs text-fg-muted">{x.label}</p><p className="mt-1 text-sm font-semibold text-fg sm:text-2xl">{x.value}</p></div>)}
    </div>
    <p className="text-sm text-fg-muted">{t("Manifests are saved on this server. Native catalogue, stream and subtitle contributions are not connected in this build.")}</p>
    {error?<p role="alert" className="text-danger">{error}</p>:null}
    {loading?<p>{t("Loading")}</p>:null}
    <ul className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" aria-label={t("Installed plugins")}>
      {items.map(p=><li key={p.id}><button onClick={()=>{setOpen(p.id);setTab("overview");}} className="flex h-full w-full flex-col gap-4 rounded-xl border border-border bg-surface p-5 text-left">
        <div className="flex items-center gap-3"><PluginMark category={category(p.manifest)} size="lg"/><div><h2 className="font-semibold text-fg">{p.name}</h2><span className="text-xs text-fg-muted">{p.version}</span></div><Badge variant="secondary">{t("Imported")}</Badge></div>
        <p className="text-sm text-fg-muted">{p.manifest.description}</p>
        <div className="flex flex-wrap gap-2">{resourceNames(p.manifest).map(r=><Badge key={r} variant="secondary">{r}</Badge>)}</div>
        <p className="mt-auto text-xs text-fg-muted">{p.manifest.catalogs?.length?t("{count} declared catalogues",{count:p.manifest.catalogs.length}):t("No catalogues declared")}</p>
      </button></li>)}
    </ul>
    {selected?<Drawer open onClose={()=>setOpen(null)} title={selected.name} size="lg" headerActions={<ActionMenu label={t("More actions")} items={[{key:"remove",label:t("Uninstall"),danger:true,onSelect:()=>{void apiFetch(`/plugins/${encodeURIComponent(selected.id)}`,{method:"DELETE"}).then(()=>{setOpen(null);return load();}).catch(e=>setError(reason(e)));}}]}/>}
      subtabs={<SubtabNav ariaLabel={t("Plugin sections")} active={tab} onSelect={setTab} tabs={[{key:"overview",label:t("Overview")},{key:"permissions",label:t("Permissions")},{key:"resources",label:t("Resources")} ]}/>}>
      <div className="space-y-5">
        <div className="flex items-center gap-4 rounded-xl border border-border bg-surface p-5"><PluginMark category={category(selected.manifest)} size="lg"/><p className="flex-1 text-sm text-fg">{selected.manifest.description}</p><Switch aria-label={t("Enable add-on")} checked={selected.enabled} onChange={e=>{void request(`/plugins/${encodeURIComponent(selected.id)}/${e.currentTarget.checked?"enable":"disable"}`,{}).then(load).catch(e=>setError(reason(e)));}}/></div>
        {tab==="overview"?<><dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-3 text-sm"><dt>{t("Version")}</dt><dd>{selected.version}</dd><dt>{t("Identifier")}</dt><dd className="break-all font-mono text-xs">{selected.id}</dd><dt>{t("Imported")}</dt><dd>{new Date(selected.installedAt*1000).toLocaleString()}</dd><dt>{t("Resource calls")}</dt><dd>{selected.impact.calls}</dd></dl><p className="rounded-xl border border-border bg-bg-subtle p-4 text-sm text-fg-muted">{t("The manifest is installed. Catalogue browsing, subtitle search and playback have not been verified.")}</p></>:null}
        {tab==="permissions"?<ul className="space-y-3 text-sm text-fg">{selected.permissions.lines.map(line=><li key={line}>{line}</li>)}</ul>:null}
        {tab==="resources"?<><p className="text-sm text-fg-muted">{t("Declared by the add-on; native integration is not connected yet.")}</p><div className="flex flex-wrap gap-2">{resourceNames(selected.manifest).map(r=><Badge key={r} variant="secondary">{r}</Badge>)}</div><ul className="divide-y divide-border">{selected.manifest.catalogs?.map((c,i)=><li key={i} className="flex justify-between py-3 text-sm"><span>{c.name??c.id}</span><span className="text-fg-muted">{c.type} · {c.id}</span></li>)}</ul></>:null}
      </div>
    </Drawer>:null}
    {adding?<Dialog open onClose={()=>setAdding(false)} title={t("Import Stremio add-on")} footer={<><Button variant="ghost" onClick={()=>setAdding(false)}>{t("Cancel")}</Button><Button disabled={busy||!url.trim()} onClick={()=>void (preview?install():inspect())}>{busy?t("Working…"):preview?t("Install"):t("Read manifest")}</Button></>}>
      <div className="space-y-4"><Input aria-label={t("Manifest URL")} value={url} onChange={e=>{setURL(e.target.value);setPreview(null);}} placeholder="https://example.com/manifest.json"/>
      {importError?<p role="alert" className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">{importError}</p>:null}
      {preview?<><h3 className="font-semibold">{preview.manifest.name} · {preview.manifest.version}</h3><p className="text-sm text-fg-muted">{preview.manifest.description}</p><p className="text-sm text-fg-muted">{t("Installation saves the manifest. Resource integration is not connected in this build.")}</p></>:null}</div>
    </Dialog>:null}
  </DomainPage>;
}
