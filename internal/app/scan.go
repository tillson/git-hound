package app

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
)

// ResultScan is the final scan result.
type ResultScan struct {
	matches []Match
	RepoSearchResult
}

// Match represents a keyword/API key match
type Match struct {
	text        string
	keywordType string
}

var scannedRepos = make(map[string]bool)
var apiKeyMap = make(map[string]bool)

// ScanAndPrintResult scans and prints information about a search result.
func ScanAndPrintResult(client *http.Client, repo RepoSearchResult) {
	if scannedRepos[repo.Repo] {
		return
	}
	var base string
	if repo.Source == "repo" {
		base = "https://raw.githubusercontent.com/"
	} else if repo.Source == "gist" {
		base = "https://gist.githubusercontent.com/"
	}
	data, err := DownloadRawFile(client, base, repo)
	if err != nil {
		log.Fatal(err)
	}
	resultString := string(data)
	keywords := MatchKeywords(resultString, repo)
	apiKeys := MatchAPIKeys(resultString, repo)

	var fossils []Match
	if RepoIsUnpopular(client, repo) {
		scannedRepos[repo.Repo] = true
		fossils = Dig(repo)
	}
	for _, result := range fossils {
		if result.keywordType == "apiKey" {
			apiKeys = append(apiKeys, result)
		} else if result.keywordType == "keyword" {
			keywords = append(apiKeys, result)
		}
	}

	fmt.Println("https://github.com/" + repo.Repo)
	for _, result := range keywords {
		fmt.Println("  [" + result.keywordType + "] " + result.text)
	}
	for _, result := range apiKeys {
		if !apiKeyMap[result.text] {
			fmt.Println(" . [" + result.keywordType + "] " + result.text)
			apiKeyMap[result.text] = true
		}
	}

}

// MatchKeywords takes a string and checks if it contains sensitive information using pattern matching.
func MatchKeywords(str string, result RepoSearchResult) (matches []Match) {
	regexString := "(?i)\\b(sf_username" +
		"|(stage|staging|atlassian|jira|conflence|zendesk|cloud|beta|dev|internal)\\." +
		regexp.QuoteMeta(result.Query) + "|db_username|db_password" +
		"|hooks\\.slack\\.com|pt_token|full_resolution_time_in_minutes" +
		"|xox[a-zA-Z]-[a-zA-Z0-9-]+" +
		"|s3\\.console\\.aws\\.amazon\\.com\\/s3\\/buckets|" +
		"id_rsa|pg_pass|[\\w\\.=-]+@" + regexp.QuoteMeta(result.Query) + ")\\b"
	regex := regexp.MustCompile(regexString)
	matcheStrings := regex.FindAllString(str, -1)

	for _, match := range matcheStrings {
		matches = append(matches, Match{
			keywordType: "keyword",
			text:        string(match),
		})
	}
	return matches
}

// MatchAPIKeys takes a string and checks if it contains API keys using pattern matching and entropy checking.
func MatchAPIKeys(str string, result RepoSearchResult) (matches []Match) {
	regexString := "(?i)(ACCESS|SECRET|LICENSE|CRYPT|PASS|KEY|ADMIn|TOKEN|PWD|Authorization|Bearer)[\\w\\s:=\"']{0,20}[=:\\s'\"]([\\w\\-+=]{32,})\\b"
	regex := regexp.MustCompile(regexString)
	matcheStrings := regex.FindAllStringSubmatch(str, -1)
	for _, match := range matcheStrings {
		matches = append(matches, Match{
			keywordType: "apiKey",
			text:        string(match[2]),
		})
	}
	return matches
}

func MatchFileExtensions(str string, result RepoSearchResult) (matches []Match) {
	regexString := "\\.(zip)$"
	regex := regexp.MustCompile(regexString)
	matcheStrings := regex.FindAllStringSubmatch(str, -1)
	for _, match := range matcheStrings {
		matches = append(matches, Match{
			keywordType: "fileExtension",
			text:        string(match[0]),
		})
	}
	return matches
}
