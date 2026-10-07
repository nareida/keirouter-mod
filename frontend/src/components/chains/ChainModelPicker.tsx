import { useMemo, useState } from "react";
import { Check, ChevronDown, Plus, Search, X } from "lucide-react";
import type { Provider } from "../../lib/api";
import { ModelCapabilityIcons } from "../ModelCapabilityIcons";
import { useModelCatalog } from "../ModelSelect";
import type { DraftChainStep } from "./chainUtils";
import { isLLMProvider, providerIcon } from "./chainUtils";

export function ChainModelPicker({ value, providers, onChange, autoFocus = false }: {
  value: DraftChainStep;
  providers: Provider[];
  onChange: (next: Pick<DraftChainStep, "provider" | "model">) => void;
  autoFocus?: boolean;
}) {
  const catalog = useModelCatalog();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [custom, setCustom] = useState(false);
  const availableProviderIDs = useMemo(() => new Set(providers.filter(isLLMProvider).map((provider) => provider.id)), [providers]);
  const selected = catalog.models.find((model) => model.providerId === value.provider && model.id === value.model);
  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase();
    return catalog.models.filter((model) => availableProviderIDs.has(model.providerId) && (!term || `${model.name} ${model.id} ${model.providerName} ${model.providerId}`.toLowerCase().includes(term))).slice(0, 60);
  }, [availableProviderIDs, catalog.models, query]);
  const select = (provider: string, model: string) => {
    onChange({ provider, model });
    setOpen(false);
    setQuery("");
    setCustom(false);
  };

  return <div className="relative min-w-0">
    <button type="button" autoFocus={autoFocus} onClick={() => setOpen((current) => !current)} aria-expanded={open} aria-haspopup="listbox" className="flex min-h-10 w-full items-center gap-2 rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] px-3 text-left text-sm transition-colors hover:border-[var(--border-strong)] focus:border-accent-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-400/30">
      {selected?.icon || value.provider ? <img src={selected?.icon || providerIcon(providers.find((provider) => provider.id === value.provider), value.provider)} alt="" className="h-5 w-5 shrink-0 rounded-sm object-contain" onError={(event) => { event.currentTarget.style.display = "none"; }} /> : null}
      <span className={`min-w-0 flex-1 truncate ${value.model ? "text-[var(--text)]" : "text-[var(--text-muted)]"}`}>{value.model ? selected?.name || value.model : "Choose a model…"}</span>
      <ChevronDown className={`h-4 w-4 shrink-0 text-[var(--text-muted)] transition-transform ${open ? "rotate-180" : ""}`} />
    </button>
    {open && <div className="absolute z-30 mt-1 w-full min-w-[320px] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] shadow-[var(--shadow-float)]">
      <div className="border-b border-[var(--border)] p-2"><div className="flex items-center gap-2 rounded-lg bg-[var(--bg-subtle)] px-2.5 py-2"><Search className="h-4 w-4 shrink-0 text-[var(--text-muted)]" /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search models and providers…" className="w-full bg-transparent text-sm outline-none placeholder:text-[var(--text-muted)]" autoFocus /></div></div>
      {custom ? <div className="space-y-3 p-3"><p className="text-xs text-[var(--text-muted)]">Use an explicit model ID when it is not in the catalog.</p><select value={value.provider} onChange={(event) => onChange({ provider: event.target.value, model: "" })} className="min-h-10 w-full rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] px-3 text-sm"><option value="">Choose provider…</option>{providers.filter(isLLMProvider).map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name}</option>)}</select><input value={value.model} onChange={(event) => onChange({ provider: value.provider, model: event.target.value })} placeholder="provider model ID" className="min-h-10 w-full rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] px-3 font-mono text-sm" /><button type="button" onClick={() => setCustom(false)} className="text-xs font-medium text-accent-700 hover:text-accent-800 dark:text-accent-300">Back to catalog</button></div> : <><div role="listbox" className="max-h-64 overflow-y-auto p-1.5">{catalog.loading ? <p className="px-3 py-4 text-center text-xs text-[var(--text-muted)]">Loading available models…</p> : filtered.length === 0 ? <p className="px-3 py-4 text-center text-xs text-[var(--text-muted)]">No catalog models match this search.</p> : filtered.map((model) => <button key={`${model.providerId}/${model.id}`} type="button" role="option" aria-selected={model.providerId === value.provider && model.id === value.model} onClick={() => select(model.providerId, model.id)} className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-[var(--bg-subtle)]"><img src={model.icon} alt="" className="h-5 w-5 shrink-0 rounded-sm object-contain" onError={(event) => { event.currentTarget.style.display = "none"; }} /><span className="min-w-0 flex-1"><span className="flex items-center gap-1"><span className="truncate text-sm font-medium">{model.name}</span><ModelCapabilityIcons capabilities={model.capabilities} size={13} /></span><span className="block truncate text-[11px] text-[var(--text-muted)]">{model.providerName} · {model.id}</span></span>{model.providerId === value.provider && model.id === value.model && <Check className="h-4 w-4 shrink-0 text-accent-600" />}</button>)}</div><div className="border-t border-[var(--border)] p-1.5"><button type="button" onClick={() => setCustom(true)} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs font-medium text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-subtle)] hover:text-[var(--text)]"><Plus className="h-3.5 w-3.5" />Use a custom model ID</button></div></>}
      <button type="button" onClick={() => setOpen(false)} className="absolute right-2 top-2 rounded p-1 text-[var(--text-muted)] hover:bg-[var(--bg-elevated)]" aria-label="Close model picker"><X className="h-3.5 w-3.5" /></button>
    </div>}
  </div>;
}
