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
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	logOnce   sync.Once
	logMu     sync.Mutex
	logWriter io.Writer = io.Discard

	// logCapture, when non-nil, accumulates llama.cpp log output
	// alongside the file log so a failed model load can fold the
	// underlying reason into its error. Guarded by logMu (the log
	// callback already holds it). nil when no capture is active.
	logCapture *strings.Builder
)

func installLlamaLogRedirect() {
	logOnce.Do(func() {
		defer C.maneater_install_log_redirect()

		// The llama.cpp log is best-effort diagnostics. When its
		// location is unresolvable or unwritable — e.g. a read-only
		// $HOME like the nix build sandbox's /homeless-shelter — fall
		// back silently to io.Discard rather than printing to stderr,
		// so a non-essential log can't pollute command output.
		path, err := llamaLogPath()
		if err != nil {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
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
	if logCapture != nil {
		logCapture.WriteString(msg)
	}
}

// startLlamaLogCapture begins accumulating llama.cpp log output in
// memory (in addition to the file log). It is not reentrant: a single
// capture is active at a time, serialized by the caller holding the
// load through stopLlamaLogCapture. Returns false if a capture is
// already active, in which case the caller must not capture.
func startLlamaLogCapture() bool {
	logMu.Lock()
	defer logMu.Unlock()
	if logCapture != nil {
		return false
	}
	logCapture = &strings.Builder{}
	return true
}

// stopLlamaLogCapture ends the active capture and returns the
// accumulated log output. Safe to call after a failed
// startLlamaLogCapture (returns "").
func stopLlamaLogCapture() string {
	logMu.Lock()
	defer logMu.Unlock()
	if logCapture == nil {
		return ""
	}
	captured := logCapture.String()
	logCapture = nil
	return captured
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
