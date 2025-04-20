package app

// Flags stores the program options.
type Flags struct {
	QueryFile         string
	Query             string
	DigRepo           bool
	DigCommits        bool
	RegexFile         string
	ConfigFile        string
	Pages             int
	ResultsOnly       bool
	NoAPIKeys         bool
	NoScoring         bool
	NoFiles           bool
	NoKeywords        bool
	AllResults        bool
	FastMode          bool
	Threads           int
	Debug             bool
	APIDebug          bool
	NoGists           bool
	NoRepos           bool
	ManyResults       bool
	JsonOutput        bool
	Dashboard         bool
	SearchType        string
	OTPCode           string
	TextRegexes       []Rule
	WebSocketURL      string
	EnableProfiling   bool   // Enable pprof profiling
	ProfileAddr       string // Address to serve pprof profiles (host:port)
	GithubAccessToken string // GitHub API token
	InsertKey         string // GitHoundExplore Insert Key
}

var flags Flags

// GetFlags is a singleton that returns the program flags.
func GetFlags() *Flags {
	return &flags
}
