// Package semver implements the subset of semantic versioning the shell needs
// to decide whether an installed module is compatible with this shell.
//
// The subset is deliberate: receipts declare simple conjunctive ranges such as
// ">=0.1.0 <1.0.0". Alternation, caret and tilde shorthands, and build metadata
// ordering are not supported, so an unfamiliar specification fails closed
// instead of being reinterpreted.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semantic version. Build metadata is not retained because
// it never affects compatibility decisions.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

// Parse reads a semantic version, tolerating a leading "v".
func Parse(input string) (Version, error) {
	text := strings.TrimSpace(input)
	text = strings.TrimPrefix(text, "v")
	if text == "" {
		return Version{}, fmt.Errorf("semver: empty version")
	}
	if index := strings.IndexByte(text, '+'); index >= 0 {
		text = text[:index]
	}

	core := text
	prerelease := ""
	if index := strings.IndexByte(text, '-'); index >= 0 {
		core, prerelease = text[:index], text[index+1:]
		if prerelease == "" {
			return Version{}, fmt.Errorf("semver: %q has an empty prerelease", input)
		}
		for _, identifier := range strings.Split(prerelease, ".") {
			if identifier == "" {
				return Version{}, fmt.Errorf("semver: %q has an empty prerelease identifier", input)
			}
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("semver: %q is not major.minor.patch", input)
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || strings.TrimLeft(part, "0123456789") != "" {
			return Version{}, fmt.Errorf("semver: %q has a non-numeric component %q", input, part)
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return Version{}, fmt.Errorf("semver: %q has an unreadable component %q", input, part)
		}
		numbers[index] = value
	}

	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Prerelease: prerelease}, nil
}

// String renders the version without a leading "v".
func (v Version) String() string {
	core := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease == "" {
		return core
	}
	return core + "-" + v.Prerelease
}

// Compare reports whether left sorts before (-1), equal to (0), or after (1)
// right. A prerelease sorts below its own release.
func Compare(left, right Version) int {
	for _, pair := range [][2]int{
		{left.Major, right.Major},
		{left.Minor, right.Minor},
		{left.Patch, right.Patch},
	} {
		if pair[0] != pair[1] {
			return sign(pair[0] - pair[1])
		}
	}
	return comparePrerelease(left.Prerelease, right.Prerelease)
}

func comparePrerelease(left, right string) int {
	switch {
	case left == right:
		return 0
	case left == "":
		return 1
	case right == "":
		return -1
	}

	leftIdentifiers := strings.Split(left, ".")
	rightIdentifiers := strings.Split(right, ".")
	for index := 0; index < len(leftIdentifiers) && index < len(rightIdentifiers); index++ {
		if result := compareIdentifier(leftIdentifiers[index], rightIdentifiers[index]); result != 0 {
			return result
		}
	}
	return sign(len(leftIdentifiers) - len(rightIdentifiers))
}

func compareIdentifier(left, right string) int {
	leftNumber, leftNumeric := numericIdentifier(left)
	rightNumber, rightNumeric := numericIdentifier(right)
	switch {
	case leftNumeric && rightNumeric:
		return sign(leftNumber - rightNumber)
	case leftNumeric:
		return -1
	case rightNumeric:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

func numericIdentifier(identifier string) (int, bool) {
	value, err := strconv.Atoi(identifier)
	if err != nil {
		return 0, false
	}
	return value, true
}

func sign(difference int) int {
	switch {
	case difference < 0:
		return -1
	case difference > 0:
		return 1
	default:
		return 0
	}
}

// Range is a conjunctive version constraint: every comparator must hold.
type Range struct {
	spec        string
	comparators []comparator
}

type comparator struct {
	operator string
	version  Version
}

// ParseRange reads a whitespace-separated conjunction of comparators, such as
// ">=0.1.0 <1.0.0". A bare version is treated as an exact match.
func ParseRange(spec string) (Range, error) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return Range{}, fmt.Errorf("semver: empty range")
	}

	comparators := make([]comparator, 0, len(fields))
	for _, field := range fields {
		operator := ""
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(field, candidate) {
				operator = candidate
				break
			}
		}
		text := strings.TrimPrefix(field, operator)
		if operator == "" {
			operator = "="
		}
		version, err := Parse(text)
		if err != nil {
			return Range{}, fmt.Errorf("semver: range %q has an invalid comparator %q: %w", spec, field, err)
		}
		comparators = append(comparators, comparator{operator: operator, version: version})
	}
	return Range{spec: strings.Join(fields, " "), comparators: comparators}, nil
}

// Contains reports whether the version satisfies every comparator.
func (r Range) Contains(version Version) bool {
	for _, entry := range r.comparators {
		result := Compare(version, entry.version)
		satisfied := false
		switch entry.operator {
		case ">=":
			satisfied = result >= 0
		case "<=":
			satisfied = result <= 0
		case ">":
			satisfied = result > 0
		case "<":
			satisfied = result < 0
		case "=":
			satisfied = result == 0
		}
		if !satisfied {
			return false
		}
	}
	return true
}

// String renders the normalized range specification.
func (r Range) String() string {
	return r.spec
}
