package cmd

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/GRbit/go-pcre"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"

	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/tillson/git-hound/internal/app"

	_ "net/http/pprof"
)

// InitializeFlags initializes GitHound's command line flags.
func InitializeFlags() {
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().SearchType, "search-type", "", "Search interface (`api` or `ui`).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().QueryFile, "query-file", "", "A file containing a list of subdomains (or other queries).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().Query, "query", "", "A query stiing (default: stdin)")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigRepo, "dig-files", false, "Dig through the repo's files to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigCommits, "dig-commits", false, "Dig through commit history to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "rules", "rules/rules-noseyparker", "Path to a list of regexes or a GitLeaks rules folder.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "regex-file", "rules/rules-noseyparker", "Alias for the 'rules' flag.")
	rootCmd.PersistentFlags().MarkHidden("regex-file")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().ConfigFile, "config-file", "", "Supply the path to a config file.")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Pages, "pages", 100, "Maximum pages to search per query")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().GithubRepo, "github-repo", false, "Search in a specific Github Repo only.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ResultsOnly, "results-only", false, "Only print match strings.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoAPIKeys, "no-api-keys", false, "Don't search for generic API keys.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoScoring, "no-scoring", false, "Don't use scoring to filter out false positives.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoFiles, "no-files", false, "Don't search for interesting files.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoKeywords, "no-keywords", false, "Don't search for built-in keywords")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ManyResults, "many-results", false, "Search >100 pages with filtering hack")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().OnlyFiltered, "filtered-only", false, "Only print filtered results (language files)")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().AllResults, "all-results", false, "Print all results, even if they do not contain secrets")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().JsonOutput, "json", false, "Print results in JSON format")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().FastMode, "fast", false, "Skip file grepping and only return search preview")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Threads, "threads", 20, "Threads to dig with")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoGists, "no-gists", false, "Don't search Gists")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoRepos, "no-repos", false, "Don't search repos")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Debug, "debug", false, "Enables verbose debug logging.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().OTPCode, "otp-code", "", "Github account 2FA token used for sign-in. (Only use if you have 2FA enabled on your account via authenticator app)")
}

var rootCmd = &cobra.Command{
	Use:   "githound",
	Short: "GitHound is a pattern-matching, batch-catching secret snatcher.",
	Long:  `GitHound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a robust scoring system.`,
	Run: func(cmd *cobra.Command, args []string) {
		ReadConfig()
		size, err := app.DirSize("/tmp/githound")
		if err == nil && size > 50e+6 {
			fmt.Println("Cleaning up local repo storage...")
			app.ClearRepoStorage()
		}

		var queries []string

		if cmd.Flag("query").Value.String() != "" {
			queries = append(queries, cmd.Flag("query").Value.String())
		}
		if cmd.Flag("query-file").Value.String() != "" {
			for _, query := range app.GetFileLines(app.GetFlags().QueryFile) {
				queries = append(queries, query)
			}

		}
		if !terminal.IsTerminal(0) {
			scanner := getScanner(args)
			for scanner.Scan() {
				bytes := scanner.Bytes()
				str := string(bytes)
				if str != "" {
					queries = append(queries, str)
				}
			}
		}
		if len(queries) == 0 {
			color.Red("[!] No search queries specified. Use flag `--query [query]`, or pipe query into GitHound.")
			os.Exit(1)
			return
		}

		var allRules []app.Rule
		// fmt.Println(app.GetFlags().RegexFile)
		// If rules is a directory, load all rules files in GitLeaks YML format
		if fileInfo, err := os.Stat(app.GetFlags().RegexFile); err == nil && fileInfo.IsDir() {
			files, err := ioutil.ReadDir(app.GetFlags().RegexFile)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			for _, file := range files {
				// if filepath.Ext(file.Name()) == ".yml" || filepath.Ext(file.Name()) == ".yml" {
				filePath := filepath.Join(app.GetFlags().RegexFile, file.Name())
				rules := LoadRegexFile(filePath)
				allRules = append(allRules, rules...)
				// }
			}
			app.GetFlags().TextRegexes = append(app.GetFlags().TextRegexes, allRules...)
		} else {
			// Otherwise, resort to regex list in txt file or legacy TOML files
			rules := LoadRegexFile(app.GetFlags().RegexFile)
			allRules = append(allRules, rules...)
		}
		if len(allRules) == 0 {
			color.Yellow("[!] 0 rules loaded. Using an empty ruleset may result in lousy performance. Consider using one of the rulesets provided with the GitHound installation or available from https://github.com/tillson/git-hound.")
		}

		app.GetFlags().TextRegexes = allRules

		// fmt.Println(app.GetFlags().TextRegexes)

		if app.GetFlags().SearchType == "ui" && viper.GetString("github_username") == "" {
			color.Red("[!] GitHound run in UI mode but github_username not specified in config.yml. Update config.yml or run in API mode (flag: `--search-type api`)")
			os.Exit(1)
		} else if app.GetFlags().SearchType == "api" && viper.GetString("github_access_token") == "" {
			color.Red("[!] GitHound run in API mode but github_access_token not specified in config.yml. Update config.yml or run in UI mode (flag: `--search-type ui`)")
			os.Exit(1)
		}

		if app.GetFlags().SearchType == "ui" {
			app.SearchWithUI(queries)
		} else {
			// fmt.Println(1)
			app.SearchWithAPI(queries)
		}

	},
}

