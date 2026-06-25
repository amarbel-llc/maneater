package embedding

/*
#cgo pkg-config: llama

#include <ggml-backend.h>

// In recent llama.cpp (crossed into the tree with a nixpkgs-master bump
// of the llama-cpp package), the compute backends (CPU, Metal, …) are
// built as separate dynamic libraries (libggml-cpu-*.so,
// libggml-metal.so) rather than statically linked into libggml. They
// are no longer registered as a side effect of llama_model_load_from_file;
// the caller must load them first or the load fails with "no backends
// are loaded".
//
// ggml_backend_load_all() discovers those libraries by scanning the
// running executable's directory (and the cwd). That works when the
// backends sit beside the binary — the dev/`go build` layout — but in
// the nix build the backends live in ${llama-cpp}/bin, far from
// maneater's own bin dir, so the scan finds nothing. MANEATER_GGML_BACKEND_DIR
// is defined via CGO_CFLAGS in the nix derivation (-D...=${llama-cpp}/bin)
// to point the loader at that directory; absent the define (non-nix
// builds), we fall back to the executable-dir scan.
static void maneater_load_backends(void) {
#ifdef MANEATER_GGML_BACKEND_DIR
    ggml_backend_load_all_from_path(MANEATER_GGML_BACKEND_DIR);
#else
    ggml_backend_load_all();
#endif
}
*/
import "C"

import "sync"

var backendOnce sync.Once

// ensureBackendsLoaded registers llama.cpp's compute backends exactly
// once per process. Must run before the first llama_model_load_from_file;
// NewEmbedder calls it. Idempotent and cheap on subsequent calls (the
// sync.Once collapses to a no-op).
func ensureBackendsLoaded() {
	backendOnce.Do(func() {
		C.maneater_load_backends()
	})
}
