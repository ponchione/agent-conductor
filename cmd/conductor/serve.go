package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/api"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
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

		slog.Info("starting observability API server", "addr", serveAddr, "db", dbPath)
		if err := http.ListenAndServe(serveAddr, api.NewServer(db, gitMgr, baseBranch)); err != nil {
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
