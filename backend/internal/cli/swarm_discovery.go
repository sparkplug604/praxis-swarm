package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newSwarmDiscoveryCommand(c *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "discovery", Short: "Read or revise the project's persistent discovery brief"}
	for _, action := range []string{"get", "save"} {
		var input string
		var revision int64
		sub := &cobra.Command{Use: action + " PROJECT", Short: action + " a versioned project brief", Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" || strings.ContainsAny(args[0], "/?#") {
				return usageError{errors.New("a single project ID is required")}
			}
			return nil
		}, RunE: func(cmd *cobra.Command, args []string) error {
			path := "swarm/discovery/" + url.PathEscape(args[0])
			var out json.RawMessage
			var err error
			if action == "save" {
				if input == "" {
					return usageError{errors.New("--input is required")}
				}
				raw, e := readSwarmInput(input)
				if e != nil {
					return usageError{e}
				}
				if !json.Valid(raw) {
					return usageError{errors.New("--input must be valid discovery JSON")}
				}
				err = c.postJSON(cmd.Context(), path, json.RawMessage(raw), &out)
			} else {
				if revision < 0 {
					return usageError{errors.New("--revision cannot be negative")}
				}
				if revision > 0 {
					path += "?revision=" + strconv.FormatInt(revision, 10)
				}
				err = c.getJSON(cmd.Context(), path, &out)
			}
			if err != nil {
				return err
			}
			return writeJSON(c.deps.Out, out)
		}}
		if action == "save" {
			sub.Flags().StringVar(&input, "input", "", "JSON containing expectedRevision and brief")
		} else {
			sub.Flags().Int64Var(&revision, "revision", 0, "Read a retained revision; zero selects latest")
		}
		cmd.AddCommand(sub)
	}
	return cmd
}
