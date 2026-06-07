package commands

import "fmt"

// minCharsPerToken is the conservative chars-per-token ratio used to
// relate a corpus's char-based chunk budget (max-chars) to a model's
// token-based context window (n-ctx). English prose averages ~3-4
// chars/token with this family of tokenizers (the crash that motivated
// this measured 2.9); 2 leaves margin for denser text. Heuristic only:
// chunks that still overflow fail (or truncate) per-document inside
// Embed instead of aborting the process.
const minCharsPerToken = 2

// chunkBudgetWarning returns a warning when maxChars chunks can
// plausibly tokenize past the model's nCtx token window, or "" when
// the budget is safe. truncate selects the wording for what happens to
// oversized chunks.
func chunkBudgetWarning(corpusName string, maxChars int, modelName string, nCtx int, truncate bool) string {
	if maxChars <= nCtx*minCharsPerToken {
		return ""
	}
	outcome := "fail with a per-document error"
	if truncate {
		outcome = "be truncated to the context window"
	}
	return fmt.Sprintf(
		"warning: corpus %q max-chars %d may exceed model %q context window (%d tokens ≈ %d chars); oversized chunks will %s",
		corpusName, maxChars, modelName, nCtx, nCtx*minCharsPerToken, outcome,
	)
}

// trainedContextWarning returns a warning when the configured context
// size exceeds what the model was trained with (GGUF metadata), or ""
// when it fits. trainedCtx <= 0 means the metadata is unavailable and
// no judgment is made.
func trainedContextWarning(modelName string, nCtx, trainedCtx int) string {
	if trainedCtx <= 0 || nCtx <= trainedCtx {
		return ""
	}
	return fmt.Sprintf(
		"warning: model %q n-ctx %d exceeds its trained context size %d; embedding quality degrades past the trained window",
		modelName, nCtx, trainedCtx,
	)
}
