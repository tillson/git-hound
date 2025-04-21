package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

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
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().Query, "query", "", "A query string (default: stdin)")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().SearchID, "search-id", "", "Search ID for dashboard mode (skips start_search step)")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigRepo, "dig-files", false, "Dig through the repo's files to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().DigCommits, "dig-commits", false, "Dig through commit history to find more secrets (CPU intensive).")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "rules", "rules/", "Path to a list of regexes or a GitLeaks rules folder.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().RegexFile, "regex-file", "rules/", "Alias for the 'rules' flag.")
	rootCmd.PersistentFlags().MarkHidden("regex-file")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().ConfigFile, "config-file", "", "Supply the path to a config file.")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Pages, "pages", 100, "Maximum pages to search per query")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ResultsOnly, "results-only", false, "Only print match strings.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoAPIKeys, "no-api-keys", false, "Don't search for generic API keys.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoScoring, "no-scoring", false, "Don't use scoring to filter out false positives.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoFiles, "no-files", false, "Don't search for interesting files.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoKeywords, "no-keywords", false, "Don't search for built-in keywords")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().ManyResults, "many-results", false, "Search >100 pages with filtering hack")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().AllResults, "all-results", false, "Print all results, even if they do not contain secrets")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().JsonOutput, "json", false, "Print results in JSON format")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().FastMode, "fast", false, "Skip file grepping and only return search preview")
	rootCmd.PersistentFlags().IntVar(&app.GetFlags().Threads, "threads", 20, "Threads to dig with")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoGists, "no-gists", false, "Don't search Gists")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().NoRepos, "no-repos", false, "Don't search repos")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Debug, "debug", false, "Enables verbose debug logging.")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().APIDebug, "api-debug", false, "Prints details about GitHub API requests and counts them.")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().OTPCode, "otp-code", "", "Github account 2FA token used for sign-in. (Only use if you have 2FA enabled on your account via authenticator app)")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Dashboard, "dashboard", false, "Stream results to GitHoundExplore.com")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().EnableProfiling, "profile", false, "Enable pprof profiling on localhost:6060")
	rootCmd.PersistentFlags().StringVar(&app.GetFlags().ProfileAddr, "profile-addr", "localhost:6060", "Address to serve pprof profiles")
	rootCmd.PersistentFlags().BoolVar(&app.GetFlags().Trufflehog, "trufflehog", false, "Ingest trufflehog output without scanning")
}

