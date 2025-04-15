package app

import (
	"fmt"
	"regexp"
	"strings"
)

type RuleConfig struct {
	Rules []Rule `yaml:"rules,omitempty"`
}

type Rule struct {
	ID             string         `yaml:"id" toml:"id"`
	Pattern        *regexp.Regexp `yaml:"pattern"`
	StringPattern  string         `toml:"regex"`
	Description    string         `yaml:"name" toml:"description"`
	SmartFiltering bool           `yaml:"smart_filtering" toml:"smart_filtering"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Rule
func (r *Rule) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Create a temporary type to avoid infinite recursion
	type RuleAlias Rule

	// Create a temporary struct with all fields
	temp := struct {
		*RuleAlias
		Pattern string `yaml:"pattern"`
		Name    string `yaml:"name"`
		ID      string `yaml:"id"`
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

	// Compile with Go's regexp package
	compiled, err := regexp.Compile(temp.Pattern)
	if err != nil {
		return fmt.Errorf("failed to compile regex '%s': %w", temp.Pattern, err)
	}

	r.Pattern = compiled
	r.StringPattern = temp.Pattern
	r.Description = temp.Name
	r.ID = temp.ID
	return nil
}
