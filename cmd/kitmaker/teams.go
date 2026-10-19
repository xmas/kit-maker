package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/team"
	"github.com/docker/kit-maker/internal/teams"
)

// teamCmd manages teams. Creating teams is an admin action (DESIGN.md §13);
// the CLI is for operators with database access.
func teamCmd() *cobra.Command {
	c := &cobra.Command{Use: "team", Aliases: []string{"teams"}, Short: "Manage teams and memberships"}

	var parent, slug string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a team (optionally nested)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			t, err := (&teams.Service{Ent: a.Ent}).Create(cmd.Context(), args[0], slug, parent)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created team %s (%s)\n", t.Slug, t.ID)
			return nil
		},
	}
	create.Flags().StringVar(&parent, "parent", "", "parent team slug")
	create.Flags().StringVar(&slug, "slug", "", "slug (defaults from the name)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List teams with members",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			ts, err := a.Ent.Team.Query().WithParent().
				WithMemberships(func(q *ent.TeamMembershipQuery) { q.WithUser() }).
				Order(ent.Asc(team.FieldSlug)).All(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TEAM\tPARENT\tMEMBERS")
			for _, t := range ts {
				parent := "-"
				if t.Edges.Parent != nil {
					parent = t.Edges.Parent.Slug
				}
				members := ""
				for i, m := range t.Edges.Memberships {
					if i > 0 {
						members += ", "
					}
					members += m.Edges.User.Email + " (" + m.Role + ")"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", t.Slug, parent, members)
			}
			return w.Flush()
		},
	}

	var role string
	add := &cobra.Command{
		Use:   "add-member <team> <email>",
		Short: "Add someone to a team with a role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			if err := (&teams.Service{Ent: a.Ent}).AddMember(cmd.Context(), args[0], args[1], role); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is %s of %s\n", args[1], role, args[0])
			return nil
		},
	}
	add.Flags().StringVar(&role, "role", authz.RoleContributor, "viewer | contributor | maintainer | team_admin")

	remove := &cobra.Command{
		Use:   "remove-member <team> <email>",
		Short: "Remove someone's manual membership",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return (&teams.Service{Ent: a.Ent}).RemoveMember(cmd.Context(), args[0], args[1])
		},
	}

	move := &cobra.Command{
		Use:   "set-parent <team> [parent]",
		Short: "Nest a team under another (omit parent to un-nest)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			parent := ""
			if len(args) == 2 {
				parent = args[1]
			}
			return (&teams.Service{Ent: a.Ent}).SetParent(cmd.Context(), args[0], parent)
		},
	}
	c.AddCommand(create, list, add, remove, move)
	return c
}

// roleCmd assigns deployment roles and lists the built-in roles.
func roleCmd() *cobra.Command {
	c := &cobra.Command{Use: "role", Aliases: []string{"roles"}, Short: "Deployment roles and the permission catalogue"}
	c.AddCommand(&cobra.Command{
		Use:   "assign <email> <owner|admin|auditor|member>",
		Short: "Grant a deployment role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return (&teams.Service{Ent: a.Ent}).AssignDeploymentRole(cmd.Context(), args[0], args[1])
		},
	}, &cobra.Command{
		Use:   "revoke <email> <role>",
		Short: "Remove a deployment role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return (&teams.Service{Ent: a.Ent}).RevokeDeploymentRole(cmd.Context(), args[0], args[1])
		},
	}, &cobra.Command{
		Use:   "list",
		Short: "List built-in roles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ROLE\tSCOPE\tPERMISSIONS\tDESCRIPTION")
			for _, r := range authz.BuiltInRoles {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.Key, r.Scope, len(r.Permissions), r.Description)
			}
			return w.Flush()
		},
	}, &cobra.Command{
		Use:   "permissions",
		Short: "Print the permission catalogue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "AREA\tPERMISSION\tSCOPE\tDESCRIPTION")
			for _, p := range authz.All() {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Area, p.Key, p.Scope, p.Description)
			}
			return w.Flush()
		},
	})
	return c
}
