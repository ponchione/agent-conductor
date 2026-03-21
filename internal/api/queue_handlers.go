package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleAddQueueItems(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	var req queueAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	for _, addReq := range req.Items {
		woPath := addReq.WorkOrderFile
		if !filepath.IsAbs(woPath) && s.workOrderDir != "" {
			woPath = filepath.Join(s.workOrderDir, woPath)
		}

		data, err := os.ReadFile(woPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot read work order file %q: %v", addReq.WorkOrderFile, err))
			return
		}

		var wo struct {
			Title        string `yaml:"title"`
			Type         string `yaml:"type"`
			TargetModule string `yaml:"target_module"`
		}
		if err := yaml.Unmarshal(data, &wo); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot parse work order file %q: %v", addReq.WorkOrderFile, err))
			return
		}

		s.runQueue.Add(QueueItem{
			WorkOrderFile: addReq.WorkOrderFile,
			Title:         wo.Title,
			Type:          wo.Type,
			TargetModule:  wo.TargetModule,
			Overrides:     addReq.Overrides,
		})
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleRemoveQueueItem(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	id := chi.URLParam(r, "id")
	if err := s.runQueue.Remove(id); err != nil {
		if errors.Is(err, ErrItemExecuting) {
			writeError(w, http.StatusConflict, "cannot remove executing item")
			return
		}
		if errors.Is(err, ErrItemNotFound) {
			writeError(w, http.StatusNotFound, "queue item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleReorderQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	var req queueReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if err := s.runQueue.Reorder(req.Order); err != nil {
		if errors.Is(err, ErrInvalidItemIDs) {
			writeError(w, http.StatusBadRequest, "invalid item IDs")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleStartQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Start(); err != nil {
		if errors.Is(err, ErrQueueInvalidState) {
			writeError(w, http.StatusConflict, "queue is not in ready state")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handlePauseQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Pause(); err != nil {
		if errors.Is(err, ErrQueueInvalidState) {
			writeError(w, http.StatusConflict, "queue is not running")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleContinueQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Continue(); err != nil {
		if errors.Is(err, ErrQueueInvalidState) {
			writeError(w, http.StatusConflict, "queue is not paused")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

func (s *Server) handleQueueEvents(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	ch := s.runQueue.Subscribe()
	defer s.runQueue.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
