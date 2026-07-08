package store

// Tool is a launchable command template (e.g. zsh, claude, codex).
type Tool struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Dir is an allowed working directory for new sessions.
type Dir struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Store) CreateTool(name, command string) (Tool, error) {
	res, err := s.db.Exec(`INSERT INTO tools (name, command) VALUES (?, ?)`, name, command)
	if err != nil {
		return Tool{}, err
	}
	id, err := res.LastInsertId()
	return Tool{ID: id, Name: name, Command: command}, err
}

func (s *Store) ListTools() ([]Tool, error) {
	rows, err := s.db.Query(`SELECT id, name, command FROM tools ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.ID, &t.Name, &t.Command); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTool(t Tool) error {
	_, err := s.db.Exec(`UPDATE tools SET name = ?, command = ? WHERE id = ?`, t.Name, t.Command, t.ID)
	return err
}

func (s *Store) DeleteTool(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tools WHERE id = ?`, id)
	return err
}

func (s *Store) CreateDir(name, path string) (Dir, error) {
	res, err := s.db.Exec(`INSERT INTO dirs (name, path) VALUES (?, ?)`, name, path)
	if err != nil {
		return Dir{}, err
	}
	id, err := res.LastInsertId()
	return Dir{ID: id, Name: name, Path: path}, err
}

func (s *Store) ListDirs() ([]Dir, error) {
	rows, err := s.db.Query(`SELECT id, name, path FROM dirs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dir
	for rows.Next() {
		var d Dir
		if err := rows.Scan(&d.ID, &d.Name, &d.Path); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDir(id int64) error {
	_, err := s.db.Exec(`DELETE FROM dirs WHERE id = ?`, id)
	return err
}

// SeedDefaults inserts the default shell tool on an empty tools table:
// zsh on macOS, bash on Linux.
func (s *Store) SeedDefaults(goos string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tools`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	shell := "bash"
	if goos == "darwin" {
		shell = "zsh"
	}
	_, err := s.CreateTool(shell, shell)
	return err
}
