import { ArrowRight, Repeat2, Shield } from "lucide-react";
import type { Chain, Provider } from "../../lib/api";
import { providerIcon, strategyLabel } from "./chainUtils";

type Step = { provider: string; model: string };

function RouteStep({ step, index, provider, fallback = false }: {
  step: Step;
  index?: number;
  provider?: Provider;
  fallback?: boolean;
}) {
  const icon = providerIcon(provider, step.provider);
  return (
    <div className={`flex min-w-0 items-center gap-2 rounded-lg border px-2 py-1.5 ${
      fallback
        ? "border-[color:var(--color-warning)]/30 bg-[color:var(--color-warning)]/5 text-[color:var(--color-warning)]"
        : "border-[var(--border)] bg-[var(--bg-elevated)]"
    }`}>
      {fallback ? (
        <Shield className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      ) : (
        <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--bg-subtle)] text-[10px] font-semibold text-[var(--text-muted)]">
          {index}
        </span>
      )}
      {icon && !fallback && (
        <img src={icon} alt="" className="h-4 w-4 shrink-0 rounded-sm object-contain" onError={(event) => { event.currentTarget.style.display = "none"; }} />
      )}
      <span className="min-w-0 truncate font-mono text-[11px] font-medium">{step.model}</span>
    </div>
  );
}

export function ChainRoutePreview({ chain, providers, compact = false }: {
  chain: Pick<Chain, "strategy" | "steps" | "fallback_provider" | "fallback_model">;
  providers: Provider[];
  compact?: boolean;
}) {
  const providerMap = new Map(providers.map((provider) => [provider.id, provider]));
  const visibleSteps = compact ? chain.steps.slice(0, 3) : chain.steps;
  const hiddenCount = chain.steps.length - visibleSteps.length;
  const hasFallback = Boolean(chain.fallback_provider && chain.fallback_model);
  const roundRobin = chain.strategy === "round_robin" || chain.strategy === "round-robin";

  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-1.5" aria-label={`${strategyLabel(chain.strategy)} route`}>
        {visibleSteps.map((step, index) => (
          <div key={`${step.provider}/${step.model}/${index}`} className="flex min-w-0 items-center gap-1.5">
            {index > 0 && <ArrowRight className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden="true" />}
            <RouteStep step={step} index={index + 1} provider={providerMap.get(step.provider)} />
          </div>
        ))}
        {hiddenCount > 0 && <span className="px-1 text-xs text-[var(--text-muted)]">+{hiddenCount} more</span>}
        {roundRobin && <Repeat2 className="ml-0.5 h-3.5 w-3.5 text-accent-600 dark:text-accent-300" aria-label="Round robin" />}
        {hasFallback && (
          <>
            <ArrowRight className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden="true" />
            <RouteStep step={{ provider: chain.fallback_provider!, model: chain.fallback_model! }} provider={providerMap.get(chain.fallback_provider!)} fallback />
          </>
        )}
      </div>
    </div>
  );
}
