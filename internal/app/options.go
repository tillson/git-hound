package app

// Flags stores the program options.
type Flags struct {
	SubdomainFile string
	NoAPIKeys     bool
	NoKeywords    bool
	Dig           bool
	Threads       int
	Pages         int
	RegexFile     string
	LanguageFile  string
	Debug         bool
	GistOnly      bool
}

var flags Flags

// GetFlags is a singleton that returns the program flags.
func GetFlags() *Flags {
	return &flags
}
