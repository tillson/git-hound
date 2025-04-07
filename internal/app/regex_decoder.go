package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/GRbit/go-pcre"
)

// RegexWrapper wraps PCRE regex objects for compatibility
type RegexWrapper struct {
	RegExp pcre.Regexp
}

// String returns the string representation of the regex pattern
func (rw *RegexWrapper) String() string {
	if rw == nil {
		return "<nil>"
	}
	return "pcre-regex" // PCRE doesn't have a String() method, return a placeholder
}

type RuleConfig struct {
	Rules []Rule `yaml:"rules,omitempty"`
}

type Rule struct {
	ID             string         `yaml:"id" toml:"id"`
	Pattern        *regexp.Regexp `yaml:"pattern"`
	PCREPattern    *RegexWrapper  `yaml:"-"` // For PCRE regexp support
	StringPattern  string         `toml:"regex"`
	Description    string         `yaml:"name" toml:"description"`
	SmartFiltering bool           `yaml:"smart_filtering" toml:"smart_filtering"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Rule
func (r *Rule) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Create a temporary type to avoid infinite recursion
	type RuleAlias Rule

	// Create a temporary struct with string pattern
	temp := struct {
		*RuleAlias
		Pattern string `yaml:"pattern"`
	}{
		RuleAlias: (*RuleAlias)(r),
	}

	// Unmarshal into the temporary struct
	if err := unmarshal(&temp); err != nil {
		return fmt.Errorf("failed to unmarshal rule: %w", err)
	}

	// Skip empty patterns
	if strings.TrimSpace(temp.Pattern) == "" {
		return nil
	}

	// Try to compile with Go's regexp package
	compiled, err := regexp.Compile(temp.Pattern)
	if err != nil {
		// If standard Go regexp fails, try PCRE
		pcrePattern, pcreErr := pcre.Compile(temp.Pattern, 0)
		if pcreErr != nil {
			return fmt.Errorf("failed to compile regex '%s' with both Go regexp and PCRE: %w", temp.Pattern, err)
		}

		// Use PCRE if successful
		r.PCREPattern = &RegexWrapper{RegExp: pcrePattern}
		r.StringPattern = temp.Pattern
		return nil
	}

	// Use Go regexp if successful
	r.Pattern = compiled
	r.StringPattern = temp.Pattern
	return nil
}
