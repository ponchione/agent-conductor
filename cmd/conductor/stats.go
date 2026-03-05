package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
		fmt.Printf("%-10s  %-14s  %-6s  %-10s  %-8s  %-10s  %s\n", "ID", "TYPE", "VERIFY", "HUMAN", "CMPLX", "AGREE", "TOKENS")
		for _, r := range recent {
			id := r.WorkflowID
			if len(id) > 8 {
				id = id[:8]
			}
			wot := nullStr(r.WorkOrderType)
			vr := nullStr(r.VerifyResult)
			hr := nullStr(r.HumanResult)
			cmplx := nullStr(r.ScopeEstimatedComplexity)
			agree := r.Agreement
			if agree == "" {
				agree = "-"
			}
			fmt.Printf("%-10s  %-14s  %-6s  %-10s  %-8s  %-10s  %s\n", id, wot, vr, hr, cmplx, agree, fmtInt(r.TotalTokens))
		}

		// --- VERIFY vs HUMAN AGREEMENT ---
		agreementRows, err := db.GetVerifyHumanAgreement(ctx)
		if err != nil {
			log.Printf("warning: failed to fetch agreement data: %v", err)
		} else {
			printAgreementSection(agreementRows)
		}

		// --- BY WORK ORDER TYPE ---
		typeRows, err := db.GetStatsByWorkOrderType(ctx)
		if err != nil {
			log.Printf("warning: failed to fetch work order type stats: %v", err)
		} else {
			printTypeBreakdownSection(typeRows)
		}

		// --- SCOPE QUALITY ---
		sqStats, err := db.GetScopeQualityStats(ctx)
		if err != nil {
			log.Printf("warning: failed to fetch scope quality stats: %v", err)
		} else {
			printScopeQualitySection(sqStats)
		}

		// --- SUB-CALL SUMMARY ---
		printSubCallSummary(ctx, db)

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

func printAgreementSection(rows []database.GetVerifyHumanAgreementRow) {
	// Build (verify_result, human_result) → count map.
	type key struct{ v, h string }
	m := make(map[key]int64)
	for _, r := range rows {
		m[key{r.VerifyResult.String, r.HumanResult.String}] = r.Count
	}

	fmt.Println()
	fmt.Println("--- VERIFY vs HUMAN AGREEMENT ---")
	fmt.Printf("%-8s  %8s  %8s\n", "", "approved", "rejected")
	for _, v := range []string{"PASS", "WARN", "FAIL"} {
		fmt.Printf("%-8s  %8d  %8d\n", v,
			m[key{v, "approved"}], m[key{v, "rejected"}])
	}

	agreed := m[key{"PASS", "approved"}] +
		m[key{"WARN", "approved"}] + m[key{"WARN", "rejected"}] +
		m[key{"FAIL", "rejected"}]
	var total int64
	for _, c := range m {
		total += c
	}
	if total > 0 {
		fmt.Printf("Agreement: %d/%d (%d%%)\n", agreed, total, agreed*100/total)
	} else {
		fmt.Printf("Agreement: 0/0 (0%%)\n")
	}
}

func printTypeBreakdownSection(rows []database.GetStatsByWorkOrderTypeRow) {
	fmt.Println()
	fmt.Println("--- BY WORK ORDER TYPE ---")
	fmt.Printf("%-16s  %5s  %4s  %4s  %4s  %8s  %8s  %9s\n",
		"TYPE", "TOTAL", "PASS", "WARN", "FAIL", "APPROVED", "REJECTED", "AVG SCOPE")
	for _, r := range rows {
		wot := nullStr(r.WorkOrderType)
		avgScope := "-"
		if r.AvgScopeSecs.Valid {
			avgScope = fmt.Sprintf("%ds", int64(r.AvgScopeSecs.Float64))
		}
		fmt.Printf("%-16s  %5d  %4d  %4d  %4d  %8d  %8d  %9s\n",
			wot, r.Total,
			nullF64ToInt(r.VerifyPass),
			nullF64ToInt(r.VerifyWarn),
			nullF64ToInt(r.VerifyFail),
			nullF64ToInt(r.HumanApproved),
			nullF64ToInt(r.HumanRejected),
			avgScope,
		)
	}
}

func printScopeQualitySection(s database.GetScopeQualityStatsRow) {
	fmt.Println()
	fmt.Println("--- SCOPE QUALITY ---")
	if s.AvgPathsStripped.Valid {
		fmt.Printf("Avg paths stripped:      %.1f\n", s.AvgPathsStripped.Float64)
	} else {
		fmt.Printf("Avg paths stripped:      0.0\n")
	}
	if s.AvgPathsReclassified.Valid {
		fmt.Printf("Avg paths reclassified:  %.1f\n", s.AvgPathsReclassified.Float64)
	} else {
		fmt.Printf("Avg paths reclassified:  0.0\n")
	}
	fmt.Printf("Complexity distribution: low %d  medium %d  high %d\n",
		nullF64ToInt(s.ComplexityLow),
		nullF64ToInt(s.ComplexityMedium),
		nullF64ToInt(s.ComplexityHigh),
	)
}

func printSubCallSummary(ctx context.Context, db *database.DB) {
	providerRows, err := db.GetSubCallGlobalStatsByProvider(ctx)
	if err != nil {
		log.Printf("warning: failed to fetch sub-call provider stats: %v", err)
		return
	}
	if len(providerRows) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("--- SUB-CALL SUMMARY ---")
	fmt.Printf("%-16s  %6s  %12s  %12s  %10s\n", "Provider", "Calls", "Tokens In", "Tokens Out", "Est. Cost")

	var totalCalls int64
	for _, r := range providerRows {
		totalCalls += r.CallCount
		cost := "$0.00"
		if r.TotalEstimatedCostUsd.Valid {
			cost = fmt.Sprintf("$%.2f", r.TotalEstimatedCostUsd.Float64)
		}
		fmt.Printf("%-16s  %6d  %12s  %12s  %10s\n",
			r.Provider,
			r.CallCount,
			fmtInt(nullF64ToInt(r.TotalTokensIn)),
			fmtInt(nullF64ToInt(r.TotalTokensOut)),
			cost,
		)
	}

	phaseRows, err := db.GetSubCallPhaseAverages(ctx)
	if err != nil {
		log.Printf("warning: failed to fetch sub-call phase averages: %v", err)
	} else if len(phaseRows) > 0 {
		fmt.Printf("\nAvg sub-calls:   ")
		for i, r := range phaseRows {
			avg := float64(r.TotalCalls)
			if r.RunCount > 0 {
				avg = float64(r.TotalCalls) / float64(r.RunCount)
			}
			if i > 0 {
				fmt.Printf("    ")
			}
			fmt.Printf("%s %.1f", r.Phase, avg)
		}
		fmt.Println()
	}

	if totalCalls > 0 {
		fmt.Printf("\nDistribution:    ")
		for i, r := range providerRows {
			pctVal := r.CallCount * 100 / totalCalls
			if i > 0 {
				fmt.Printf("    ")
			}
			fmt.Printf("%s %d%%", r.Provider, pctVal)
		}
		fmt.Println()
	}
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
