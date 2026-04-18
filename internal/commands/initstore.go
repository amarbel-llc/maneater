package commands

import (
	"fmt"

	"github.com/amarbel-llc/maneater/internal/config"
	"github.com/amarbel-llc/maneater/internal/madder"
)

// RunInitStore initializes the madder store whose ID is in cfg.Storage.StoreID
// (or "maneater" by default). Safe to call repeatedly — a no-op if the store
// already exists.
func RunInitStore() error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	sc := config.ResolveStorage(cfg)
	store := &madder.Store{StoreID: sc.StoreID}

	exists, err := store.Exists()
	if err != nil {
		return err
	}
	if exists {
		fmt.Printf("Madder store %q already exists.\n", store.StoreID)
		return nil
	}

	if err := store.Init(); err != nil {
		return err
	}
	fmt.Printf("Initialized madder store %q.\n", store.StoreID)
	return nil
}
