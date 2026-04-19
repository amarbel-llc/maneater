package manpath_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/maneater/internal/0/manpath"
)

func TestResolveHeuristicProbesExistingDirs(t *testing.T) {
	cwd := t.TempDir()

	// Create only man/ and share/man/ — not doc/man/.
	for _, rel := range []string{"man", "share/man"} {
		if err := os.MkdirAll(filepath.Join(cwd, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := manpath.Resolve(nil, false, cwd)
	if err != nil {
		t.Fatal(err)
	}

	wantMan := filepath.Join(cwd, "man")
	wantShare := filepath.Join(cwd, "share/man")
	noWantDoc := filepath.Join(cwd, "doc/man")

	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}

	if !found[wantMan] {
		t.Errorf("expected %s in manpath", wantMan)
	}
	if !found[wantShare] {
		t.Errorf("expected %s in manpath", wantShare)
	}
	if found[noWantDoc] {
		t.Errorf("doc/man should not be in manpath (directory does not exist)")
	}
}

func TestResolveNoAutoDisablesHeuristics(t *testing.T) {
	cwd := t.TempDir()

	if err := os.MkdirAll(filepath.Join(cwd, "man"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := manpath.Resolve(nil, true, cwd)
	if err != nil {
		t.Fatal(err)
	}

	heuristic := filepath.Join(cwd, "man")
	for _, p := range paths {
		if p == heuristic {
			t.Errorf("heuristic path %s should not appear when no-auto=true", heuristic)
		}
	}
}

func TestResolveIncludePathsPrepended(t *testing.T) {
	cwd := t.TempDir()

	paths, err := manpath.Resolve([]string{"/custom/man"}, true, cwd)
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected at least one path")
	}
	if paths[0] != "/custom/man" {
		t.Errorf("first path = %q, want /custom/man", paths[0])
	}
}

func TestResolveRelativeIncludeResolvedFromCwd(t *testing.T) {
	cwd := t.TempDir()

	paths, err := manpath.Resolve([]string{"vendor/man"}, true, cwd)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(cwd, "vendor/man")
	if len(paths) == 0 || paths[0] != want {
		t.Errorf("first path = %q, want %q", paths[0], want)
	}
}

func TestResolveHeuristicsBeforeIncludeBeforeSystem(t *testing.T) {
	cwd := t.TempDir()

	// Create man/ so heuristic fires.
	if err := os.MkdirAll(filepath.Join(cwd, "man"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := manpath.Resolve([]string{"/custom/man"}, false, cwd)
	if err != nil {
		t.Fatal(err)
	}

	heuristic := filepath.Join(cwd, "man")
	custom := "/custom/man"

	var heuristicIdx, customIdx int
	for i, p := range paths {
		switch p {
		case heuristic:
			heuristicIdx = i
		case custom:
			customIdx = i
		}
	}

	if heuristicIdx >= customIdx {
		t.Errorf("heuristic (%d) should come before include (%d)", heuristicIdx, customIdx)
	}
}
