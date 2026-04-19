package commands

import (
	"context"
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/maneater/internal/0/config"
	"github.com/amarbel-llc/maneater/internal/0/madder"
)

// RunInitStore initializes the madder store whose ID is in cfg.Storage.StoreID
// (or "maneater" by default). Safe to call repeatedly — a no-op if the store
// already exists. Emits TAP-14 output.
func RunInitStore(ctx context.Context) error {
	tw := tap.NewWriter(os.Stdout)

	cfg, err := config.LoadDefault()
	if err != nil {
		tw.BailOut(fmt.Sprintf("loading config: %v", err))
		return fmt.Errorf("loading config: %w", err)
	}

	sc := config.ResolveStorage(cfg)
	store := &madder.Store{StoreID: sc.StoreID}

	exists, err := store.Exists(ctx)
	if err != nil {
		tw.BailOut(err.Error())
		return err
	}
	if exists {
		tw.Skip(fmt.Sprintf("madder store %q", store.StoreID), "already exists")
		tw.Plan()
		return nil
	}

	if err := store.Init(ctx); err != nil {
		tw.BailOut(err.Error())
		return err
	}
	tw.Ok(fmt.Sprintf("initialized madder store %q", store.StoreID))
	tw.Plan()
	return nil
}
