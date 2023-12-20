package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/fatih/color"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"

	"strings"

	"github.com/spf13/cobra"
	"github.com/tillson/git-hound/internal/app"
)

// InitializeFlags initializes GitHound's command line flags.
func InitializeFlags() {
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().SearchType, "search-type", "", "Search interface (`api` or `ui`).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().QueryFile, "query-file", "", "A file containing a list of subdomains (or other queries).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().Query, "query", "", "A query stiing (default: stdin)")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigRepo, "dig-files", false, "Dig through the repo's files to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigCommits, "dig-commits", false, "Dig through commit history to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "regex-file", "rules.toml", "Path to a list of regexes.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().LanguageFile, "language-file", "", "Supply your own list of languages to search (java, python).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().ConfigFile, "config-file", "", "Supply the path to a config file.")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Pages, "pages", 100, "Maximum pages to search per query")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().GithubRepo, "github-repo", false, "Search in a specific Github Repo only.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ResultsOnly, "results-only", false, "Only print match strings.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoAPIKeys, "no-api-keys", false, "Don't search for generic API keys.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoScoring, "no-scoring", false, "Don't use scoring to filter out false positives.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().LegacySearch, "legacy", false, "Use the legacy search method.")
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

		if app.GetFlags().SearchType == "ui" && viper.GetString("github_username") == "" {
			color.Red("[!] GitHound run in UI mode but github_username not specified in config.yml. Update config.yml or run in API mode (flag: `--search-type api`)")
			os.Exit(1)
		} else if app.GetFlags().SearchType == "api" && viper.GetString("github_access_token") == "" {
			color.Red("[!] GitHound run in API mode but github_access_token not specified in config.yml. Update config.yml or run in UI mode (flag: `--search-type ui`)")
			os.Exit(1)
		}
		client, err := app.LoginToGitHub(app.GitHubCredentials{
			Username: viper.GetString("github_username"),
			Password: viper.GetString("github_password"),
			OTP:      viper.GetString("github_totp_seed"),
		})

		if err != nil {
			fmt.Println(err)
			color.Red("[!] Unable to login to GitHub. Please check your username/password credentials.")
			os.Exit(1)
		}
		if !app.GetFlags().ResultsOnly && !app.GetFlags().JsonOutput {
			color.Cyan("[*] Logged into GitHub as " + viper.GetString("github_username"))
		}
		toml.DecodeFile(app.GetFlags().RegexFile, &app.GetFlags().TextRegexes)
		for _, query := range queries {
			_, err = app.Search(query, client)
			if err != nil {
				color.Red("[!] Unable to collect search results for query '" + query + "'.")
				break
			}
		}
		size, err = app.DirSize("/tmp/githound")
		if err == nil && size > 50e+6 {
			app.ClearRepoStorage()
		}
		if !app.GetFlags().ResultsOnly && !app.GetFlags().JsonOutput {
			color.Green("Finished.")
		}

		app.SearchWaitGroup.Wait()
	},
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
			color.Red("[!] '" + app.GetFlags().ConfigFile + "' was not found. Please check the file path and try again.")

		} else {
			color.Red("[!] config.yml was not found. Please ensure config.yml exists in current working directory or $HOME/.githound/, or use flag `--config [config_path]`.")
		}
		os.Exit(1)
		return
	}
}
