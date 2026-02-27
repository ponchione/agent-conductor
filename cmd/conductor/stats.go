package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Display pipeline statistics",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		ctx := context.Background()

		stats, err := db.GetPipelineStats(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch pipeline stats: %w", err)
		}

		recent, err := db.GetRecentPipelineRuns(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch recent runs: %w", err)
		}

		total := stats.TotalRuns

		fmt.Println("=== PIPELINE STATS ===")
		fmt.Printf("Total runs:     %d\n", total)

		vPass := nullF64ToInt(stats.VerifyPass)
		vWarn := nullF64ToInt(stats.VerifyWarn)
		vFail := nullF64ToInt(stats.VerifyFail)
		fmt.Printf("Verify results: PASS %d (%d%%)  WARN %d (%d%%)  FAIL %d (%d%%)\n",
			vPass, pct(vPass, total),
			vWarn, pct(vWarn, total),
			vFail, pct(vFail, total),
		)

		hApproved := nullF64ToInt(stats.HumanApproved)
		hRejected := nullF64ToInt(stats.HumanRejected)
		hPending := nullF64ToInt(stats.HumanPending)
		fmt.Printf("Human results:  approved %d    rejected %d     pending %d\n",
			hApproved, hRejected, hPending,
		)

		fmt.Println()
		fmt.Println("--- TOKEN USAGE ---")
		fmt.Printf("Scope:   %s in  /  %s out\n",
			fmtInt(nullF64ToInt(stats.TotalScopeTokensIn)),
			fmtInt(nullF64ToInt(stats.TotalScopeTokensOut)),
		)
		fmt.Printf("Verify:  %s in  /  %s out\n",
			fmtInt(nullF64ToInt(stats.TotalVerifyTokensIn)),
			fmtInt(nullF64ToInt(stats.TotalVerifyTokensOut)),
		)

		fmt.Println()
		fmt.Println("--- PHASE TIMING (avg) ---")
		if stats.AvgScopeSecs.Valid {
			fmt.Printf("Scope:   %ds\n", int64(stats.AvgScopeSecs.Float64))
		} else {
			fmt.Printf("Scope:   0s\n")
		}
		if stats.AvgVerifySecs.Valid {
			fmt.Printf("Verify:  %ds\n", int64(stats.AvgVerifySecs.Float64))
		} else {
			fmt.Printf("Verify:  0s\n")
		}

		fmt.Println()
		fmt.Println("--- RECENT RUNS ---")
		fmt.Printf("%-10s  %-14s  %-6s  %-10s  %s\n", "ID", "TYPE", "VERIFY", "HUMAN", "TOKENS")
		for _, r := range recent {
			id := r.WorkflowID
			if len(id) > 8 {
				id = id[:8]
			}
			wot := nullStr(r.WorkOrderType)
			vr := nullStr(r.VerifyResult)
			hr := nullStr(r.HumanResult)
			fmt.Printf("%-10s  %-14s  %-6s  %-10s  %s\n", id, wot, vr, hr, fmtInt(r.TotalTokens))
		}

		return nil
	},
}

func nullF64ToInt(f sql.NullFloat64) int64 {
	if !f.Valid {
		return 0
	}
	return int64(f.Float64)
}

func nullStr(s sql.NullString) string {
	if !s.Valid {
		return "-"
	}
	return s.String
}

func pct(part, total int64) int64 {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}

// fmtInt formats an int64 with comma separators (e.g. 1234567 → "1,234,567").
func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
