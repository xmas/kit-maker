package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/user"
)

// userCmd is break-glass and day-one account admin from the command line
// (DESIGN.md §12). Anyone with database access can run it.
func userCmd() *cobra.Command {
	c := &cobra.Command{Use: "user", Aliases: []string{"users"}, Short: "Manage accounts (break-glass admin)"}
	c.AddCommand(userListCmd(), userCreateCmd(), userPasswordCmd(), userStatusCmd("suspend", user.StatusSuspended),
		userStatusCmd("activate", user.StatusActive), userSignOutCmd())
	return c
}

func userListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			users, err := a.Ent.User.Query().Order(ent.Asc(user.FieldEmail)).All(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "EMAIL\tNAME\tSTATUS\tPASSWORD\tLAST SIGN-IN")
			for _, u := range users {
				last := "never"
				if u.LastLoginAt != nil {
					last = u.LastLoginAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\n", u.Email, u.DisplayName, u.Status, u.PasswordHash != nil, last)
			}
			return w.Flush()
		},
	}
}

func userCreateCmd() *cobra.Command {
	var name string
	var stdin, generate bool
	c := &cobra.Command{
		Use:   "create <email>",
		Short: "Create a local account",
		Example: `  kitmaker user create sam@acme.com --name "Sam R" --generate-password
  printf '%s' "$PW" | kitmaker user create sam@acme.com --password-stdin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readPassword(cmd, stdin, generate)
			if err != nil {
				return err
			}
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			u, err := (&auth.Service{Ent: a.Ent}).CreateUser(cmd.Context(), args[0], name, pw)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (%s)\n", u.Email, u.ID)
			if generate {
				fmt.Fprintf(cmd.OutOrStdout(), "password: %s\n", pw)
			}
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "display name")
	c.Flags().BoolVar(&stdin, "password-stdin", false, "read the password from stdin")
	c.Flags().BoolVar(&generate, "generate-password", false, "generate a password and print it once")
	return c
}

func userPasswordCmd() *cobra.Command {
	var stdin, generate bool
	c := &cobra.Command{
		Use:   "set-password <email>",
		Short: "Set or reset a local password (signs the user out everywhere)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readPassword(cmd, stdin, generate)
			if err != nil {
				return err
			}
			if pw == "" {
				return errors.New("pass --password-stdin or --generate-password")
			}
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			u, err := (&auth.Service{Ent: a.Ent}).SetPassword(cmd.Context(), args[0], pw)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password set for %s; existing sessions signed out\n", u.Email)
			if generate {
				fmt.Fprintf(cmd.OutOrStdout(), "password: %s\n", pw)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&stdin, "password-stdin", false, "read the password from stdin")
	c.Flags().BoolVar(&generate, "generate-password", false, "generate a password and print it once")
	return c
}

func userStatusCmd(verb string, status user.Status) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <email>",
		Short: strings.ToUpper(verb[:1]) + verb[1:] + " an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			u, err := (&auth.Service{Ent: a.Ent}).SetStatus(cmd.Context(), args[0], status)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s\n", u.Email, u.Status)
			return nil
		},
	}
}

func userSignOutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sign-out <email>",
		Short: "Sign an account out of every session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			u, err := a.Ent.User.Query().Where(user.Email(auth.NormalizeEmail(args[0]))).Only(cmd.Context())
			if err != nil {
				return err
			}
			if err := (&auth.Service{Ent: a.Ent}).SignOutEverywhere(cmd.Context(), u.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s signed out everywhere\n", u.Email)
			return nil
		},
	}
}

func readPassword(cmd *cobra.Command, stdin, generate bool) (string, error) {
	switch {
	case stdin && generate:
		return "", errors.New("use --password-stdin or --generate-password, not both")
	case generate:
		return auth.RandomPassword(20)
	case stdin:
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	default:
		return "", nil
	}
}
