package store

import (
	"errors"
	"fmt"
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
	if err := s.SeedDefaults("darwin", "/Users/someone"); err != nil {
		t.Fatal(err)
	}
	tools, _ := s.ListTools()
	if len(tools) != 1 || tools[0].Name != "zsh" || tools[0].Command != "zsh" {
		t.Fatalf("darwin seed = %+v", tools)
	}
	dirs, _ := s.ListDirs()
	if len(dirs) != 1 || dirs[0].Name != "~" || dirs[0].Path != "/Users/someone" {
		t.Fatalf("dir seed = %+v", dirs)
	}
	// Idempotent: second seed adds nothing.
	if err := s.SeedDefaults("darwin", "/Users/someone"); err != nil {
		t.Fatal(err)
	}
	if tools, _ = s.ListTools(); len(tools) != 1 {
		t.Fatalf("seed not idempotent: %+v", tools)
	}
	if dirs, _ = s.ListDirs(); len(dirs) != 1 {
		t.Fatalf("dir seed not idempotent: %+v", dirs)
	}
}

func TestSeedDefaultsLinux(t *testing.T) {
	s := openTestStore(t)
	if err := s.SeedDefaults("linux", "/home/someone"); err != nil {
		t.Fatal(err)
	}
	tools, _ := s.ListTools()
	if len(tools) != 1 || tools[0].Name != "bash" {
		t.Fatalf("linux seed = %+v", tools)
	}
}

// Each table is checked on its own, so an empty dirs table still gets seeded
// while tools are populated, and vice versa.
func TestSeedDefaultsSeedsTablesIndependently(t *testing.T) {
	s := openTestStore(t)
	if err := s.SeedDefaults("linux", "/home/someone"); err != nil {
		t.Fatal(err)
	}
	dirs, _ := s.ListDirs()
	if err := s.DeleteDir(dirs[0].ID); err != nil {
		t.Fatal(err)
	}
	// Tools are still populated, so an all-or-nothing seed would skip the dir.
	if err := s.SeedDefaults("linux", "/home/someone"); err != nil {
		t.Fatal(err)
	}
	if dirs, _ = s.ListDirs(); len(dirs) != 1 {
		t.Fatalf("dirs after reseed = %+v", dirs)
	}
	tools, _ := s.ListTools()
	if err := s.DeleteTool(tools[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedDefaults("linux", "/home/someone"); err != nil {
		t.Fatal(err)
	}
	if tools, _ = s.ListTools(); len(tools) != 1 {
		t.Fatalf("tools after reseed = %+v", tools)
	}
}

// No home directory available is not an error; the user can add one by hand.
func TestSeedDefaultsWithoutHome(t *testing.T) {
	s := openTestStore(t)
	if err := s.SeedDefaults("linux", ""); err != nil {
		t.Fatal(err)
	}
	if dirs, _ := s.ListDirs(); len(dirs) != 0 {
		t.Fatalf("dirs = %+v", dirs)
	}
}

func TestSubdirHistoryRecordsMostRecentFirst(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")

	for _, sub := range []string{"web", "cmd", "internal/server"} {
		if err := s.RecordSubdir(d.ID, sub); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListSubdirs(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "internal/server,cmd,web" {
		t.Fatalf("history = %v, want newest first", got)
	}

	// Re-using an entry bumps it to the front instead of duplicating it.
	if err := s.RecordSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ListSubdirs(d.ID); strings.Join(got, ",") != "web,internal/server,cmd" {
		t.Fatalf("history after re-use = %v", got)
	}
}

func TestSubdirHistoryIgnoresBlank(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	if err := s.RecordSubdir(d.ID, "   "); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSubdirs(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("history = %v, want empty", got)
	}
	// A directory with no history must still list as an empty slice, not nil:
	// the handler writes it straight to JSON, and nil marshals as null.
	if got == nil {
		t.Fatal("ListSubdirs returned nil, want empty slice")
	}
}

func TestSubdirHistoryIsCappedAtTen(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	for i := 0; i < 11; i++ {
		if err := s.RecordSubdir(d.ID, fmt.Sprintf("dir%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.ListSubdirs(d.ID)
	if len(got) != 10 {
		t.Fatalf("history length = %d, want 10", len(got))
	}
	if got[0] != "dir10" || got[9] != "dir01" {
		t.Fatalf("history = %v, want dir10..dir01 (dir00 evicted)", got)
	}
}

func TestSubdirHistoryIsPerDirectory(t *testing.T) {
	s := openTestStore(t)
	a, _ := s.CreateDir("repos", "/repos")
	b, _ := s.CreateDir("home", "/home")
	if err := s.RecordSubdir(a.ID, "web/src"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(b.ID); len(got) != 0 {
		t.Fatalf("other directory's history = %v, want empty", got)
	}
}

func TestDeleteSubdir(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	_ = s.RecordSubdir(d.ID, "web")
	_ = s.RecordSubdir(d.ID, "cmd")

	if err := s.DeleteSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(d.ID); strings.Join(got, ",") != "cmd" {
		t.Fatalf("history after delete = %v", got)
	}
	// Deleting something that is already gone is the client repeating itself,
	// not an error.
	if err := s.DeleteSubdir(d.ID, "web"); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
}

// The history hangs off the directory row; removing the directory must not
// leave rows nothing can reach.
func TestSubdirHistoryDiesWithItsDirectory(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	if err := s.RecordSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDir(d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(d.ID); len(got) != 0 {
		t.Fatalf("history survived its directory: %v", got)
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain command is one segment", "zsh", []string{"zsh"}},
		{"separator splits", "zsh ;; claude", []string{"zsh", "claude"}},
		{"separator needs no spaces", "zsh;;claude", []string{"zsh", "claude"}},
		{"three segments", "zsh ;; claude ;; codex --model gpt", []string{"zsh", "claude", "codex --model gpt"}},
		{"escaped separator stays literal", `echo a\;;b`, []string{"echo a;;b"}},
		{"escape only applies to the separator", `echo a\b`, []string{`echo a\b`}},
		{"blank segments are dropped", "zsh ;;  ;; claude", []string{"zsh", "claude"}},
		// An empty command field is not a group; it stays one segment so a
		// launch behaves exactly as it did before groups existed.
		{"empty command is one empty segment", "", []string{""}},
		{"whitespace-only command is one empty segment", "   ", []string{""}},
		{"separators only", " ;; ;; ", []string{""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SplitCommand(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("SplitCommand(%q) = %q, want %q", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("SplitCommand(%q) = %q, want %q", c.in, got, c.want)
				}
			}
		})
	}
}

func TestCommandLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"claude", "claude"},
		{"claude --resume", "claude"},
		{"/usr/local/bin/zsh -l", "zsh"},
		{"  codex   --model gpt  ", "codex"},
		{"", ""},
	}
	for _, c := range cases {
		if got := CommandLabel(c.in); got != c.want {
			t.Fatalf("CommandLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
