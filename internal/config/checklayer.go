package config

// CheckLayer validates one config file's bytes against the config schema, as Load does for every
// layer it reads. The problem quotes no file content.
func CheckLayer(path string, data []byte) error {
	_, err := parseLayer(path, data)
	return err
}
