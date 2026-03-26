package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Manage persistent key-value store for an environment",
		Long: `Read and write key-value pairs that persist across the entire lifecycle
of an environment. Values set here are available to all hooks via
PREVIEWCTL_STORE_{KEY} environment variables and in config templates
via {{store.KEY}}.

Hooks can set values using 'previewctl store set' to persist state
(e.g., the GCP zone a VM was created in) for use by later hooks.`,
	}

	cmd.AddCommand(
		newStoreSetCmd(),
		newStoreGetCmd(),
		newStoreListCmd(),
	)

	return cmd
}

func newStoreSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <env-name> <KEY=VALUE> [KEY=VALUE...]",
		Short: "Set one or more persistent environment variables",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]

			mgr, _, err := buildManager(nil)
			if err != nil {
				return err
			}

			entry, err := mgr.GetEnvironment(cmd.Context(), envName)
			if err != nil {
				return fmt.Errorf("loading environment: %w", err)
			}
			if entry == nil {
				return fmt.Errorf("environment '%s' not found", envName)
			}

			for _, kv := range args[1:] {
				key, value, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("invalid format '%s', expected KEY=VALUE", kv)
				}
				entry.SetEnv(key, value)
			}

			if err := mgr.SaveEnvironment(cmd.Context(), envName, entry); err != nil {
				return fmt.Errorf("saving environment: %w", err)
			}

			for _, kv := range args[1:] {
				fmt.Fprintln(os.Stderr, "  "+kv)
			}
			return nil
		},
	}
}

func newStoreGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <env-name> <KEY>",
		Short: "Get a persistent environment variable",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName, key := args[0], args[1]

			mgr, _, err := buildManager(nil)
			if err != nil {
				return err
			}

			entry, err := mgr.GetEnvironment(cmd.Context(), envName)
			if err != nil {
				return fmt.Errorf("loading environment: %w", err)
			}
			if entry == nil {
				return fmt.Errorf("environment '%s' not found", envName)
			}

			value, ok := entry.GetEnv(key)
			if !ok {
				return fmt.Errorf("key '%s' not found", key)
			}

			fmt.Println(value)
			return nil
		},
	}
}

func newStoreListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <env-name>",
		Short: "List all persistent environment variables",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]

			mgr, _, err := buildManager(nil)
			if err != nil {
				return err
			}

			entry, err := mgr.GetEnvironment(cmd.Context(), envName)
			if err != nil {
				return fmt.Errorf("loading environment: %w", err)
			}
			if entry == nil {
				return fmt.Errorf("environment '%s' not found", envName)
			}

			if len(entry.Env) == 0 {
				fmt.Fprintln(os.Stderr, "No environment variables set")
				return nil
			}

			keys := make([]string, 0, len(entry.Env))
			for k := range entry.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				fmt.Printf("%s=%s\n", k, entry.Env[k])
			}
			return nil
		},
	}
}
