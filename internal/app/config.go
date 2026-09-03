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

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The way back from each subcommand's usage refusals.
const (
	configListUsage  = "Run wso2 config list [--output table|json]."
	configGetUsage   = "Run wso2 config get <key> [--output table|json]."
	configSetUsage   = "Run wso2 config set <key> <value> [--output table|json]."
	configUnsetUsage = "Run wso2 config unset <key> [--output table|json]."
)

// configRecovery is what a refusal from the config command itself, rather
// than one of its subcommands, points a user at.
const configRecovery = "Run wso2 config list to show every preference, wso2 config get <key> to " +
	"show one, wso2 config set <key> <value> to change one, or wso2 config unset <key> to " +
	"restore one to its built-in default."

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
		Long:                  configRecovery,
		DisableFlagsInUseLine: true,
		// A RunE is declared here so that a mistyped subcommand is refused:
		// Cobra validates a non-leaf command's arguments only when the command
		// is Runnable, so without this, wso2 config bogus prints help and
		// exits 0 — a typo reported as success to whatever script ran it.
		// All five families agree on this, since #133.
		//
		// A bare wso2 config is the other arm, and is deliberately not a
		// refusal. See helpForBareFamily.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 config subcommand", args[0])).
				WithRecovery(configRecovery)
		},
	}
	// Preferences are machine-local, not context-scoped: a saved output mode or
	// catalog origin applies to every context on this machine, so --context has
	// nothing to select here. --output is declared on the family so every
	// subcommand inherits it.
	declareOutputFlag(command.PersistentFlags())
	command.AddCommand(s.configListCommand(), s.configGetCommand(), s.configSetCommand(),
		s.configUnsetCommand())
	return command
}

func (s Shell) configListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show every shell preference in the closed key set.",
		Args:  noArguments(configListUsage),
		RunE: func(command *cobra.Command, args []string) error {
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
			return s.configSet(command, args[0], args[1])
		},
	}
}

func (s Shell) configUnsetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove one shell preference, so its built-in default governs again.",
		Args:  exactlyOneArgument("a preference key", configUnsetUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.configUnset(command, args[0])
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

// configUnset removes one preference, so the built-in default governs again,
// refusing a key outside the closed set.
//
// Unsetting a key that was never set succeeds: the caller asked for the state
// the machine is already in, and refusing would make a recovery script check
// before it clears (the config family already treats "unset" as a fact, not a
// failure — wso2 config get reports it with exit 0). The two cases render the
// same rows and differ only in the sentence above them, so a script parses one
// shape either way. Nothing is written in the already-unset case; a command
// that changes nothing should not create a preferences document to say so.
func (s Shell) configUnset(command *cobra.Command, rawKey string) error {
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
	_, wasSet := document.Get(key)
	if wasSet {
		s.log.Debug("unsetting a shell preference",
			"key", string(key), "document", preferences.Path(root))
		writeErr := preferences.Update(root, func(document preferences.Document) (preferences.Document, error) {
			document.SchemaVersion = preferences.SchemaVersion
			return document.Unset(key)
		})
		if writeErr != nil {
			return writeErr
		}
	}

	// Same reasoning as configSet's mode correction, in the other direction:
	// the confirmation for unsetting the output preference renders in the mode
	// that governs after the write — the built-in table default — unless an
	// explicit --output asked for something more specific.
	if key == preferences.KeyOutputMode && wasSet {
		if flag := shellFlag(command, outputFlag); flag == nil || !flag.Changed {
			mode = output.ModeTable
		}
	}

	// The Value row carries what governs now — the built-in default — rather
	// than the empty string wso2 config get would show for the stored
	// preference: this command's one job is reverting to a value, and a report
	// that hid it would leave the user no better informed than before F3.
	// Set is false either way; a caller must read it as "not configured", not
	// "configured to this value".
	entry := configEntry{Key: string(key), Value: builtInDefault(key), Set: false}
	if mode == output.ModeJSON {
		return encodeConfigJSON(s.Streams.Out, entry)
	}
	sentence := fmt.Sprintf("\nUnset %q. The built-in default %q governs again.\n", key, entry.Value)
	if !wasSet {
		sentence = fmt.Sprintf("\n%q was not set. The built-in default %q already governs.\n", key, entry.Value)
	}
	if _, err := fmt.Fprint(s.Streams.Out, sentence); err != nil {
		return err
	}
	return output.Fields(s.Streams.Out, entry.fields())
}

// builtInDefault names what governs a key when no preference is set. These
// literals belong to the consumers — internal/output's default mode and
// internal/catalog's published origin — and are read from them rather than
// restated, so this report cannot drift from what the shell actually does.
// (WSO2_CLI_CATALOG_ORIGIN can still outrank the catalog default, but only
// the test harness sets it, and naming it here would confuse everyone else.)
func builtInDefault(key preferences.Key) string {
	switch key {
	case preferences.KeyOutputMode:
		return string(output.ModeTable)
	case preferences.KeyCatalogOrigin:
		return catalog.DefaultOrigin
	default:
		return ""
	}
}

// The results this family reports. Both renderings are driven by the same
// value and publish no schema discriminator, for the reasons context.go's
// equivalent comment gives.
type (
	// configEntry is what wso2 config get, wso2 config set, and
	// wso2 config unset report.
	configEntry struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		// Set reports whether this key is currently configured. In get and
		// list, a key that is not carries an empty Value, which a caller must
		// not mistake for the key being configured to the empty string: no key
		// this shell defines accepts one (Document.Set refuses it for both).
		// unset is the one report where an unset key carries a non-empty
		// Value — the built-in default now governing — see configUnset.
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
