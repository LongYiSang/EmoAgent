package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/logcenter"
)

func (a *App) ListLogSources(ctx context.Context) ([]logcenter.Source, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	if services.LogCenter == nil {
		return nil, nil
	}
	services.LogCenter.Poll(ctx)
	return services.LogCenter.Sources(), nil
}

func (a *App) ListLogEvents(ctx context.Context, query logcenter.Query) ([]logcenter.Event, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	if services.LogCenter == nil {
		return nil, nil
	}
	_ = ctx
	return services.LogCenter.Events(query), nil
}

func (a *App) SubscribeLogEvents(ctx context.Context, query logcenter.Query) (<-chan logcenter.Event, func(), error) {
	services, err := a.services()
	if err != nil {
		return nil, nil, err
	}
	if services.LogCenter == nil {
		ch := make(chan logcenter.Event)
		close(ch)
		return ch, func() {}, nil
	}
	in, cancel := services.LogCenter.Subscribe()
	out := make(chan logcenter.Event, logcenter.DefaultSubscriberBuffer)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-in:
				if !ok {
					return
				}
				if !logcenter.Match(event, query) {
					continue
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, cancel, nil
}