func LoadRegexFile(path string) []app.Rule {
	file, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if err != nil {
		color.Yellow("[!} Error opening rules file %v: %v", app.GetFlags().RegexFile+"", err)
	}
	defer file.Close()

	dec := yaml.NewDecoder(file)
	ruleConfig := app.RuleConfig{}
	err = dec.Decode(&ruleConfig)
	if err != nil {
		_, err := toml.DecodeFile(path, &ruleConfig)

		if err != nil {
			// fmt.Println("Resorting to .txt")
			file, _ := os.Open(path)
			defer file.Close()
			scanner := bufio.NewScanner(file)
			idCount := 1

			for scanner.Scan() {
				line := scanner.Text()
				// fmt.Println(line)
				// Assuming each line is a regex pattern, we create a Rule from it
				compiled, err := pcre.Compile(line, 0)
				if err != nil {
					fmt.Printf("Unable to parse regex `%s` in TXT file.\n", line)
					continue
				}

				// Create a new rule
				rule := app.Rule{
					ID:             fmt.Sprintf("Rule-%d", idCount), // Incremental ID
					Pattern:        app.RegexWrapper{RegExp: compiled},
					StringPattern:  line,                                            // Store the original pattern as StringPattern
					Description:    fmt.Sprintf("Description for Rule-%d", idCount), // Incremental description
					SmartFiltering: false,                                           // Default to false, you can modify if needed
				}

				// Add the rule to the config
				ruleConfig.Rules = append(ruleConfig.Rules, rule)

				idCount++ // Increment the rule ID counter
			}
		} else {
			// Convert StringPattern to Pattern for TOML
			for i, rule := range ruleConfig.Rules {
				if rule.StringPattern != "" {
					compiled, err := pcre.Compile(rule.StringPattern, 0)
					if err != nil {
						// fmt.Println("Unable to parse regex `" + rule.StringPattern + "` in TOML file.")
					}
					ruleConfig.Rules[i].Pattern = app.RegexWrapper{RegExp: compiled}

				}
			}
			// fmt.Println("Parsed as TOML")
		}
	} else {
		// fmt.Println("Parsed as YML")
	}
	// fmt.Println(2)
	return ruleConfig.Rules

}

func getScanner(args []string) *bufio.Scanner {
	if len(args) == 2 {
		if args[0] == "searchKeyword" {
			return bufio.NewScanner(strings.NewReader(args[1]))
		}
	}
	return bufio.NewScanner(os.Stdin)
}

// Execute command
func Execute() {
	InitializeFlags()
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// ReadConfig initializes Viper, the config parser
func ReadConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath("$HOME/.githound")
	viper.AddConfigPath(".")
	if app.GetFlags().ConfigFile != "" {
		viper.SetConfigFile(app.GetFlags().ConfigFile)
	}
	err := viper.ReadInConfig()
	if err != nil {
		if app.GetFlags().ConfigFile != "" {
			color.Red("[!] Config file '" + app.GetFlags().ConfigFile + "' was not found. Please specify a correct config path with `--config-file`.")

		} else {
			color.Red("[!] config.yml was not found. Please ensure config.yml exists in current working directory or $HOME/.githound/, or use flag `--config [config_path]`.")
		}
		os.Exit(1)
		return
	}
}
