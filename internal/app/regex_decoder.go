package app

import (
	"fmt"

	"github.com/GRbit/go-pcre"
)

type RuleConfig struct {
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	ID             string       `yaml:"id"`
	Pattern        regexwrapper `yaml:"pattern"`
	Description    string       `yaml:"name"`
	SmartFiltering bool         `yaml:"smart_filtering"`
}

type regexwrapper struct {
	RegExp pcre.Regexp
}

func (r *regexwrapper) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var patternText string
	// First, unmarshal the text as a string
	if err := unmarshal(&patternText); err != nil {
		return fmt.Errorf("failed to unmarshal pattern text: %w", err)
	}

	// Compile the regular expression
	compiledRegex, err := pcre.Compile(patternText, 0)
	if err != nil {
		return fmt.Errorf("failed to compile regex '%s': %w", patternText, err)
	}

	// Assign the compiled regex to the RegExp field
	r.RegExp = compiledRegex
	return nil
}