var rootCmd = &cobra.Command{
	Use:   "githound",
	Short: "GitHound is a pattern-matching, batch-catching secret snatcher.",
	Long:  `GitHound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a robust scoring system.`,
	Run: func(cmd *cobra.Command, args []string) {
		ReadConfig()

		// Start pprof server if profiling is enabled
		if app.GetFlags().EnableProfiling {
			StartPprofServer()
			color.Cyan("[*] pprof profiling server started at %s", app.GetFlags().ProfileAddr)
			color.Cyan("[*] Visit http://%s/debug/pprof/ in your browser", app.GetFlags().ProfileAddr)
			color.Cyan("[*] Run 'go tool pprof http://%s/debug/pprof/profile' for CPU profiling", app.GetFlags().ProfileAddr)
			color.Cyan("[*] Run 'go tool pprof http://%s/debug/pprof/heap' for memory profiling", app.GetFlags().ProfileAddr)
		}

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
		if !terminal.IsTerminal(0) && !app.GetFlags().Trufflehog {
			scanner := getScanner(args)
			for scanner.Scan() {
				bytes := scanner.Bytes()
				str := string(bytes)
				if str != "" {
					queries = append(queries, str)
				}
			}
		}
		// fmt.Println(queries)

		// fmt.Println(123)
		// Handle dashboard mode

		if app.GetFlags().Trufflehog {
			app.GetFlags().Dashboard = true
		}

		if app.GetFlags().Dashboard {
			// First try to read the insert key from file
			homeDir, err := os.UserHomeDir()
			if err != nil {
				color.Red("Error getting home directory: %v", err)
				os.Exit(1)
			}
			tokenFilePath := filepath.Join(homeDir, ".githound", "insert_token.txt")

			// If insert key is not already set, try to read it from file
			if app.GetFlags().InsertKey == "" {
				if _, err := os.Stat(tokenFilePath); err == nil {
					// Token file exists, read it
					tokenBytes, err := ioutil.ReadFile(tokenFilePath)
					if err == nil {
						app.GetFlags().InsertKey = strings.TrimSpace(string(tokenBytes))
					}
				}
			}

			// Start WebSocket connection
			app.StartWebSocket(app.GetFlags().WebSocketURL)

			// If we don't have an insert key, wait for verification
			if app.GetFlags().InsertKey == "" {
				color.Cyan("[*] Waiting for account verification...")
				// Wait for authentication to complete
				select {
				case authenticated := <-app.WsAuthenticated:
					if !authenticated {
						color.Red("[!] Account verification failed")
						os.Exit(1)
					}
					color.Green("[+] Account verified successfully")
				case <-time.After(5 * time.Minute):
					color.Red("[!] Account verification timed out after 5 minutes")
					os.Exit(1)
				}
			} else {
				color.Green("[+] Dashboard mode enabled with existing Insert Key")
			}
		}

		// Handle trufflehog mode
		if app.GetFlags().Trufflehog {
			// Automatically enable dashboard mode when trufflehog is enabled
			app.GetFlags().Dashboard = true

			if app.GetFlags().InsertKey == "" {
				color.Red("[!] Trufflehog mode requires an Insert Key to be set")
				os.Exit(1)
			}

			// Initialize WebSocket connection
			app.StartWebSocket(app.GetFlags().WebSocketURL)

			// Wait for authentication to complete
			select {
			case authenticated := <-app.WsAuthenticated:
				if !authenticated {
					color.Red("WebSocket authentication failed")
					return
				}
			case <-time.After(5 * time.Minute):
				color.Red("[!] WebSocket authentication timed out after 5 minutes")
				os.Exit(1)
			}

			// Start a new search session
			app.BrokerSearchCreation("TruffleHog Search")

			color.Cyan("[*] Waiting for trufflehog output from stdin...")
			color.Cyan("[*] Example usage: trufflehog git <repo-url> --json | go run main.go --trufflehog --dashboard")

			// Start a goroutine to read from stdin
			go func() {
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						continue
					}

					if app.GetFlags().Debug {
						color.Cyan("[*] Received line: %s", line)
					}

					// Parse trufflehog JSON output
					var result map[string]interface{}
					if err := json.Unmarshal([]byte(line), &result); err != nil {
						if app.GetFlags().Debug {
							color.Yellow("[!] Failed to parse trufflehog output: %v", err)
						}
						continue
					}

					// Add insert key and search ID to the message
					result["insertToken"] = app.GetFlags().InsertKey
					result["event"] = "trufflehog_result"
					if app.GetFlags().SearchID != "" {
						result["searchID"] = app.GetFlags().SearchID
					}

					// Convert back to JSON
					jsonData, err := json.Marshal(result)
					if err != nil {
						if app.GetFlags().Debug {
							color.Yellow("[!] Failed to marshal trufflehog output: %v", err)
						}
						continue
					}

					// Send to WebSocket
					app.SendToWebSocket(string(jsonData))
					if app.GetFlags().Debug {
						color.Green("[+] Sent message to WebSocket")
					}
				}

				if err := scanner.Err(); err != nil {
					color.Red("[!] Error reading trufflehog output: %v", err)
					os.Exit(1)
				}

				// When scanner.Scan() returns false and there's no error, stdin is closed
				color.Green("[+] Finished processing trufflehog output")
				os.Exit(0)
			}()

			// Wait indefinitely for input
			select {}
		}

		if len(queries) == 0 {
			if app.GetFlags().Dashboard {
				color.Red("[!] No search queries specified.")
				color.Blue("[*] Please choose an option:")
				color.Blue("1) Run a GitHub code search secret scan with GitHound")
				color.Blue("2) Upload results from TruffleHog into the dashboard")
				fmt.Print("Enter your choice (1 or 2): ")

				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					choice := scanner.Text()
					if choice == "1" {
						color.Blue("[*] Please enter your search query:")
						for scanner.Scan() {
							query := scanner.Text()
							if query != "" {
								queries = append(queries, query)
								break
							}
						}
						fmt.Println("In the future, you can run this command with the query specified:")
						fmt.Println("echo \"your_query\" | githound --dashboard")
						break
					} else if choice == "2" {
						color.Cyan("\nTo upload TruffleHog results to the dashboard:")
						color.Cyan("1. Run TruffleHog with the --json flag")
						color.Cyan("2. Pipe the output into GitHound with the --trufflehog flag")
						color.Cyan("\nExample:")
						color.Cyan("trufflehog git <repo-url> --json | githound --dashboard --trufflehog")
						color.Cyan("\nPlease re-run the command with the --trufflehog flag.")
						os.Exit(0)
					} else {
						color.Red("[!] Invalid choice. Please enter 1 or 2:")
						continue
					}
				}
			} else {
				color.Red("[!] No search queries specified. Use flag `--query [query]`, or pipe query into GitHound.")
				os.Exit(1)
				return
			}
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
				if filepath.Ext(file.Name()) == ".yml" || filepath.Ext(file.Name()) == ".yml" {
					filePath := filepath.Join(app.GetFlags().RegexFile, file.Name())
					rules := LoadRegexFile(filePath)
					allRules = append(allRules, rules...)
				}
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

		// Print API request summary if enabled
		if app.GetFlags().APIDebug {
			app.PrintAPIRequestSummary()
		}
	},
}

