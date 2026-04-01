// worker パッケージ — バックグラウンドで Cognito import ジョブのポーリングを行う。
//
// CognitoImportQueue テーブルを定期的にチェックし、
// Cognito 側のインポートジョブの完了を検知してローカル DB を更新する。
package worker

import (
	"context"
	"time"
)

// PendingImportProcessor は未完了の Cognito import ジョブを処理するインターフェース。
type PendingImportProcessor interface {
	ProcessPendingImports()
}

// Worker はバックグラウンドでポーリングを行うワーカー。
type Worker struct {
	processor PendingImportProcessor
	interval  time.Duration
}

// New は新しい Worker を作成する。
func New(processor PendingImportProcessor, interval time.Duration) *Worker {
	return &Worker{
		processor: processor,
		interval:  interval,
	}
}

// Start はバックグラウンドの goroutine でポーリングループを開始する。
// ctx がキャンセルされるとループを終了する。
func (w *Worker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.processor.ProcessPendingImports()
			}
		}
	}()
}
