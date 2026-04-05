package worker

import (
	"context"
	"testing"
	"time"
)

func TestWorkerPassesContextToProcessor(t *testing.T) {
	processor := &stubPendingImportProcessor{called: make(chan struct{}, 1)}
	w := New(processor, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Start(ctx)

	select {
	case <-processor.called:
		if processor.ctx == nil {
			t.Fatal("processor ctx is nil")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("worker did not invoke processor")
	}
}

type stubPendingImportProcessor struct {
	ctx    context.Context
	called chan struct{}
}

func (s *stubPendingImportProcessor) ProcessPendingImports(ctx context.Context) {
	s.ctx = ctx
	select {
	case s.called <- struct{}{}:
	default:
	}
}
