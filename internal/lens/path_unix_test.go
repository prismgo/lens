//go:build !windows

package lens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellPathForProfileOnlyRewritesHomeBoundary(t *testing.T) {
	tests := []struct {
		name string
		home string
		path string
		want string
	}{
		{name: "inside home", home: "/home/alice", path: "/home/alice/go/bin", want: "$HOME/go/bin"},
		{name: "sibling prefix", home: "/home/alice", path: "/home/alice2/go/bin", want: "/home/alice2/go/bin"},
		{name: "home itself", home: "/home/alice", path: "/home/alice", want: "$HOME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellPathForProfile(tt.home, tt.path); got != tt.want {
				t.Fatalf("shellPathForProfile(%q, %q) = %q, want %q", tt.home, tt.path, got, tt.want)
			}
		})
	}
}

func TestShellPathForProfileCommandQuotesSpecialPaths(t *testing.T) {
	home := "/home/alice"
	special := "/home/alice/go bin/$bad`tick\"double'quote"

	if got, want := unixPATHCommand(home, special, filepath.Join(home, ".profile")), `export PATH="$PATH:"$HOME'/go bin/$bad`+"`"+`tick"double'\''quote'`; got != want {
		t.Fatalf("posix command = %q, want %q", got, want)
	}
	if got, want := unixPATHCommand(home, special, filepath.Join(home, ".config", "fish", "config.fish")), `fish_add_path $HOME'/go bin/$bad`+"`"+`tick"double'\''quote'`; got != want {
		t.Fatalf("fish command = %q, want %q", got, want)
	}
	nonHome := "/opt/bin/$bad`tick\"double'quote"
	if got, want := unixPATHCommand(home, nonHome, filepath.Join(home, ".profile")), `export PATH="$PATH:"'/opt/bin/$bad`+"`"+`tick"double'\''quote'`; got != want {
		t.Fatalf("non-home command = %q, want %q", got, want)
	}
}

func TestWritePATHProfileBlockPreservesExistingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".profile")
	if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod profile: %v", err)
	}

	if err := writePATHProfileBlock(path, `export PATH="$PATH:"$HOME'/go/bin'`); err != nil {
		t.Fatalf("write PATH block: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat profile: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("profile mode = %v, want 0600", got)
	}
}

func TestWritePATHProfileBlockCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".profile")
	if err := writePATHProfileBlock(path, `export PATH="$PATH:"$HOME'/go/bin'`); err != nil {
		t.Fatalf("write PATH block: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat profile: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("profile mode = %v, want 0644", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if !strings.Contains(string(body), pathBlockStart) || !strings.Contains(string(body), `export PATH="$PATH:"$HOME'/go/bin'`) {
		t.Fatalf("profile missing managed PATH block:\n%s", body)
	}
}

func TestWritePATHProfileBlockReplacesDuplicateManagedBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".profile")
	existing := "before\n# >>> prismgolens PATH >>>\none\n# <<< prismgolens PATH <<<\nmiddle\n# >>> prismgolens PATH >>>\ntwo\n# <<< prismgolens PATH <<<\nafter\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	if err := writePATHProfileBlock(path, `export PATH="$PATH:"$HOME'/go/bin'`); err != nil {
		t.Fatalf("write PATH block: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	next := string(body)
	if strings.Count(next, pathBlockStart) != 1 || strings.Count(next, pathBlockEnd) != 1 {
		t.Fatalf("managed block should appear once:\n%s", next)
	}
	if strings.Contains(next, "\none\n") || strings.Contains(next, "\ntwo\n") {
		t.Fatalf("old managed block content should be removed:\n%s", next)
	}
	for _, want := range []string{"before", "middle", "after"} {
		if !strings.Contains(next, want) {
			t.Fatalf("user content %q should be preserved:\n%s", want, next)
		}
	}
}

func TestUnixProfileTargets(t *testing.T) {
	home := t.TempDir()
	if got := unixProfileTargets(home, "/usr/bin/fish"); strings.Join(got, ",") != filepath.Join(home, ".config", "fish", "config.fish") {
		t.Fatalf("fish targets = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# zsh\n"), 0o644); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}
	zsh := unixProfileTargets(home, "/bin/zsh")
	if len(zsh) != 2 || zsh[0] != filepath.Join(home, ".zprofile") || zsh[1] != filepath.Join(home, ".zshrc") {
		t.Fatalf("zsh targets = %+v", zsh)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# bash\n"), 0o644); err != nil {
		t.Fatalf("write bashrc: %v", err)
	}
	bash := unixProfileTargets(home, "/bin/bash")
	if len(bash) != 2 || bash[0] != filepath.Join(home, ".profile") || bash[1] != filepath.Join(home, ".bashrc") {
		t.Fatalf("bash targets = %+v", bash)
	}
}
