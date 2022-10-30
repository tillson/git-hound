package app

import (
	"regexp"
)

type (
	config struct {
		Rules []regexrule
	}
	regexrule struct {
		Regex          regexwrapper
		Name           string `toml:"description"`
		SmartFiltering bool   `toml:"smart_filtering"`
	}
	regexwrapper struct {
		RegExp *regexp.Regexp
	}
)

func (r *regexwrapper) UnmarshalText(text []byte) error {
	var err error
	r.RegExp, err = regexp.Compile(string(text))
	// r.Regex, err = mail.ParseAddress(string(text))
	return err
}
