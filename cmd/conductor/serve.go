package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/ponchione/agent-conductor/internal/api"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/lock"
	"github.com/spf13/cobra"
)

var (
	serveAddr    string
	serveDBPath  string
	serveDataDir string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve read-only observability APIs over HTTP",
	Args:  cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configureLogging()
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, err := resolveServeDBPath(cmd)
		if err != nil {
			return err
		}

		// Resolve data dir for lock file
		dataDir := ""
		if cfg != nil {
			dataDir = cfg.Project.DataDir
		} else if serveDataDir != "" {
			dataDir = serveDataDir
		}

		// Parse port from addr for lock file
		_, portStr, _ := net.SplitHostPort(serveAddr)
		port, _ := strconv.Atoi(portStr)

		// Check for existing live server
		if dataDir != "" {
			existing, err := lock.CheckLock(dataDir)
			if err != nil {
				return fmt.Errorf("check lock: %w", err)
			}
			if existing != nil {
				return fmt.Errorf("another topham server is already running (PID %d, port %d). Stop it first or use that instance.", existing.PID, existing.Port)
			}

			// Write lock
			if err := lock.WriteLock(dataDir, port); err != nil {
				return fmt.Errorf("write lock: %w", err)
			}
			defer lock.RemoveLock(dataDir)
		}

		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		gitMgr := git.New(nil)
		baseBranch := "main"
		if cfg != nil && cfg.Git.BaseBranch != "" {
			baseBranch = cfg.Git.BaseBranch
		}

		rq := api.NewRunQueue()

		workOrderDir := ""
		if cfg != nil {
			workOrderDir = filepath.Join(cfg.Project.DataDir, "work-orders")
			executeFn, monitorFn := buildServeQueueCallbacks(db, cfg, gitMgr, workOrderDir)
			rq.SetCallbacks(executeFn, monitorFn)
		}

		handler := api.NewServer(db, gitMgr, baseBranch, rq, workOrderDir, cfg)
		srv := &http.Server{Addr: serveAddr, Handler: handler}

		// Signal handling for graceful shutdown
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			<-ctx.Done()
			slog.Info("shutting down server")
			srv.Shutdown(context.Background())
		}()

		slog.Info("starting observability API server", "addr", serveAddr, "db", dbPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve API: %w", err)
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:8088", "HTTP listen address")
	serveCmd.Flags().StringVar(&serveDBPath, "db", "", "Path to an existing conductor.db file")
	serveCmd.Flags().StringVar(&serveDataDir, "data-dir", "", "Path to a conductor project data dir containing db/conductor.db")
}

func resolveServeDBPath(cmd *cobra.Command) (string, error) {
	switch {
	case serveDBPath != "":
		return requireExistingDB(serveDBPath)
	case serveDataDir != "":
		return requireExistingDB(filepath.Join(serveDataDir, "db", "conductor.db"))
	}

	if projectFlag := rootCmd.PersistentFlags().Lookup("project"); projectFlag != nil && projectFlag.Changed {
		if err := loadProjectConfig(); err != nil {
			return "", err
		}
		return requireExistingDB(filepath.Join(cfg.Project.DataDir, "db", "conductor.db"))
	}

	if _, err := os.Stat(projectPath); err == nil {
		if err := loadProjectConfig(); err != nil {
			return "", err
		}
		return requireExistingDB(filepath.Join(cfg.Project.DataDir, "db", "conductor.db"))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check project config %q: %w", projectPath, err)
	}

	return "", fmt.Errorf("serve requires an existing database; pass --db <path>, --data-dir <dir>, or --project <path>")
}

func requireExistingDB(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("database not found at %q", path)
		}
		return "", fmt.Errorf("stat database %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("database path %q is a directory", path)
	}
	return path, nil
}
