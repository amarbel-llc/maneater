package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amarbel-llc/maneater/internal/config"
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

	listed, err := exec.Command("madder", "list").Output()
	if err != nil {
		return fmt.Errorf("could not list madder stores: %w\nIs madder installed and on PATH?", err)
	}

	for _, line := range strings.Split(string(listed), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == sc.StoreID {
			fmt.Printf("Madder store %q already exists.\n", sc.StoreID)
			return nil
		}
	}

	cmd := exec.Command("madder", "init", sc.StoreID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("madder init %s: %w", sc.StoreID, err)
	}

	fmt.Printf("Initialized madder store %q.\n", sc.StoreID)
	return nil
}
