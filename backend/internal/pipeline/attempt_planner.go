package pipeline

import (
	"context"

	"github.com/mydisha/keirouter/backend/internal/core"
	"github.com/mydisha/keirouter/backend/internal/dispatch"
)

// attemptPlanner refreshes routing state after each failed credential while
// preserving the target order chosen for the request's first plan.
type attemptPlanner struct {
	dispatcher *dispatch.Dispatcher
	tenantID   string
	targets    []dispatch.Target
	required   core.CapabilitySet
	options    dispatch.PlanOptions
	affinity   dispatch.PlanOptions
	excluded   map[string]struct{}
	attempted  map[dispatch.AttemptKey]struct{}
	current    dispatch.Attempt
	remaining  bool
}

func newAttemptPlanner(
	dispatcher *dispatch.Dispatcher,
	tenantID string,
	originalTargets []dispatch.Target,
	required core.CapabilitySet,
	options dispatch.PlanOptions,
	initial []dispatch.Attempt,
) *attemptPlanner {
	excluded := make(map[string]struct{}, len(options.ExcludedAccountIDs))
	for id := range options.ExcludedAccountIDs {
		excluded[id] = struct{}{}
	}

	retryOptions := options
	retryOptions.Strategy = dispatch.StrategyFallback
	retryOptions.ChainID = ""
	retryOptions.AccountStrategy = dispatch.StrategyFallback
	retryOptions.AccountAffinityKey = ""
	retryOptions.ExcludedAccountIDs = excluded
	attempted := make(map[dispatch.AttemptKey]struct{}, len(options.ExcludedAttempts))
	for key := range options.ExcludedAttempts {
		attempted[key] = struct{}{}
	}
	retryOptions.ExcludedAttempts = attempted
	if len(options.ProviderAccountStrategies) > 0 {
		retryOptions.ProviderAccountStrategies = make(map[string]dispatch.AccountRoutingOptions, len(options.ProviderAccountStrategies))
		for provider, providerOptions := range options.ProviderAccountStrategies {
			providerOptions.Strategy = dispatch.StrategyFallback
			providerOptions.AffinityKey = ""
			retryOptions.ProviderAccountStrategies[provider] = providerOptions
		}
	}

	planner := &attemptPlanner{
		dispatcher: dispatcher,
		tenantID:   tenantID,
		targets:    stableTargetOrder(initial, originalTargets),
		required:   required,
		options:    retryOptions,
		affinity:   options,
		excluded:   excluded,
		attempted:  attempted,
	}
	if len(initial) > 0 {
		planner.current = initial[0]
		planner.remaining = true
	}
	return planner
}

func (p *attemptPlanner) Current() (dispatch.Attempt, bool) {
	return p.current, p.remaining
}

func (p *attemptPlanner) AfterFailure(ctx context.Context, failed dispatch.Attempt, pe *core.ProviderError) (dispatch.Attempt, bool) {
	if !p.remaining || failed.Account.ID == "" {
		p.remaining = false
		return dispatch.Attempt{}, false
	}
	p.attempted[failed.Key()] = struct{}{}
	p.options.ExcludedAttempts = p.attempted
	if pe != nil && pe.EffectiveScope() == core.FailureScopeAccount {
		p.excluded[failed.Account.ID] = struct{}{}
	}
	p.options.ExcludedAccountIDs = p.excluded
	p.dispatcher.EvictAccountAffinity(ctx, p.tenantID, failed.Target, p.affinity, failed.Account.ID)

	attempts, err := p.dispatcher.PlanWith(ctx, p.tenantID, p.targets, p.required, p.options)
	if err != nil || len(attempts) == 0 {
		p.remaining = false
		return dispatch.Attempt{}, false
	}
	p.current = attempts[0]
	return p.current, true
}

func (p *attemptPlanner) AfterRepair(ctx context.Context, failed dispatch.Attempt, pe *core.ProviderError) (dispatch.Attempt, bool) {
	if next, ok := p.AfterFailure(ctx, failed, pe); ok {
		return next, true
	}
	p.current = failed
	p.remaining = true
	return failed, true
}

func stableTargetOrder(initial []dispatch.Attempt, original []dispatch.Target) []dispatch.Target {
	out := make([]dispatch.Target, 0, len(original))
	seen := make(map[dispatch.Target]struct{}, len(original))
	for _, attempt := range initial {
		if _, ok := seen[attempt.Target]; ok {
			continue
		}
		seen[attempt.Target] = struct{}{}
		out = append(out, attempt.Target)
	}
	for _, target := range original {
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}
