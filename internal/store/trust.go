package store

// Trust records that the project file at path with this content hash is trusted. Trusting a path
// again replaces its hash, so a changed file is untrusted until it is trusted anew.
func (s *Store) Trust(path, sha256 string) error {
	file := s.path("trust.json")
	return s.withLock(func() error {
		m := map[string]string{}
		if err := readJSON(file, &m); err != nil {
			return err
		}
		m[path] = sha256
		return writeJSON(file, m)
	})
}

// Trusted reports whether path was trusted with exactly this content hash. It reads without the
// lock: writes are atomic renames, so a reader sees the old file or the new one.
func (s *Store) Trusted(path, sha256 string) (bool, error) {
	m := map[string]string{}
	if err := readJSON(s.path("trust.json"), &m); err != nil {
		return false, err
	}
	got, ok := m[path]
	return ok && got == sha256, nil
}
