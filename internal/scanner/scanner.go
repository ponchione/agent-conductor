package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
)

type Scanner struct {
	cfg     *config.Config
	db      *database.DB
	git     *git.GitManager
	watcher *fsnotify.Watcher
	timers  map[string]*time.Timer
	mu      sync.Mutex
}

func New(cfg *config.Config, db *database.DB, gitMgr *git.GitManager) (*Scanner, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Scanner{
		cfg:     cfg,
		db:      db,
		git:     gitMgr,
		watcher: watcher,
		timers:  make(map[string]*time.Timer),
	}, nil
}

func (s *Scanner) Start(ctx context.Context) error {
	for repoName := range s.cfg.Repos {
		orderPath := filepath.Join(s.cfg.Scanner.InboxPath, repoName, "orders")
		if err := os.MkdirAll(orderPath, 0755); err != nil {
			return err
		}
		if err := s.watcher.Add(orderPath); err != nil {
			return err
		}

		ticketPath := filepath.Join(s.cfg.Scanner.InboxPath, repoName, "tickets")
		if err := os.MkdirAll(ticketPath, 0755); err != nil {
			return err
		}
		if err := s.watcher.Add(ticketPath); err != nil {
			return err
		}

		slog.Info("Watching inbox", "repo", repoName, "path", orderPath)
	}
	go s.eventLoop(ctx)
	return nil
}

func (s *Scanner) Stop() { s.watcher.Close() }

func (s *Scanner) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				s.debounce(event.Name)
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("Watcher error", "error", err)
		}
	}
}

func (s *Scanner) debounce(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.timers[path]; ok {
		t.Stop()
	}
	s.timers[path] = time.AfterFunc(500*time.Millisecond, func() {
		s.mu.Lock()
		delete(s.timers, path)
		s.mu.Unlock()
		if err := s.processFile(path); err != nil {
			slog.Error("Failed to process file", "path", path, "error", err)
		}
	})
}

func (s *Scanner) processFile(path string) error {
	parts := strings.Split(path, string(os.PathSeparator))
	if len(parts) < 3 {
		return nil
	}

	dir := filepath.Dir(path)
	typeDir := filepath.Base(dir)
	repoDir := filepath.Base(filepath.Dir(dir))

	if _, ok := s.cfg.Repos[repoDir]; !ok {
		return nil
	}

	fileName := filepath.Base(path)
	if strings.HasPrefix(fileName, ".") || !strings.HasSuffix(fileName, ".md") {
		return nil
	}

	if typeDir == "orders" {
		return s.handleWorkOrder(repoDir, path)
	} else if typeDir == "tickets" {
		return s.handleTicket(repoDir, path)
	}
	return nil
}

func (s *Scanner) handleWorkOrder(repo, path string) error {
	slog.Info("Processing new work order", "repo", repo, "file", filepath.Base(path))

	// Create IDs ahead of time
	wfID := uuid.New().String()
	taskID := uuid.New().String()
	branchName := fmt.Sprintf("%s-%s", s.cfg.Git.BranchPrefix, wfID[:8])

	repoConfig, ok := s.cfg.Repos[repo]
	if ok {
		slog.Info("Creating git branch", "repo", repo, "branch", branchName)
		if err := s.git.CreateBranch(repoConfig.Path, branchName, "main"); err != nil {
			slog.Error("Failed to create git branch", "error", err)
			return fmt.Errorf("git create branch failed: %w", err)
		}
	}

	err := s.db.CreateWorkflow(context.Background(), database.CreateWorkflowParams{
		ID:              wfID,
		OriginalIntent:  "Work Order: " + filepath.Base(path),
		OriginalFile:    path,
		CurrentState:    "pending",
		TargetRepo:      repo,
		GitBranch:       branchName,
		MaxDepth:        int64(s.cfg.Safety.MaxDepth),
		MaxFilesChanged: int64(s.cfg.Safety.MaxFilesChanged),
		MaxDurationMins: int64(s.cfg.Safety.MaxWorkflowDurationMinutes),
	})
	if err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}

	err = s.db.CreateTask(context.Background(), database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wfID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     s.cfg.Repos[repo].OpenCodeAgentExecutor,
		TargetRepo:    repo,
		InputArtifact: path,
		State:         "pending",
		MaxAttempts:   int64(s.cfg.Safety.MaxTaskRetries),
	})
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	s.db.LogEvent(wfID, taskID, "workflow_created", map[string]any{"file": path, "branch": branchName})
	return nil
}

func (s *Scanner) handleTicket(repo, path string) error {
	slog.Info("Processing new ticket", "repo", repo, "file", filepath.Base(path))
	fileName := filepath.Base(path)
	parts := strings.Split(fileName, "-")

	var workflowID string
	if len(parts) >= 2 && parts[0] == "ticket" {
		workflowID = parts[1]
	}

	var wf database.Workflow
	var err error

	if workflowID != "" {
		wf, err = s.db.GetWorkflow(context.Background(), workflowID)
	}

	// If workflow not found or error, create new one via handleWorkOrder
	if workflowID == "" || err != nil {
		slog.Info("Could not link ticket to workflow, creating new one", "ticket", fileName)
		return s.handleWorkOrder(repo, path)
	}

	// Link to existing workflow
	taskID := uuid.New().String()

	err = s.db.CreateTask(context.Background(), database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wf.ID,
		SequenceNum:   int64(wf.CurrentDepth + 1),
		TaskType:      "work_order_generation",
		AgentType:     s.cfg.Repos[repo].OpenCodeAgentWorkOrder,
		TargetRepo:    repo,
		InputArtifact: path,
		State:         "pending",
		MaxAttempts:   int64(s.cfg.Safety.MaxTaskRetries),
	})
	if err != nil {
		return err
	}

	s.db.LogEvent(wf.ID, taskID, "task_created_from_ticket", map[string]any{"file": path})
	return nil
}
