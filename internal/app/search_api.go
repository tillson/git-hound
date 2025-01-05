package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/fatih/color"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/viper"
)

func SearchWithAPI(queries []string) {
	token := viper.GetString("github_access_token")
	client := github.NewClient(nil).WithAuthToken(token)
	if client == nil {
		color.Red("[!] Unable to authenticate with GitHub API. Please check that your GitHub personal access token is correct.")
		os.Exit(1)
	}
	if !GetFlags().ResultsOnly && !GetFlags().JsonOutput {
		color.Cyan("[*] Logged into GitHub using API key")
	}

	options := github.SearchOptions{
		Sort: "indexed",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	// TODO: need to add coding language flag support

	http_client := http.Client{}
	rt := WithHeader(http_client.Transport)
	rt.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36")
	http_client.Transport = rt

	backoff := 1.0
	for _, query := range queries {
		for page := 0; page < int(math.Min(10, float64(GetFlags().Pages))); page++ {
			options.Page = page
			result, _, err := client.Search.Code(context.Background(), query, &options)
			for err != nil {
				// color.Red("Error searching GitHub: " + err.Error())
				time.Sleep(5 * time.Second)
				backoff = backoff * 1.5
				result, _, err = client.Search.Code(context.Background(), query, &options)
			}

			backoff = backoff / 1.5
			backoff = math.Max(1, backoff)
			if !GetFlags().ResultsOnly && !GetFlags().JsonOutput {
				fmt.Println("Analyzing " + strconv.Itoa(result.GetTotal()) + " repos on page " + strconv.Itoa(page+1) + "...")
			}
			for _, code_result := range result.CodeResults {
				// fmt.Println(*code_result.GetRepository())
				author_repo_str := code_result.GetRepository().GetOwner().GetLogin() + "/" + code_result.GetRepository().GetName()
				// fmt.Println(code_result.GetPath())

				re := regexp.MustCompile(`\/([a-f0-9]{40})\/`)
				matches := re.FindStringSubmatch(code_result.GetHTMLURL())

				sha := ""
				if len(matches) > 1 {
					sha = matches[1]
				}

				// fmt.Println(code_result.GetSHA())
				// fmt.Println(1)
				SearchWaitGroup.Add(1)
				go ScanAndPrintResult(&http_client, RepoSearchResult{
					Repo:   author_repo_str,
					File:   code_result.GetPath(),
					Raw:    author_repo_str + "/" + sha + "/" + code_result.GetPath(),
					Source: "repo",
					Query:  query,
					URL:    "https://github.com/" + author_repo_str + "/blob/" + sha + "/" + code_result.GetPath(),
				})

				// break
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

// // SearchGitHubRepositories searches GitHub repositories based on the provided query
// func SearchGitHubRepositories(query string) ([]*github.Repository, error) {
// 	client := GitHubAPIClient()

// 	opt := &github.SearchOptions{
// 		ListOptions: github.ListOptions{PerPage: 10},
// 	}

// 	result, _, err := client.Search.Repositories(context.Background(), query, opt)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return result.Repositories, nil
// }
