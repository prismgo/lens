package lens

import (
	"strings"
	"testing"
)

func TestReadPromptReturnsEmbeddedPromptAndMissingAssetError(t *testing.T) {
	prompt, ok := DefaultPromptRegistry().Lookup("prismgo-code-simplifier")
	if !ok {
		t.Fatal("default prompt should be registered")
	}
	text, err := ReadPrompt(prompt)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if !strings.Contains(text, "application-info") || !strings.Contains(text, "search-docs") {
		t.Fatalf("prompt should include required context tools: %s", text)
	}

	if _, err := ReadPrompt(Prompt{Name: "missing", AssetPath: "assets/prompts/missing.md"}); err == nil {
		t.Fatal("missing prompt asset should return an error")
	}
}
