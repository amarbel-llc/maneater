package corpus

import (
	"fmt"

	"github.com/amarbel-llc/maneater/internal/0/config"
)

// FromConfig builds a Corpus from a CorpusConfig for the "files" and
// "command" types. The "manpages" type is handled separately — its
// dependencies (manpath discovery, mandoc/pandoc) don't belong here.
func FromConfig(cc config.CorpusConfig) (Corpus, error) {
	switch cc.Type {
	case "files":
		if cc.Name == "" {
			return nil, fmt.Errorf("corpus of type %q requires a name", cc.Type)
		}
		return &FilesCorpus{
			CorpusName: cc.Name,
			Patterns:   cc.Paths,
			MaxChars:   cc.MaxChars,
		}, nil
	case "command":
		if cc.Name == "" {
			return nil, fmt.Errorf("corpus of type %q requires a name", cc.Type)
		}
		if len(cc.ListCmd) == 0 {
			return nil, fmt.Errorf("corpus %q: command type requires list-cmd", cc.Name)
		}
		if len(cc.ReadCmd) == 0 {
			return nil, fmt.Errorf("corpus %q: command type requires read-cmd", cc.Name)
		}
		return &CommandCorpus{
			CorpusName: cc.Name,
			ListCmd:    cc.ListCmd,
			ReadCmd:    cc.ReadCmd,
			MaxChars:   cc.MaxChars,
		}, nil
	default:
		return nil, fmt.Errorf("unknown corpus type %q", cc.Type)
	}
}
