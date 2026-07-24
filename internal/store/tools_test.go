package store

import (
	"errors"
	"strings"
	"testing"
)

func TestToolCRUD(t *testing.T) {
	s := openTestStore(t)
	tool, err := s.CreateTool("claude", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if tool.ID == 0 || tool.Name != "claude" {
		t.Fatalf("bad tool: %+v", tool)
	}
	tool.Command = "claude --continue"
	if err := s.UpdateTool(tool); err != nil {
		t.Fatal(err)
	}
	tools, err := s.ListTools()
	if err != nil || len(tools) != 1 || tools[0].Command != "claude --continue" {
		t.Fatalf("list = %+v, %v", tools, err)
	}
	if err := s.DeleteTool(tool.ID); err != nil {
		t.Fatal(err)
	}
	if tools, _ = s.ListTools(); len(tools) != 0 {
		t.Fatalf("want empty after delete, got %+v", tools)
	}
}

func TestDirCRUD(t *testing.T) {
	s := openTestStore(t)
	d, err := s.CreateDir("repos", "/Users/jon/Repos")
	if err != nil || d.ID == 0 {
		t.Fatalf("CreateDir: %+v, %v", d, err)
	}
	dirs, _ := s.ListDirs()
	if len(dirs) != 1 || dirs[0].Path != "/Users/jon/Repos" {
		t.Fatalf("list = %+v", dirs)
	}
	if err := s.DeleteDir(d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReorderTools(t *testing.T) {
	s := openTestStore(t)
	var ids []int64
	for _, name := range []string{"zsh", "claude", "codex"} {
		tool, err := s.CreateTool(name, name)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tool.ID)
	}
	// Creation order is the starting order.
	if got := toolNames(t, s); got != "zsh,claude,codex" {
		t.Fatalf("initial order = %s", got)
	}

	if err := s.ReorderTools([]int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(t, s); got != "codex,zsh,claude" {
		t.Fatalf("after reorder = %s, want codex,zsh,claude", got)
	}

	// A tool added later goes to the end, not back into the middle.
	if _, err := s.CreateTool("bash", "bash"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(t, s); got != "codex,zsh,claude,bash" {
		t.Fatalf("after add = %s, want bash last", got)
	}
}

func TestReorderRejectsMismatchedIDs(t *testing.T) {
	s := openTestStore(t)
	a, _ := s.CreateTool("zsh", "zsh")
	b, _ := s.CreateTool("claude", "claude")

	cases := map[string][]int64{
		"too short":  {b.ID},
		"unknown id": {a.ID, b.ID, 999},
		"duplicate":  {a.ID, a.ID},
	}
	for name, ids := range cases {
		if err := s.ReorderTools(ids); !errors.Is(err, ErrOrderMismatch) {
			t.Fatalf("%s: err = %v, want ErrOrderMismatch", name, err)
		}
	}
	// Rejected reorders leave the stored order untouched.
	if got := toolNames(t, s); got != "zsh,claude" {
		t.Fatalf("order changed after rejection: %s", got)
	}
}

func TestReorderDirs(t *testing.T) {
	s := openTestStore(t)
	a, _ := s.CreateDir("repos", "/a")
	b, _ := s.CreateDir("tmp", "/b")
	if err := s.ReorderDirs([]int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	dirs, _ := s.ListDirs()
	if len(dirs) != 2 || dirs[0].Name != "tmp" || dirs[1].Name != "repos" {
		t.Fatalf("dirs = %+v", dirs)
	}
}

func toolNames(t *testing.T, s *Store) string {
	t.Helper()
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
	}
	return strings.Join(names, ",")
}

func TestSeedDefaults(t *testing.T) {
	s := openTestStore(t)
	if err := s.SeedDefaults("darwin"); err != nil {
		t.Fatal(err)
	}
	tools, _ := s.ListTools()
	if len(tools) != 1 || tools[0].Name != "zsh" || tools[0].Command != "zsh" {
		t.Fatalf("darwin seed = %+v", tools)
	}
	// Idempotent: second seed adds nothing.
	if err := s.SeedDefaults("darwin"); err != nil {
		t.Fatal(err)
	}
	if tools, _ = s.ListTools(); len(tools) != 1 {
		t.Fatalf("seed not idempotent: %+v", tools)
	}
}

func TestSeedDefaultsLinux(t *testing.T) {
	s := openTestStore(t)
	if err := s.SeedDefaults("linux"); err != nil {
		t.Fatal(err)
	}
	tools, _ := s.ListTools()
	if len(tools) != 1 || tools[0].Name != "bash" {
		t.Fatalf("linux seed = %+v", tools)
	}
}
