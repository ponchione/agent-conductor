package scanner

import (
	"context"
	"database/sql"
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
	"github.com/ponchione/agent-conductor/internal/models"
	"gopkg.in/yaml.v3"
)

type Scanner struct {
	cfg     *config.ProjectConfig
	db      *database.DB
	git     *git.GitManager
	watcher *fsnotify.Watcher
	timers  map[string]*time.Timer
	mu      sync.Mutex
}

func New(cfg *config.ProjectConfig, db *database.DB, gitMgr *git.GitManager) (*Scanner, error) {
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
	baseInbox := filepath.Join(s.cfg.Project.DataDir, "inbox")
	orderPath := filepath.Join(baseInbox, "orders")
	if err := os.MkdirAll(orderPath, 0755); err != nil {
		return err
	}
	if err := s.watcher.Add(orderPath); err != nil {
		return err
	}

	slog.Info("Watching inbox", "path", baseInbox)

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
	if len(parts) < 2 {
		return nil
	}

	dir := filepath.Dir(path)
	typeDir := filepath.Base(dir)
	// inbox/orders -> typeDir=orders

	fileName := filepath.Base(path)
	if strings.HasPrefix(fileName, ".") || !strings.HasSuffix(fileName, ".md") {
		return nil
	}

	if typeDir == "orders" {
		return s.handleWorkOrder(path)
	}
	return nil
}

// validateWorkOrder parses and validates required fields of a work order file.
// Returns a descriptive error if any required field is missing or invalid.
func validateWorkOrder(path string) (*models.WorkOrder, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read work order: %w", err)
	}

	var wo models.WorkOrder
	if err := yaml.Unmarshal(data, &wo); err != nil {
		return nil, fmt.Errorf("invalid YAML in work order: %w", err)
	}

	if strings.TrimSpace(wo.Title) == "" {
		return nil, fmt.Errorf("work order missing required field: title")
	}
	if strings.TrimSpace(wo.TargetModule) == "" {
		return nil, fmt.Errorf("work order missing required field: target_module")
	}
	if strings.TrimSpace(wo.Type) == "" {
		return nil, fmt.Errorf("work order missing required field: type")
	}
	if len(wo.AcceptanceCriteria) == 0 {
		return nil, fmt.Errorf("work order missing required field: acceptance_criteria (must have at least one entry)")
	}

	return &wo, nil
}

func (s *Scanner) handleWorkOrder(path string) error {
	repoName := s.cfg.Project.Name
	slog.Info("Processing new work order", "file", filepath.Base(path))

	// Validate before creating any DB records — bad work orders are rejected here.
	wo, err := validateWorkOrder(path)
	if err != nil {
		slog.Error("Invalid work order — rejecting without creating workflow", "file", filepath.Base(path), "error", err)
		return fmt.Errorf("work order validation failed: %w", err)
	}

	// Create IDs ahead of time
	wfID := uuid.New().String()
	taskID := uuid.New().String()
	branchName := fmt.Sprintf("%s-%s", s.cfg.Git.BranchPrefix, wfID[:8])

	slog.Info("Creating git branch", "branch", branchName)
	if err := s.git.CreateBranch(s.cfg.Project.Path, branchName, "main"); err != nil {
		slog.Error("Failed to create git branch", "error", err)
		return fmt.Errorf("git create branch failed: %w", err)
	}
	
	maxDepth := int64(5)
	maxFiles := int64(50)
	maxDuration := int64(60)

	if err := s.db.CreateWorkflow(context.Background(), database.CreateWorkflowParams{
		ID:                     wfID,
		OriginalIntent:         "Work Order: " + filepath.Base(path),
		OriginalFile:           path,
		CurrentState:           "pending",
		TargetRepo:             repoName,
		GitBranch:              branchName,
		ContextPackagePath:     sql.NullString{}, // NULL
		VerificationReportPath: sql.NullString{}, // NULL
		MaxDepth:               maxDepth,
		MaxFilesChanged:        maxFiles,
		MaxDurationMins:        maxDuration,
	}); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}

	if err := s.db.CreateTask(context.Background(), database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wfID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "opencode", // Default agent
		TargetRepo:    repoName,
		Phase:         "scope", // Start with scope phase
		InputArtifact: path,
		State:         "pending",
		MaxAttempts:   2, // Default
	}); err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	s.db.LogEvent(wfID, taskID, "workflow_created", map[string]any{"file": path, "branch": branchName})

	// Create pipeline_run row to track phase timing and outcomes
	pipelineRunID := uuid.New().String()
	if err := s.db.CreatePipelineRun(context.Background(), pipelineRunID, wfID, repoName, wo.Type); err != nil {
		slog.Warn("Failed to create pipeline_run", "workflow", wfID, "error", err)
	}

	return nil
}
