package app

// Flags stores the program options.
type Flags struct {
	QueryFile    string
	Query        string
	DigRepo      bool
	DigCommits   bool
	RegexFile    string
	ConfigFile   string
	Pages        int
	GithubRepo   bool
	ResultsOnly  bool
	NoAPIKeys    bool
	NoScoring    bool
	NoFiles      bool
	NoKeywords   bool
	OnlyFiltered bool
	AllResults   bool
	FastMode     bool
	Threads      int
	Debug        bool
	NoGists      bool
	NoRepos      bool
	ManyResults  bool
	JsonOutput   bool
	Dashboard    bool
	SearchType   string
	OTPCode      string
	TextRegexes  []Rule
	WebSocketURL string
}

var flags Flags

// GetFlags is a singleton that returns the program flags.
func GetFlags() *Flags {
	return &flags
}
