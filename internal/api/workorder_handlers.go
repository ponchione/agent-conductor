package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// workOrderMeta holds metadata extracted from a work order YAML file.
type workOrderMeta struct {
	Title        string `yaml:"title"`
	Type         string `yaml:"type"`
	TargetModule string `yaml:"target_module"`
}

// validateWorkOrderFilename rejects filenames that are empty, contain path
// traversal components, or do not end in .yaml/.yml.
func validateWorkOrderFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename is required")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("filename must not contain '..'")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("filename must not contain path separators")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("filename must end in .yaml or .yml")
	}
	return nil
}

// parseWorkOrderYAML extracts title, type, and target_module from raw YAML.
func parseWorkOrderYAML(data []byte) (workOrderMeta, error) {
	var meta workOrderMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return workOrderMeta{}, fmt.Errorf("invalid YAML: %w", err)
	}
	return meta, nil
}

func (s *Server) handleListWorkOrders(w http.ResponseWriter, r *http.Request) {
	if s.workOrderDir == "" {
		writeJSON(w, http.StatusOK, workOrderListResponse{
			WorkOrders: []workOrderFileResponse{},
		})
		return
	}

	entries, err := os.ReadDir(s.workOrderDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, workOrderListResponse{
				WorkOrders: []workOrderFileResponse{},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read work-orders directory: %v", err))
		return
	}

	var files []workOrderFileResponse
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		resp := workOrderFileResponse{
			Filename: entry.Name(),
		}

		info, err := entry.Info()
		if err == nil {
			resp.SizeBytes = info.Size()
			resp.ModTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}

		data, err := os.ReadFile(filepath.Join(s.workOrderDir, entry.Name()))
		if err == nil {
			meta, err := parseWorkOrderYAML(data)
			if err == nil {
				resp.Title = meta.Title
				resp.Type = meta.Type
				resp.TargetModule = meta.TargetModule
			}
		}

		files = append(files, resp)
	}

	if files == nil {
		files = []workOrderFileResponse{}
	}

	writeJSON(w, http.StatusOK, workOrderListResponse{
		WorkOrders: files,
		Directory:  s.workOrderDir,
	})
}

func (s *Server) handleGetWorkOrder(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if err := validateWorkOrderFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.workOrderDir == "" {
		writeError(w, http.StatusNotFound, "work order directory not configured")
		return
	}

	path := filepath.Join(s.workOrderDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("work order %q not found", filename))
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read work order: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workOrderContentResponse{
		Filename: filename,
		Content:  string(data),
	})
}

func (s *Server) handleUpdateWorkOrder(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if err := validateWorkOrderFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.workOrderDir == "" {
		writeError(w, http.StatusNotFound, "work order directory not configured")
		return
	}

	path := filepath.Join(s.workOrderDir, filename)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("work order %q not found", filename))
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("stat work order: %v", err))
		return
	}

	var req workOrderUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}

	if _, err := parseWorkOrderYAML([]byte(req.Content)); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("content is not valid YAML: %v", err))
		return
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("write work order: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workOrderUpdateResponse{
		Filename: filename,
		Content:  req.Content,
		Valid:    true,
	})
}
