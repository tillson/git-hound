package app

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

// RepoSearchResult represents a result in GitHub/Gist code search.
type RepoSearchResult struct {
	Repo          string
	File          string
	Raw           string
	Source        string
	Query         string
	URL           string
	searchOptions *SearchOptions
}

// Search Everything
func Search(query string, client *http.Client) (results []RepoSearchResult, err error) {

	var languages []string
	if GetFlags().LanguageFile != "" {
		languages = GetFileLines(GetFlags().LanguageFile)
	}

	options := SearchOptions{
		MaxPages: 100,
	}

	resultMap := make(map[string]bool)
	if !GetFlags().NoRepos {
		if len(languages) > 0 {
			for _, language := range languages {
				options.Language = language
				err = SearchGitHub(query, options, client, &results, resultMap)
				if err != nil {
					color.Red("[!] Error searching GitHub for `" + query + "`")
				}
			}
		}
		if !GetFlags().OnlyFiltered {
			err = SearchGitHub(query, options, client, &results, resultMap)
			if err != nil {
				color.Red("[!] Error searching GitHub for `" + query + "`")
			}
		}
	}
	if !GetFlags().NoGists {
		resultMap = make(map[string]bool)
		if len(languages) > 0 {
			for _, language := range languages {
				options.Language = language
				err = SearchGist(query, options, client, &results, resultMap)
				if err != nil {
					color.Red("[!] Error searching Gist for `" + query + "`")
				}
			}
		}
		if !GetFlags().OnlyFiltered {
			err = SearchGist(query, options, client, &results, resultMap)
			if err != nil {
				color.Red("[!] Error searching Gist for `" + query + "`")
			}
		}
	}

	return results, err
}

// SearchGitHub searches GitHub code results for the given query
func SearchGitHub(query string, options SearchOptions, client *http.Client, results *[]RepoSearchResult, resultSet map[string]bool) (err error) {
	// TODO: A lot of this code is shared between GitHub and Gist searches,
	// so we should rework the logic
	base := ""
	if GetFlags().GithubRepo {
		base = "https://github.com/" + query + "/search"
	} else {
		base = "https://github.com/search"
	}

	page, pages := 0, 1
	var delay = 5
	orders := []string{"asc"}
	rankings := []string{"indexed"}
	for i := 0; i < len(orders); i++ {
		for j := 0; j < len(rankings); j++ {
			if i == 1 && j == 1 {
				continue
			}
			for page < pages {
				options.Page = (page + 1)
				str := ConstructSearchURL(base, query, options)
				// fmt.Println(str)
				response, err := client.Get(str)
				if err != nil {
					if response != nil {
						if response.StatusCode == 403 {
							response.Body.Close()
							delay += 5
							color.Yellow("[!] Rate limited by GitHub. Waiting " + strconv.Itoa(delay) + "s...")
							time.Sleep(time.Duration(delay) * time.Second)
						} else if response.StatusCode == 503 {
							break
						}
					} else {
						fmt.Println(err)
					}
					continue
				}
				if delay > 10 {
					delay--
				}
				responseData, err := ioutil.ReadAll(response.Body)
				responseStr := string(responseData)
				if err != nil {
					log.Fatal(err)
				}
				response.Body.Close()
				if page == 0 {
					regex := regexp.MustCompile("\\bdata\\-total\\-pages\\=\"(\\d+)\"")
					match := regex.FindStringSubmatch(responseStr)
					if err != nil {
						log.Fatal(err)
					}
					if len(match) == 2 {
						newPages, err := strconv.Atoi(match[1])
						if err == nil {
							if newPages > GetFlags().Pages {
								newPages = GetFlags().Pages
							}
							pages = newPages
							if pages > 99 && GetFlags().ManyResults {
								if !GetFlags().ResultsOnly {
									color.Cyan("[*] Searching 100+ pages of results for '" + query + "'...")
								}
								orders = append(orders, "desc")
								rankings = append(orders, "")
							} else {
								if !GetFlags().ResultsOnly {
									color.Cyan("[*] Searching " + strconv.Itoa(pages) + " pages of results for '" + query + "'...")
								}
							}
						} else {
							color.Red("[!] An error occurred while parsing the page count.")
							fmt.Println(err)
						}
					} else {
						if strings.Index(responseStr, "Sign in to GitHub") > -1 {
							color.Red("[!] Unable to log into GitHub.")
							log.Fatal()
						} else {
							if !GetFlags().ResultsOnly {
								color.Cyan("[*] Searching 1 page of results for '" + query + "'...")
							}
						}
					}
				}
				page++
				resultRegex := regexp.MustCompile("href=\"\\/((.*)\\/blob\\/([0-9a-f]{40}\\/([^#\"]+)))\">")
				matches := resultRegex.FindAllStringSubmatch(responseStr, -1)
				for _, element := range matches {
					if len(element) == 5 {
						if resultSet[(element[2]+"/"+element[3])] == true {
							continue
						}
						resultSet[(element[2] + "/" + element[3])] = true
						go ScanAndPrintResult(client, RepoSearchResult{
							Repo:   element[2],
							File:   element[4],
							Raw:    element[2] + "/master/" + element[4],
							Source: "repo",
							Query:  query,
							URL:    "https://github.com/" + element[2] + "/blob/" + element[3],
						})
					}
				}
				time.Sleep(time.Duration(delay) * time.Second)
			}
		}
	}
	return nil
}

