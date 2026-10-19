package main

import (
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/ent/team"
)

// aiCmd checks the AI integration and manages budgets.
func aiCmd() *cobra.Command {
	c := &cobra.Command{Use: "ai", Short: "Check the AI integration, usage and budgets"}
	c.AddCommand(aiPingCmd(), aiUsageCmd(), aiBudgetCmd())
	return c
}

func aiPingCmd() *cobra.Command {
	var record string
	c := &cobra.Command{
		Use:   "ping",
		Short: "Make one small structured call through the default AI integration",
		Long: `Make one small structured call through the default AI integration and print
the answer, model and tokens. --record writes the answer as a fixture that the
fake driver replays (point KITMAKER_AI_FIXTURES at the directory).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			client, err := a.AI.Client(cmd.Context())
			if err != nil {
				return err
			}
			if record != "" {
				client = &ai.Recorder{Client: client, Dir: record}
			}
			start := time.Now()
			res, err := ai.Ping(cmd.Context(), client)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\n", res.JSON)
			fmt.Fprintf(out, "provider %s · model %s · %s · in %d (cache read %d, write %d) · out %d tokens\n",
				client.Provider(), res.Model, time.Since(start).Round(time.Millisecond),
				res.Usage.InputTokens, res.Usage.CacheReadTokens, res.Usage.CacheWriteTokens, res.Usage.OutputTokens)
			return nil
		},
	}
	c.Flags().StringVar(&record, "record", "", "write the answer as a fixture into this directory")
	return c
}

func aiUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show this month's AI usage per team against its budget",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			a, err := open(ctx)
			if err != nil {
				return err
			}
			defer a.Close()
			b, err := a.AI.Budget(ctx)
			if err != nil {
				return err
			}
			teams, err := a.Ent.Team.Query().Order(team.BySlug()).All(ctx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TEAM\tUSED (billable tokens)\tMONTHLY LIMIT")
			for _, t := range teams {
				used, err := ai.Used(ctx, a.Ent, t.ID, time.Now())
				if err != nil {
					return err
				}
				limit := "none"
				if l := b.LimitFor(t.ID); l > 0 {
					limit = strconv.FormatInt(l, 10)
				}
				fmt.Fprintf(w, "%s\t%d\t%s\n", t.Slug, used, limit)
			}
			return w.Flush()
		},
	}
}

func aiBudgetCmd() *cobra.Command {
	var teamSlug string
	c := &cobra.Command{
		Use:   "budget <tokens>",
		Short: "Set the monthly AI budget in billable tokens (0 = no limit)",
		Long: `Set the monthly AI budget in billable tokens: input + cache writes + output,
plus a tenth of cache reads. Without --team it is the default for every team;
with --team it overrides that team's limit (0 = unlimited for that team).`,
		Example: `  kitmaker ai budget 20000000
  kitmaker ai budget 50000000 --team platform`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tokens, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || tokens < 0 {
				return fmt.Errorf("tokens must be a whole number ≥ 0, got %q", args[0])
			}
			a, err := open(ctx)
			if err != nil {
				return err
			}
			defer a.Close()
			b, err := a.AI.Budget(ctx)
			if err != nil {
				return err
			}
			if teamSlug == "" {
				b.TeamMonthlyTokens = tokens
			} else {
				t, err := a.Ent.Team.Query().Where(team.Slug(teamSlug)).Only(ctx)
				if err != nil {
					return fmt.Errorf("team %s: %w", teamSlug, err)
				}
				if b.Teams == nil {
					b.Teams = map[string]int64{}
				}
				b.Teams[t.ID] = tokens
			}
			if err := a.Settings.SetSetting(ctx, ai.BudgetSettingKey, b); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "budget saved")
			return nil
		},
	}
	c.Flags().StringVar(&teamSlug, "team", "", "team slug to override")
	return c
}
