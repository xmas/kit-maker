package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/secrets"
	"github.com/docker/kit-maker/internal/settings"
)

// secretsCmd groups keeper helpers that need no database.
func secretsCmd() *cobra.Command {
	c := &cobra.Command{Use: "secrets", Short: "Secrets keeper helpers"}
	c.AddCommand(&cobra.Command{
		Use:   "new-key",
		Short: "Print a fresh local keeper URL for KITMAKER_SECRETS_URL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			u, err := secrets.NewLocalKeyURL()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), u)
			return nil
		},
	})
	return c
}

// integrationCmd manages per-deployment drivers from the command line.
func integrationCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "integration",
		Aliases: []string{"integrations"},
		Short:   "Manage registries, build runners, git hosts and AI providers",
	}
	c.AddCommand(integrationListCmd(), integrationSetCmd(), integrationTestCmd(), integrationDriversCmd())
	return c
}

func integrationDriversCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drivers",
		Short: "List available drivers and their config fields",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KIND\tDRIVER\tFIELDS")
			for _, k := range drivers.Kinds {
				for _, n := range drivers.Names(k) {
					d, _ := drivers.Get(k, n)
					var fs []string
					for _, f := range d.Fields() {
						s := f.Key
						if f.Required {
							s += "*"
						}
						if f.Secret {
							s += " (secret)"
						}
						fs = append(fs, s)
					}
					fmt.Fprintf(w, "%s\t%s\t%s\n", k, n, strings.Join(fs, ", "))
				}
			}
			return w.Flush()
		},
	}
}

func integrationListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List integrations (secrets redacted)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			list, err := a.Settings.List(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tNAME\tDRIVER\tDEFAULT\tSTATUS")
			for _, i := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n", i.ID, i.Kind, i.Name, i.Driver, i.IsDefault, status(i))
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

func status(i settings.Integration) string {
	switch {
	case i.LastError != "":
		return i.LastError
	case i.VerifiedAt != nil:
		return "verified " + i.VerifiedAt.Format("2006-01-02 15:04")
	default:
		return "not tested"
	}
}

func integrationSetCmd() *cobra.Command {
	var name string
	var cfg []string
	var isDefault, test bool
	c := &cobra.Command{
		Use:   "set <kind> <driver>",
		Short: "Create or update an integration",
		Example: `  kitmaker integration set registry zot --name local --config host=zot:5000 --config namespace=kits --config insecure=true --default --test
  kitmaker integration set ai anthropic --config api_key=$ANTHROPIC_API_KEY --default`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			conf := drivers.Config{}
			for _, kv := range cfg {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("--config wants key=value, got %q", kv)
				}
				conf[k] = v
			}
			i, err := a.Settings.Save(cmd.Context(), settings.SaveInput{
				Kind: drivers.Kind(args[0]), Driver: args[1], Name: name,
				Config: conf, IsDefault: isDefault, Test: test,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved %s %s (%s): %s\n", i.Kind, i.Name, i.ID, status(*i))
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "name (defaults to the driver)")
	c.Flags().StringArrayVar(&cfg, "config", nil, "config key=value (repeatable)")
	c.Flags().BoolVar(&isDefault, "default", false, "make it the default for its kind")
	c.Flags().BoolVar(&test, "test", false, "test against the real service before saving")
	return c
}

func integrationTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <id>",
		Short: "Test an integration against the real service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			i, err := a.Settings.Test(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", i.Kind, i.Name, status(*i))
			if i.LastError != "" && i.VerifiedAt == nil {
				os.Exit(2)
			}
			return nil
		},
	}
}
