package onebotv11

import (
	"context"
	"fmt"
	"sync"
)

type EchoStore struct {
	mu      sync.Mutex
	pending map[string]chan echoResult
}

type echoResult struct {
	response ActionResponse
	err      error
}

func newEchoStore() *EchoStore {
	return &EchoStore{pending: map[string]chan echoResult{}}
}

func (s *EchoStore) register(echo string) func(context.Context) (ActionResponse, error) {
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]chan echoResult{}
	}
	ch := make(chan echoResult, 1)
	s.pending[echo] = ch
	s.mu.Unlock()
	return func(ctx context.Context) (ActionResponse, error) {
		select {
		case result := <-ch:
			return result.response, result.err
		case <-ctx.Done():
			s.remove(echo)
			return ActionResponse{}, ctx.Err()
		}
	}
}

func (s *EchoStore) resolve(resp ActionResponse) bool {
	echo := echoString(resp.Echo)
	if echo == "" {
		return false
	}
	s.mu.Lock()
	ch, ok := s.pending[echo]
	if ok {
		delete(s.pending, echo)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	ch <- echoResult{response: resp}
	return true
}

func (s *EchoStore) failAll(err error) {
	s.mu.Lock()
	pending := s.pending
	s.pending = map[string]chan echoResult{}
	s.mu.Unlock()
	for _, ch := range pending {
		ch <- echoResult{err: err}
	}
}

func (s *EchoStore) remove(echo string) {
	s.mu.Lock()
	delete(s.pending, echo)
	s.mu.Unlock()
}

func echoString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case jsonNumber:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

type jsonNumber interface {
	String() string
}
