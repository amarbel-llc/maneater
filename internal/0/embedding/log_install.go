package embedding

/*
#cgo pkg-config: llama

#include <llama.h>
#include <ggml.h>

extern void maneaterLlamaLogCallback(int level, const char *text, void *user_data);

static void maneater_log_trampoline(enum ggml_log_level level, const char *text, void *user_data) {
    maneaterLlamaLogCallback((int)level, text, user_data);
}

static void maneater_install_log_redirect(void) {
    llama_log_set(maneater_log_trampoline, NULL);
    ggml_log_set(maneater_log_trampoline, NULL);
}
*/
import "C"

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	logOnce   sync.Once
	logMu     sync.Mutex
	logWriter io.Writer = io.Discard
)

func installLlamaLogRedirect() {
	logOnce.Do(func() {
		defer C.maneater_install_log_redirect()

		path, err := llamaLogPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: cannot resolve llama log path: %v\n", err)
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "maneater: cannot create %s: %v\n", filepath.Dir(path), err)
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "maneater: cannot open %s: %v\n", path, err)
			return
		}
		logMu.Lock()
		logWriter = f
		logMu.Unlock()
	})
}

func writeLlamaLog(msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	_, _ = io.WriteString(logWriter, msg)
}

func llamaLogPath() (string, error) {
	if p := os.Getenv("XDG_LOG_HOME"); p != "" {
		return filepath.Join(p, "maneater", "llama.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "log", "maneater", "llama.log"), nil
}