func LoadRegexFile(path string) []app.Rule {
	// Skip processing if the file is named LICENSE
	baseName := filepath.Base(path)
	if baseName == "LICENSE" {
		return nil
	}

	file, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if err != nil {
		color.Yellow("[!] Error opening rules file %v: %v", app.GetFlags().RegexFile+"", err)
		return nil
	}
	defer file.Close()

	// Check file extension
	ext := strings.ToLower(filepath.Ext(path))
	isYamlFile := ext == ".yml" || ext == ".yaml"

	// For YAML files, only try YAML parsing
	if isYamlFile {
		dec := yaml.NewDecoder(file)
		ruleConfig := app.RuleConfig{}
		err = dec.Decode(&ruleConfig)
		if err != nil {
			color.Yellow("[!] Error parsing YAML file %v: %v", path, err)
			return nil
		}

		if len(ruleConfig.Rules) > 0 && app.GetFlags().Debug {
			color.Green("[+] Loaded %d regex rules from %s", len(ruleConfig.Rules), path)
		}

		return ruleConfig.Rules
	}

	// For non-YAML files, try YAML first, then TOML, then line-by-line
	dec := yaml.NewDecoder(file)
	ruleConfig := app.RuleConfig{}
	err = dec.Decode(&ruleConfig)
	if err != nil {
		_, err := toml.DecodeFile(path, &ruleConfig)

		if err != nil {
			// Try to parse as a text file with one regex per line
			file, err := os.Open(path)
			if err != nil {
				color.Yellow("[!] Error reopening file %v: %v", path, err)
				return nil
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			idCount := 1
			skippedCount := 0

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())

				// Skip empty lines, comments, or lines that are obviously not regexes
				if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") ||
					strings.HasPrefix(line, " -") || strings.Contains(line, "++++++") {
					continue
				}

				// Try to compile with Go's regexp
				compiled, err := regexp.Compile(line)
				if err != nil {
					if skippedCount < 5 {
						color.Yellow("[!] Skipping invalid regex: %s - %v", line, err)
					} else if skippedCount == 5 {
						color.Yellow("[!] Skipping additional invalid regexes...")
					}
					skippedCount++
					continue
				}

				// Create a new rule
				rule := app.Rule{
					ID:             fmt.Sprintf("Rule-%d", idCount), // Incremental ID
					Pattern:        compiled,
					StringPattern:  line,                                            // Store the original pattern as StringPattern
					Description:    fmt.Sprintf("Description for Rule-%d", idCount), // Incremental description
					SmartFiltering: false,                                           // Default to false, you can modify if needed
				}

				// Add the rule to the config
				ruleConfig.Rules = append(ruleConfig.Rules, rule)

				idCount++ // Increment the rule ID counter
			}

			if skippedCount > 0 {
				color.Yellow("[!] Skipped %d invalid regex patterns", skippedCount)
			}
		} else {
			// Convert StringPattern to Pattern for TOML
			for i, rule := range ruleConfig.Rules {
				if rule.StringPattern != "" {
					compiled, err := regexp.Compile(rule.StringPattern)
					if err != nil {
						color.Yellow("[!] Unable to parse regex '%s' in TOML file: %v", rule.StringPattern, err)
						continue
					}
					ruleConfig.Rules[i].Pattern = compiled
				}
			}
		}
	}

	// Debug info about loaded rules
	if len(ruleConfig.Rules) > 0 && app.GetFlags().Debug {
		color.Green("[+] Loaded %d regex rules from %s", len(ruleConfig.Rules), path)
	}

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
	viperext := viper.New()
	viperext.SetConfigName("config")
	viperext.AddConfigPath("$HOME/.githound")
	viperext.AddConfigPath(".")
	if app.GetFlags().ConfigFile != "" {
		viperext.SetConfigFile(app.GetFlags().ConfigFile)
	}

	// Try reading the config file, but don't exit immediately on error
	configReadErr := viperext.ReadInConfig()

	// Read WebSocket URL from config (best effort)
	app.GetFlags().WebSocketURL = viperext.GetString("websocket_url")
	if app.GetFlags().WebSocketURL == "" {
		app.GetFlags().WebSocketURL = "wss://githoundexplore.com/ws"
	}

	// Read GitHub token from config (if available)
	githubToken := viperext.GetString("github_access_token")
	// Override with environment variable if set
	if envToken := os.Getenv("GITHOUND_GITHUB_TOKEN"); envToken != "" {
		githubToken = envToken
	}
	app.GetFlags().GithubAccessToken = githubToken

	// Read Insert Key from config (if available)
	insertKey := viperext.GetString("insert_key")
	// Override with environment variable if set
	if envInsertKey := os.Getenv("GITHOUND_INSERT_KEY"); envInsertKey != "" {
		insertKey = envInsertKey
	}

	// Check for insert_key.txt in ~/.githound
	homeDir, err := os.UserHomeDir()
	if err == nil {
		insertKeyPath := filepath.Join(homeDir, ".githound", "insert_key.txt")
		if keyBytes, err := ioutil.ReadFile(insertKeyPath); err == nil {
			insertKey = strings.TrimSpace(string(keyBytes))
		}
	}

	app.GetFlags().InsertKey = insertKey

	// // Handle dashboard-specific configuration first
	// if app.GetFlags().Dashboard {
	// 	if app.GetFlags().InsertKey == "" {
	// 		// color.Cyan("[*] Starting dashboard mode...")
	// 		// Start WebSocket connection - this will handle the account linking process
	// 		// app.StartWebSocket(app.GetFlags().WebSocketURL)

	// 		// Wait for the Insert Key to be set with timeout
	// 		timeout := time.After(30 * time.Second)
	// 		ticker := time.NewTicker(1 * time.Second)
	// 		defer ticker.Stop()

	// 		for {
	// 			select {
	// 			case <-ticker.C:
	// 				// Check if token file exists
	// 				homeDir, err := os.UserHomeDir()
	// 				if err != nil {
	// 					color.Red("Error getting home directory: %v", err)
	// 					continue
	// 				}
	// 				tokenFilePath := filepath.Join(homeDir, ".githound", "insert_token.txt")
	// 				if _, err := os.Stat(tokenFilePath); err == nil {
	// 					// Token file exists, read it
	// 					tokenBytes, err := ioutil.ReadFile(tokenFilePath)
	// 					if err == nil {
	// 						app.GetFlags().InsertKey = string(tokenBytes)
	// 						// color.Green("[+] Dashboard mode enabled with Insert Key.")
	// 						return
	// 					}
	// 				}
	// 			case <-timeout:
	// 				color.Red("[!] Timeout waiting for account linking. Please try again.")
	// 				os.Exit(1)
	// 			}
	// 		}
	// 	} else {
	// 		color.Green("[+] Dashboard mode enabled with Insert Key.")
	// 	}
	// 	return // Exit early for dashboard mode - no need to check GitHub token
	// }

	// Now, check if the essential GitHub token is present
	// Skip this check if in trufflehog mode
	if !app.GetFlags().Trufflehog && app.GetFlags().GithubAccessToken == "" {
		// Token is missing. Explain why and exit.
		if configReadErr != nil {
			if app.GetFlags().ConfigFile != "" {
				// Config file was specified but not found/readable
				color.Red("[!] Config file '%s' could not be read: %v", app.GetFlags().ConfigFile, configReadErr)
			} else {
				// Default config file locations not found/readable
				color.Red("[!] Default config file (config.yml in . or $HOME/.githound) could not be read: %v", configReadErr)
			}
			color.Red("[!] GitHub token also not found in GITHOUND_GITHUB_TOKEN environment variable.")
		} else {
			// Config file was read successfully, but token was missing
			color.Red("[!] GitHub token not found in config file ('github_access_token') or GITHOUND_GITHUB_TOKEN environment variable.")
		}
		color.Red("[!] A GitHub token is required to run GitHound.")
		os.Exit(1)
		return
	}
}

// StartPprofServer starts the pprof HTTP server for profiling
func StartPprofServer() {
	go func() {
		log.Println(http.ListenAndServe(app.GetFlags().ProfileAddr, nil))
	}()
}
