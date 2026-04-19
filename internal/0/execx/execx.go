// Package execx contains tiny helpers shared by packages that shell out
// to external commands (internal/0/madder, internal/0/storage,
// internal/alfa/corpus). It intentionally does not wrap exec.Command
// itself — callers keep their own exec + buffer + error-wrap
// conventions.
package execx

// AppendArg returns base + [arg]. It always allocates a new slice so
// concurrent callers never alias a shared backing array, which matters
// when the same base command array is handed to a worker pool (e.g.
// corpus.CommandCorpus).
func AppendArg(base []string, arg string) []string {
	out := make([]string, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, arg)
	return out
}
