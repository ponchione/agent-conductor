package rag

import (
	"encoding/json"
	"os"
)

func loadFileHashes(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string), nil // corrupt → start fresh
	}
	return m, nil
}

func saveFileHashes(path string, hashes map[string]string) error {
	data, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
