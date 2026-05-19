package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const aoneSyncInterval = 10 * time.Minute

func runAoneSyncScheduler(ctx context.Context, queries *db.Queries, txStarter service.TxStarter) {
	svc := service.NewAoneSyncService(queries, txStarter)

	ticker := time.NewTicker(aoneSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Debug("aone sync: tick")
			svc.SyncAll(ctx)
		}
	}
}
