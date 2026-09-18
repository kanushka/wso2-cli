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

package wizard

import (
	"errors"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// formPrompter asks with a huh form drawn on a terminal, in theme.
type formPrompter struct {
	in  io.Reader
	out io.Writer
}

func (p formPrompter) Select(title string, options []Option, fallback int) (int, error) {
	choices := make([]huh.Option[int], len(options))
	for index, option := range options {
		choices[index] = huh.NewOption(option.Label, index)
	}
	picked := fallback
	field := huh.NewSelect[int]().
		Title(title).
		Options(choices...).
		Validate(func(index int) error {
			if note := options[index].Unavailable; note != "" {
				return Hint(note)
			}
			return nil
		}).
		Value(&picked)
	if err := p.run(field); err != nil {
		return 0, err
	}
	return picked, nil
}

func (p formPrompter) Input(title, fallback string, validate func(string) error) (string, error) {
	var answer string
	field := huh.NewInput().
		Title(title).
		Placeholder(fallback).
		Validate(func(typed string) error {
			return check(validate, orDefault(typed, fallback))
		}).
		Value(&answer)
	if err := p.run(field); err != nil {
		return "", err
	}
	return orDefault(answer, fallback), nil
}

// Confirm is a select of Yes and No rather than huh's confirm buttons, so a
// yes/no question is answered the same way as every other choice. Filtering
// two options helps nobody, so its key and hint are left out: the binding has
// no keys at all, because huh enables the filter key again whenever it clears
// the filter.
func (p formPrompter) Confirm(title string, fallback bool) (bool, error) {
	answer := fallback
	field := huh.NewSelect[bool]().
		Title(title).
		Options(huh.NewOption("Yes", true), huh.NewOption("No", false)).
		Value(&answer)
	keymap := huh.NewDefaultKeyMap()
	keymap.Select.Filter = key.NewBinding()
	if err := p.runWith(field, keymap); err != nil {
		return false, err
	}
	return answer, nil
}

// theme is huh's base theme, which sets no colors of its own, with the bar
// beside a question drawn thin and in the terminal's dim gray (ANSI 8), so it
// frames the question without outshining it. The terminal's palette and
// NO_COLOR decide the rest, as for all the shell's output.
func theme(isDark bool) *huh.Styles {
	styles := huh.ThemeBase(isDark)
	styles.Focused.Base = styles.Focused.Base.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8"))
	styles.Focused.Card = styles.Focused.Base
	return styles
}

func (p formPrompter) run(field huh.Field) error {
	return p.runWith(field, huh.NewDefaultKeyMap())
}

func (p formPrompter) runWith(field huh.Field, keymap *huh.KeyMap) (err error) {
	// huh dereferences a nil model when its program ends on end of input
	// before the form is answered. A terminal that closed is no answer, not a
	// crash.
	defer func() {
		if recover() != nil {
			err = ErrNoAnswer
		}
	}()
	err = huh.NewForm(huh.NewGroup(field)).
		WithInput(p.in).
		WithOutput(p.out).
		WithWidth(p.width()).
		WithTheme(huh.ThemeFunc(theme)).
		WithKeyMap(keymap).
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// Drawable reports whether out is a terminal that reports a size a form can
// be drawn in. A pseudo-terminal nobody sized reports zero rows, and a form
// drawn there shows nothing, so such a terminal gets line prompts.
func Drawable(out io.Writer) bool {
	file, ok := out.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	width, height, err := term.GetSize(file.Fd())
	return err == nil && width > 0 && height > 0
}

// width is the terminal's width, or a usual one when it cannot be read. huh
// needs one before its first frame: without it, a field with a placeholder
// fails to draw.
func (p formPrompter) width() int {
	if file, ok := p.out.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(file.Fd()); err == nil && width > 0 {
			return width
		}
	}
	return defaultWidth
}

const defaultWidth = 80

// orDefault is typed trimmed, or fallback when nothing was typed.
func orDefault(typed, fallback string) string {
	if trimmed := strings.TrimSpace(typed); trimmed != "" {
		return trimmed
	}
	return fallback
}
