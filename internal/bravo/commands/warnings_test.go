package commands

import (
	"strings"
	"testing"
)

func TestChunkBudgetWarning(t *testing.T) {
	// The config that motivated this: max-chars 2000 against snowflake's
	// default 512-token window (a 2000-char chunk measured 688 tokens).
	msg := chunkBudgetWarning("newsblur-starred", 2000, "snowflake", 512, false)
	if msg == "" {
		t.Fatal("max-chars 2000 vs n-ctx 512 should warn")
	}
	if !strings.Contains(msg, "newsblur-starred") || !strings.Contains(msg, "snowflake") {
		t.Errorf("warning should name corpus and model: %q", msg)
	}
	if !strings.Contains(msg, "per-document error") {
		t.Errorf("truncate=false wording should mention per-document error: %q", msg)
	}

	if msg := chunkBudgetWarning("c", 2000, "m", 512, true); !strings.Contains(msg, "truncated") {
		t.Errorf("truncate=true wording should mention truncation: %q", msg)
	}

	// The default budget against the default window is safe.
	if msg := chunkBudgetWarning("manpages", 500, "snowflake", 512, false); msg != "" {
		t.Errorf("max-chars 500 vs n-ctx 512 should not warn, got %q", msg)
	}

	// Exactly at the ratio boundary is safe; one past it warns.
	if msg := chunkBudgetWarning("c", 512*minCharsPerToken, "m", 512, false); msg != "" {
		t.Errorf("max-chars at boundary should not warn, got %q", msg)
	}
	if msg := chunkBudgetWarning("c", 512*minCharsPerToken+1, "m", 512, false); msg == "" {
		t.Error("max-chars one past boundary should warn")
	}
}

func TestTrainedContextWarning(t *testing.T) {
	if msg := trainedContextWarning("snowflake", 512, 8192); msg != "" {
		t.Errorf("n-ctx within trained window should not warn, got %q", msg)
	}
	if msg := trainedContextWarning("snowflake", 16384, 8192); msg == "" {
		t.Error("n-ctx past trained window should warn")
	}
	// Metadata unavailable: no judgment.
	if msg := trainedContextWarning("m", 4096, 0); msg != "" {
		t.Errorf("trainedCtx 0 should not warn, got %q", msg)
	}
}
