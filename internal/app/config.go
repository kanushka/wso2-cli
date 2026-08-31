// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	configListUsage = "Run wso2 config list [--output table|json]."
	configGetUsage  = "Run wso2 config get <key> [--output table|json]."
	configSetUsage  = "Run wso2 config set <key> <value> [--output table|json]."
)

// configRecovery is what a refusal from the config command itself, rather
// than one of its subcommands, points a user at.
const configRecovery = "Run wso2 config list to show every preference, wso2 config get <key> to " +
	"show one, or wso2 config set <key> <value> to change one."

// configCommand builds the wso2 config tree.
//
// It reads and writes the shell's non-secret, machine-local preferences: the
// default output mode and the catalog origin override. The key set is closed
// (R8, #112): naming a key here means teaching internal/preferences about it
// first, which is what stops this family from becoming a place arbitrary
// state accumulates. A colour preference was cut from the closed set before
// shipping (fix round 1, F3): output.ColorEnabled has zero production
// callers today, so it is the obvious first key to add once something in
// this shell actually renders in colour.
func (s Shell) configCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "config <subcommand>",
		Short:                 "Show and change shell preferences.",
		Long:                  "Subcommands: list, get, set.",
		DisableFlagsInUseLine: true,
		// A RunE is declared here for the reason org.go's states: Cobra only
		// validates a non-leaf command's arguments when it is Runnable, so
		// without this, wso2 config bogus prints help and exits 0 — a mistyped
		// subcommand reported as success to whatever script ran it. This
		// branch and org's must agree, because this branch introduces both.
		// The pre-existing context, identity and module families still print
		// help and exit 0; turning that into a refusal is a user-visible
		// change to shipped commands and is filed as #133 rather than made
		// here.
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			if len(args) == 0 {
				return problem.New(problem.CategoryUsage, "shell.missing_argument",
					"wso2 config needs a subcommand").
					WithRecovery(configRecovery)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 config subcommand", args[0])).
				WithRecovery(configRecovery)
		},
	}
	command.AddCommand(s.configListCommand(), s.configGetCommand(), s.configSetCommand())
	return command
}

func (s Shell) configListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show every shell preference in the closed key set.",
		Args:  noArguments(configListUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.configList(command)
		},
	}
}

func (s Shell) configGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Show one shell preference.",
		Args:  exactlyOneArgument("a preference key", configGetUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.configGet(command, args[0])
		},
	}
}

func (s Shell) configSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change one shell preference.",
		Args:  exactlyTwoArguments("a preference key and a value", configSetUsage),
		RunE: func(command *cobra.Command, args []string) error {
			if err := refuseUnusableShellFlags(command); err != nil {
				return err
			}
			return s.configSet(command, args[0], args[1])
		},
	}
}

// configList shows every key in the closed set, whether or not it is
// currently configured.
func (s Shell) configList(command *cobra.Command) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// The diagnostic for a preferences document this shell could not read is
	// already written once per invocation by dispatch (app.go), which runs
	// before the Cobra and product-namespace paths fork and so before any
	// command body. This second Load, like every other consumer's, discards
	// its own diagnostic rather than repeating it.
	document, _ := preferences.Load(root)

	listing := configListing{Entries: make([]configEntry, 0, len(preferences.Keys()))}
	for _, key := range preferences.Keys() {
		value, set := document.Get(key)
		listing.Entries = append(listing.Entries, configEntry{Key: string(key), Value: value, Set: set})
	}
	if mode == output.ModeJSON {
		return encodeConfigJSON(s.Streams.Out, listing)
	}
	table := output.NewTable("key", "value", "set")
	for _, entry := range listing.Entries {
		table.Append(entry.Key, entry.Value, yesNo(entry.Set))
	}
	return table.Render(s.Streams.Out)
}

// configGet shows one preference, refusing a key outside the closed set.
func (s Shell) configGet(command *cobra.Command, rawKey string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	key, ok := preferences.ParseKey(rawKey)
	if !ok {
		return preferences.UnknownKey(rawKey)
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	document, _ := preferences.Load(root)
	value, set := document.Get(key)
	entry := configEntry{Key: string(key), Value: value, Set: set}
	if mode == output.ModeJSON {
		return encodeConfigJSON(s.Streams.Out, entry)
	}
	return output.Fields(s.Streams.Out, entry.fields())
}

// configSet writes one preference, refusing a key outside the closed set or a
// value the key does not accept.
//
// The write goes through preferences.Update, which holds the document lock
// across the whole read-modify-write, so the other two keys a previous
// wso2 config set wrote are preserved rather than clobbered.
func (s Shell) configSet(command *cobra.Command, rawKey, rawValue string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	key, ok := preferences.ParseKey(rawKey)
	if !ok {
		return preferences.UnknownKey(rawKey)
	}
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	// Every value here came from an argument the user typed, so none of it is
	// credential material: the closed key set (R8) never names one, and this
	// is already in their shell history.
	s.log.Debug("setting a shell preference",
		"key", string(key), "value", rawValue, "document", preferences.Path(root))

	writeErr := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
		document.SchemaVersion = preferences.SchemaVersion
		return document.Set(key, rawValue)
	})
	if writeErr != nil {
		return writeErr
	}

	// The confirmation is rendered in the mode just written, not the one
	// being replaced. shellOutputMode resolved mode before the write, so
	// wso2 config set output json used to answer with a table and
	// wso2 config set output table with JSON — internally consistent, and it
	// reads as a bug every time, because the one thing the command reports is
	// the setting it has just changed. An explicit --output still wins: it is
	// the more specific source, and a caller who asked for JSON is parsing
	// this. The value parses because document.Set has already validated it.
	if key == preferences.KeyOutputMode {
		if flag := shellFlag(command, outputFlag); flag == nil || !flag.Changed {
			if written, ok := output.ParseMode(rawValue); ok {
				mode = written
			}
		}
	}

	entry := configEntry{Key: string(key), Value: rawValue, Set: true}
	if mode == output.ModeJSON {
		return encodeConfigJSON(s.Streams.Out, entry)
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "\nSet %q to %q.\n", key, rawValue); err != nil {
		return err
	}
	return output.Fields(s.Streams.Out, entry.fields())
}

// The results this family reports. Both renderings are driven by the same
// value and publish no schema discriminator, for the reasons context.go's
// equivalent comment gives.
type (
	// configEntry is what wso2 config get and wso2 config set report.
	configEntry struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		// Set reports whether this key is currently configured. A key that is
		// not carries an empty Value, which a caller must not mistake for the
		// key being configured to the empty string: no key this shell defines
		// accepts one (Document.Set refuses it for both).
		Set bool `json:"set"`
	}

	// configListing is what wso2 config list reports.
	configListing struct {
		Entries []configEntry `json:"entries"`
	}
)

func (c configEntry) fields() [][2]string {
	return [][2]string{
		{"Key", c.Key},
		{"Value", c.Value},
		{"Set", yesNo(c.Set)},
	}
}

// encodeConfigJSON writes one result as an indented JSON document.
func encodeConfigJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("app: cannot encode the config result: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}