// SearchGist searches Gist results for the given query
func SearchGist(query string, options SearchOptions, client *http.Client, results *[]RepoSearchResult, resultSet map[string]bool) (err error) {
	// TODO: A lot of this code is shared between GitHub and Gist searches,
	// so we should rework the logic
	base := "https://gist.github.com/search"
	page, pages := 0, 1
	var delay = 5
	for page < pages {
		options.Page = (page + 1)
		response, err := client.Get(ConstructSearchURL(base, query, options))
		if err != nil {
			if response != nil {
				if response.StatusCode == 403 {
					delay += 5
					color.Yellow("[!] Rate limited by GitHub. Waiting " + strconv.Itoa(delay) + "s...")
					time.Sleep(time.Duration(delay) * time.Second)
				} else if response.StatusCode == 503 {
					break
				}
			} else {
				fmt.Println(err)
			}
			continue
		}
		if delay > 10 {
			delay--
		}
		responseData, err := ioutil.ReadAll(response.Body)
		responseStr := string(responseData)
		if err != nil {
			log.Fatal(err)
		}
		if page == 0 {
			regex := regexp.MustCompile("\\bdata\\-total\\-pages\\=\"(\\d+)\"")
			match := regex.FindStringSubmatch(responseStr)
			if err != nil {
				log.Fatal(err)
			}
			if len(match) == 2 {
				newPages, err := strconv.Atoi(match[1])
				if err == nil {
					if newPages > GetFlags().Pages {
						newPages = GetFlags().Pages
					}
					pages = newPages
					if pages > 99 {
						if !GetFlags().ResultsOnly {
							color.Cyan("[*] Searching 100+ pages of Gist results for '" + query + "'...")
						}
					} else {
						if !GetFlags().ResultsOnly {
							color.Cyan("[*] Searching " + strconv.Itoa(pages) + " pages of results for '" + query + "'...")
						}
					}
				} else {
					color.Red("[!] An error occurred while parsing the Gist page count.")
					fmt.Println(err)
				}
			} else {
				if strings.Index(responseStr, "Sign in to GitHub") > -1 {
					color.Red("[!] Unable to log into GitHub.")
					log.Fatal()
				} else {
					if !GetFlags().ResultsOnly {
						color.Cyan("[*] Searching 1 page of Gist results for '" + query + "'...")
					}
				}
			}
		}
		page++
		resultRegex := regexp.MustCompile("href=\"\\/(\\w+\\/[0-9a-z]{5,})\">")
		matches := resultRegex.FindAllStringSubmatch(responseStr, -1)
		for _, element := range matches {
			if len(element) == 2 {
				if resultSet[element[1]] == true {
					continue
				}
				resultSet[element[1]] = true
				go ScanAndPrintResult(client, RepoSearchResult{
					Repo:   element[1],
					File:   element[1],
					Raw:    GetRawGistPage(client, element[1]),
					Source: "gist",
					Query:  query,
					URL:    "https://gist.github.com/" + element[1],
				})
			}
		}
		time.Sleep(time.Duration(delay) * time.Second)
	}
	return nil
}
