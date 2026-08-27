package app

import (
	"context"
	"testing"

	"invoice-backend/internal/config"
)

type recordingRenderProfilePasswordBackfiller struct {
	calls int
	count int
	err   error
}

func (b *recordingRenderProfilePasswordBackfiller) BackfillLegacyRenderProfilePasswords(context.Context) (int, error) {
	b.calls++
	return b.count, b.err
}

func TestOnlyHTTPRuntimeBackfillsLegacyRenderProfilePasswords(t *testing.T) {
	ctx := context.Background()
	backfiller := &recordingRenderProfilePasswordBackfiller{count: 3}

	count, err := backfillLegacyRenderProfilePasswords(ctx, config.ProfileHTTP, backfiller)
	if err != nil {
		t.Fatalf("backfill HTTP render profile passwords: %v", err)
	}
	if count != 3 || backfiller.calls != 1 {
		t.Fatalf("HTTP backfill count/calls = %d/%d, want 3/1", count, backfiller.calls)
	}

	for _, profile := range []config.Profile{config.ProfileA2A, config.ProfileInvoice, config.ProfileGST} {
		count, err := backfillLegacyRenderProfilePasswords(ctx, profile, backfiller)
		if err != nil {
			t.Fatalf("non-HTTP profile %q backfill: %v", profile, err)
		}
		if count != 0 {
			t.Fatalf("non-HTTP profile %q backfill count = %d, want 0", profile, count)
		}
	}
	if backfiller.calls != 1 {
		t.Fatalf("backfill calls after worker profiles = %d, want 1", backfiller.calls)
	}
}
