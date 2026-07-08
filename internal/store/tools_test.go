package store

import "testing"

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
