package cmd

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/spf13/cobra"
	"github.com/tillson/git-hound/internal/app"
)

// InitializeFlags initializes GitHound's command line flags.
func InitializeFlags() {
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().SubdomainFile, "subdomain-file", "", "A file containing a list of subdomains (or other queries).")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Dig, "dig", false, "Dig through commit history to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "regex-file", "", "Supply your own list of regexes.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().LanguageFile, "language-file", "", "Supply your own list of languages to search (java, python).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().ConfigFile, "config-file", "", "Supply the path to a config file.")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Pages, "pages", 100, "Maximum pages to search per query")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ResultsOnly, "results-only", false, "Only print match strings.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoAPIKeys, "no-api-keys", false, "Don't search for generic API keys.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoScoring, "no-scoring", false, "Don't use scoring to filter out false positives.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoFiles, "no-files", false, "Don't search for interesting files.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoKeywords, "no-keywords", false, "Don't search for built-in keywords")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ManyResults, "many-results", false, "Search >100 pages with filtering hack")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().OnlyFiltered, "filtered-only", false, "Only print filtered results (language files)")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Threads, "threads", 20, "Threads to dig with (default 10).")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoGists, "no-gists", false, "Don't search Gists")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoRepos, "no-repos", false, "Don't search repos")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Debug, "debug", false, "Enables verbose debug logging.")
}

var rootCmd = &cobra.Command{
	Use:   "githound",
	Short: "GitHound is a pattern-matching, batch-catching secret snatcher.",
	Long:  `GitHound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a robust scoring system.`,
	Run: func(cmd *cobra.Command, args []string) {
		ReadConfig()
		size, err := app.DirSize("/tmp/githound")
		if err == nil && size > 50e+6 {
			app.ClearRepoStorage()
		}

		var queries []string
		if cmd.Flag("subdomain-file").Value.String() != "" {
			for _, query := range app.GetFileLines(app.GetFlags().SubdomainFile) {
				queries = append(queries, query)
			}
		} else {
			if !terminal.IsTerminal(0) {
				b, _ := ioutil.ReadAll(os.Stdin)
				for _, line := range strings.Split(string(b), "\n") {
					if line != "" {
						queries = append(queries, line)
					}
				}
			} else {
				color.Red("[!] No search queries specified.")
				return
			}
		}

		client, err := app.LoginToGitHub(app.GitHubCredentials{
			Username: viper.GetString("github_username"),
			Password: viper.GetString("github_password"),
		})
		if err != nil {
			color.Red("[!] Unable to login to GitHub.")
			log.Fatal(err)
		}
		if !app.GetFlags().ResultsOnly {
			color.Cyan("[*] Logged into GitHub as " + viper.GetString("github_username"))
		}

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
		if !app.GetFlags().ResultsOnly {
			color.Green("Finished.")
		}
	},
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
		viper.AddConfigPath(app.GetFlags().ConfigFile)
	}
	err := viper.ReadInConfig()
	if err != nil {
		color.Red("[!] config.yml was not found.")
		return
	}
}
