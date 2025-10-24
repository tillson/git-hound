package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/google/go-github/v57/github"
)

// Semaphore to limit concurrent HTTP requests
var httpSemaphore chan struct{}
var httpSemaphoreOnce sync.Once

func initHTTPSemaphore() {
	httpSemaphoreOnce.Do(func() {
		threads := GetFlags().Threads
		if threads <= 0 {
			threads = 10
		}
		httpSemaphore = make(chan struct{}, threads)
	})
}

func SearchWithAPI(queries []string) {
	// Initialize HTTP request limiter
	initHTTPSemaphore()

	token := GetFlags().GithubAccessToken
	if token == "" {
		color.Red("[!] GitHub access token not found. Please set it using GITHOUND_GITHUB_TOKEN environment variable or in your config file.")
		os.Exit(1)
	}

	client := github.NewClient(nil).WithAuthToken(token)
	if client == nil {
		color.Red("[!] Unable to create GitHub client. Please check your configuration.")
		os.Exit(1)
	}

	// Test the token by making a simple API call
	_, _, err := client.Users.Get(context.Background(), "")
	if err != nil {
		if strings.Contains(err.Error(), "401") {
			color.Red("[!] Invalid GitHub access token. Please check that your token is correct and has the necessary permissions.")
			os.Exit(1)
		} else if strings.Contains(err.Error(), "403") && strings.Contains(err.Error(), "rate reset in") {
			color.Yellow("[!] Rate limited by GitHub. Scans may be slower...")
		} else {
			color.Red("[!] Error authenticating with GitHub: %v", err)
			os.Exit(1)
		}
	}

	if !GetFlags().ResultsOnly && !GetFlags().JsonOutput && GetFlags().Debug {
		color.Cyan("[*] Logged into GitHub using API key")
	}

	options := github.SearchOptions{
		Sort: "indexed",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	// Enable text match metadata when in match-query mode
	if GetFlags().MatchQuery {
		options.TextMatch = true
	}

	http_client := http.Client{}
	rt := WithHeader(http_client.Transport)
	rt.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36")
	http_client.Transport = rt

	for _, query := range queries {
		for page := 0; page < int(math.Min(10, float64(GetFlags().Pages))); page++ {
			options.Page = page
			if GetFlags().Debug {
				TrackAPIRequest("Search.Code", fmt.Sprintf("Query: %s, Page: %d", query, page))
			}

			result, _, err := client.Search.Code(context.Background(), query, &options)
			// fmt.Println(result)
			for err != nil {
				// fmt.Println(err)
				if strings.Contains(err.Error(), "ERROR_TYPE_QUERY_PARSING_FATAL") {
					color.Red("[!] Invalid query: %s (maybe you need to use quotes?)", query)
					os.Exit(1)
				}
				resetTime := extractResetTime(err.Error())
				sleepDuration := resetTime + 3
				if !GetFlags().JsonOutput {
					color.Yellow("[!] GitHub API rate limit exceeded. Waiting %d seconds...", sleepDuration)
				}
				time.Sleep(time.Duration(sleepDuration) * time.Second)
				if GetFlags().Debug {
					TrackAPIRequest("Search.Code", fmt.Sprintf("Query: %s, Page: %d (retry)", query, page))
				}
				result, _, err = client.Search.Code(context.Background(), query, &options)
			}

			// If we get an empty page of results, stop searching
			if len(result.CodeResults) == 0 {
				if GetFlags().Debug {
					fmt.Println("No more results found, stopping search...")
				}
				break
			}

			if !GetFlags().ResultsOnly && !GetFlags().JsonOutput && GetFlags().Debug {
				fmt.Println("Analyzing " + strconv.Itoa(len(result.CodeResults)) + " repos on page " + strconv.Itoa(page+1) + "...")
			}

			// Initialize the worker pool if not already done
			workerPool := GetGlobalPool()
			// fmt.Println(result)
			for _, code_result := range result.CodeResults {
				// fmt.Println(code_result.GetPath())
				author_repo_str := code_result.GetRepository().GetOwner().GetLogin() + "/" + code_result.GetRepository().GetName()
				re := regexp.MustCompile(`\/([a-f0-9]{40})\/`)
				matches := re.FindStringSubmatch(code_result.GetHTMLURL())

				sha := ""
				if len(matches) > 1 {
					sha = matches[1]
				}

				// Get file commit information using git operations
				lastAuthor, lastUpdated := getFileCommitInfo(
					code_result.GetRepository().GetOwner().GetLogin(),
					code_result.GetRepository().GetName(),
					code_result.GetPath(),
					sha)

				// Create a repo result object to pass to the worker
				repoResult := RepoSearchResult{
					Repo:                      author_repo_str,
					File:                      code_result.GetPath(),
					Raw:                       author_repo_str + "/" + sha + "/" + code_result.GetPath(),
					Source:                    "repo",
					Query:                     query,
					URL:                       "https://github.com/" + author_repo_str + "/blob/" + sha + "/" + code_result.GetPath(),
					SourceFileLastAuthorEmail: lastAuthor,
					SourceFileLastUpdated:     lastUpdated,
					TextMatches:               code_result.TextMatches,
				}

				// Increment the wait group before submitting the job
				SearchWaitGroup.Add(1)
				if GetFlags().Debug {
					fmt.Printf("[DEBUG] Added to wait group for repo: %s (total: %d)\n", author_repo_str, len(result.CodeResults))
				}

				// Submit the job to the worker pool instead of creating a goroutine directly
				workerPool.Submit(func() {
					// Process the repository in the worker pool
					ScanAndPrintResult(&http_client, repoResult)
				})
			}
		}

		if !GetFlags().ResultsOnly && !GetFlags().JsonOutput {
			color.Green("Finished searching... Now waiting for scanning to finish.")
		}

		SearchWaitGroup.Wait()
		if !GetFlags().ResultsOnly && !GetFlags().JsonOutput {
			color.Green("Finished scanning.")
		}
	}
}

// getFileCommitInfo fetches the last commit information for a specific file using git operations
func getFileCommitInfo(owner, repo, path, commitHash string) (lastAuthor, lastUpdated string) {
	// Create a temporary directory for git operations
	tempDir, err := os.MkdirTemp("", "git-hound-*")
	if err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error creating temp dir: %v\n", err)
		}
		return "", ""
	}
	defer os.RemoveAll(tempDir)

	// Initialize empty git repository
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error initializing git repo: %v\n", err)
		}
		return "", ""
	}

	// Add remote origin
	remoteCmd := exec.Command("git", "remote", "add", "origin", fmt.Sprintf("https://github.com/%s/%s.git", owner, repo))
	remoteCmd.Dir = tempDir
	if err := remoteCmd.Run(); err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error adding remote: %v\n", err)
		}
		return "", ""
	}

	// Fetch only the specific commit using --filter=blob:none
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", commitHash, "--filter=blob:none")
	fetchCmd.Dir = tempDir
	if err := fetchCmd.Run(); err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error fetching commit %s: %v\n", commitHash, err)
		}
		// Try alternative fetch method without filter
		fetchCmd2 := exec.CommandContext(ctx, "git", "fetch", "origin", commitHash)
		fetchCmd2.Dir = tempDir
		if err2 := fetchCmd2.Run(); err2 != nil {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Error with alternative fetch for commit %s: %v\n", commitHash, err2)
			}
			return "", ""
		}
	}

	// Get commit metadata using git cat-file
	catCmd := exec.CommandContext(ctx, "git", "cat-file", "-p", commitHash)
	catCmd.Dir = tempDir
	output, err := catCmd.Output()
	if err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error reading commit %s: %v\n", commitHash, err)
		}
		return "", ""
	}

	// Parse commit object to extract author and date
	commitText := string(output)
	lines := strings.Split(commitText, "\n")

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Commit object for %s:\n%s\n", commitHash, commitText)
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "author ") {
			// Parse author line: "author Name <email> timestamp timezone"
			parts := strings.Fields(line)
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Author line parts: %v\n", parts)
			}

			if len(parts) >= 4 {
				// Extract email from <email> format
				emailRegex := regexp.MustCompile(`<([^>]+)>`)
				emailMatch := emailRegex.FindStringSubmatch(line)
				if len(emailMatch) > 1 {
					lastAuthor = emailMatch[1]
				}

				// Extract timestamp (second to last field)
				timestampStr := parts[len(parts)-2]
				if GetFlags().Debug {
					fmt.Printf("[DEBUG] Timestamp string: %s\n", timestampStr)
				}
				if timestamp, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
					lastUpdated = time.Unix(timestamp, 0).Format(time.RFC3339)
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Parsed timestamp: %d -> %s\n", timestamp, lastUpdated)
					}
				} else {
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Error parsing timestamp %s: %v\n", timestampStr, err)
					}
				}
			}
			break
		}
	}

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Found commit info for %s/%s/%s (commit %s): author=%s, updated=%s\n", owner, repo, path, commitHash, lastAuthor, lastUpdated)
	}

	return lastAuthor, lastUpdated
}

// extractResetTime extracts the number of seconds until the rate limit resets from the error message.
func extractResetTime(errorMessage string) int {
	re := regexp.MustCompile(`rate reset in (\d+)s`)
	matches := re.FindStringSubmatch(errorMessage)
	if len(matches) > 1 {
		seconds, err := strconv.Atoi(matches[1])
		if err == nil {
			return seconds
		}
	}
	return 0
}
