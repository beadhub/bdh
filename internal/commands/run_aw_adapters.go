package commands

import (
	"context"

	awrun "github.com/awebai/aw/run"
)

type awRunDispatcherAdapter struct {
	inner runDispatcher
}

func (a *awRunDispatcherAdapter) Next(ctx context.Context, autofeed bool) (awrun.DispatchDecision, error) {
	decision, err := a.inner.Next(ctx, autofeed)
	if err != nil {
		return awrun.DispatchDecision{}, err
	}
	return awrun.DispatchDecision{
		Prompt:      decision.Prompt,
		WaitSeconds: decision.WaitSeconds,
		Skip:        decision.Skip,
	}, nil
}
