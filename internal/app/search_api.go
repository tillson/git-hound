package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/viper"
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

	token := viper.GetString("github_access_token")
	if token == "" {
		color.Red("[!] GitHub access token not found. Please set it using --token or in your config file.")
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
		} else {
			color.Red("[!] Error authenticating with GitHub: %v", err)
		}
		os.Exit(1)
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

	http_client := http.Client{}
	rt := WithHeader(http_client.Transport)
	rt.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36")
	http_client.Transport = rt

	backoff := 1.0
	for _, query := range queries {
		if GetFlags().Dashboard && InsertKey != "" {
			BrokerSearchCreation(query)
		}
		for page := 0; page < int(math.Min(10, float64(GetFlags().Pages))); page++ {
			options.Page = page
			if GetFlags().Debug {
				TrackAPIRequest("Search.Code", fmt.Sprintf("Query: %s, Page: %d", query, page))
			}
			result, _, err := client.Search.Code(context.Background(), query, &options)
			for err != nil {
				fmt.Println(err)
				resetTime := extractResetTime(err.Error())
				sleepDuration := resetTime + 3
				color.Yellow("[!] GitHub API rate limit exceeded. Waiting %d seconds...", sleepDuration)
				time.Sleep(time.Duration(sleepDuration) * time.Second)
				backoff = backoff * 1.5
				if GetFlags().Debug {
					TrackAPIRequest("Search.Code", fmt.Sprintf("Query: %s, Page: %d (retry)", query, page))
				}
				result, _, err = client.Search.Code(context.Background(), query, &options)
			}

			backoff = backoff / 1.5
			backoff = math.Max(1, backoff)
			if !GetFlags().ResultsOnly && !GetFlags().JsonOutput && GetFlags().Debug {
				fmt.Println("Analyzing " + strconv.Itoa(len(result.CodeResults)) + " repos on page " + strconv.Itoa(page+1) + "...")
			}

			// Initialize the worker pool if not already done
			workerPool := GetGlobalPool()

			for _, code_result := range result.CodeResults {
				// fmt.Println(code_result.GetPath())
				author_repo_str := code_result.GetRepository().GetOwner().GetLogin() + "/" + code_result.GetRepository().GetName()
				re := regexp.MustCompile(`\/([a-f0-9]{40})\/`)
				matches := re.FindStringSubmatch(code_result.GetHTMLURL())

				sha := ""
				if len(matches) > 1 {
					sha = matches[1]
				}

				// Create a repo result object to pass to the worker
				repoResult := RepoSearchResult{
					Repo:   author_repo_str,
					File:   code_result.GetPath(),
					Raw:    author_repo_str + "/" + sha + "/" + code_result.GetPath(),
					Source: "repo",
					Query:  query,
					URL:    "https://github.com/" + author_repo_str + "/blob/" + sha + "/" + code_result.GetPath(),
				}

				// Increment the wait group before submitting the job
				SearchWaitGroup.Add(1)

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
