package commands

import (
	"context"
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/storage"
)

// RunInitStore initializes the configured blob store (madder by default;
// any backend configured via [storage] read/write/exists/init-cmd).
// Safe to call repeatedly — a no-op if the store already exists.
// Emits TAP-14 output.
func RunInitStore(ctx context.Context) error {
	tw := tap.NewWriter(os.Stdout)

	cfg, err := config.LoadDefault()
	if err != nil {
		tw.BailOut(fmt.Sprintf("loading config: %v", err))
		return fmt.Errorf("loading config: %w", err)
	}

	sc := config.ResolveStorage(cfg)
	store := storage.FromConfig(sc)

	exists, err := store.Exists(ctx)
	if err != nil {
		tw.BailOut(err.Error())
		return err
	}
	if exists {
		tw.Skip(fmt.Sprintf("blob store %q", sc.StoreID), "already exists")
		tw.Plan()
		return nil
	}

	if err := store.Init(ctx); err != nil {
		tw.BailOut(err.Error())
		return err
	}
	tw.Ok(fmt.Sprintf("initialized blob store %q", sc.StoreID))
	tw.Plan()
	return nil
}
