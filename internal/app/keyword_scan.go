package app

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// ResultScan is the final scan result.
type ResultScan struct {
	Matches []Match
	RepoSearchResult
}

// Match represents a keyword/API key match
type Match struct {
	Text        string
	KeywordType string
	Line        Line
	Commit      string
	CommitFile  string
}

// Line represents a text line, the context for a Match.
type Line struct {
	Text          string
	MatchIndex    int
	MatchEndIndex int
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
		base = "https://raw.githubusercontent.com"
	} else if repo.Source == "gist" {
		base = "https://gist.githubusercontent.com"
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
		if result.KeywordType == "apiKey" {
			apiKeys = append(apiKeys, result)
		} else if result.KeywordType == "keyword" {
			keywords = append(apiKeys, result)
		}
	}

	if len(keywords)+len(apiKeys) > 0 {
		color.Green("[https://github.com/" + repo.Repo + "]")
		for _, result := range keywords {
			// color.Red("[" + result.KeywordType + "] " + result.Text)
			PrintContextLine(result.Line)
			PrintResultLink(repo, result)
			// fmt.Println()
		}
		for _, result := range apiKeys {
			if !apiKeyMap[result.Text] {
				// color.Red("[" + result.KeywordType + "] " + result.Text)
				apiKeyMap[result.Text] = true
				PrintContextLine(result.Line)
				PrintResultLink(repo, result)
				// fmt.Println()
			}
		}
	}

}

// MatchKeywords takes a string and checks if it contains sensitive information using pattern matching.
func MatchKeywords(str string, result RepoSearchResult) (matches []Match) {
	// fmt.Println(regexp.QuoteMeta(result.Query))
	regexString := "(?i)\\b(sf_username|" +
		"[\\.\b][A-z0-9\\-]{1,256}\\." +
		regexp.QuoteMeta(result.Query) + "|db_username|db_password" +
		"|hooks\\.slack\\.com|pt_token|full_resolution_time_in_minutes" +
		"|xox[a-zA-Z]-[a-zA-Z0-9-]+" +
		"|s3\\.console\\.aws\\.amazon\\.com\\/s3\\/buckets|" +
		"id_rsa|pg_pass|[\\w\\.=-]+@" + regexp.QuoteMeta(result.Query) + ")\\b"
	regex := regexp.MustCompile(regexString)
	matchStrings := regex.FindAllString(str, -1)

	for _, match := range matchStrings {
		matches = append(matches, Match{
			KeywordType: "keyword",
			Text:        string(match),
			Line:        GetLine(str, match),
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
		if Entropy(match[2]) > 3.5 {
			matches = append(matches, Match{
				KeywordType: "apiKey",
				Text:        string(match[2]),
				Line:        GetLine(str, match[2]),
			})
		}
	}
	return matches
}

// MatchFileExtensions matches interesting file extensions.
func MatchFileExtensions(str string, result RepoSearchResult) (matches []Match) {
	if str == "" {
		return matches
	}
	regexString := "\\.(zip)$"
	regex := regexp.MustCompile(regexString)
	matcheStrings := regex.FindAllStringSubmatch(str, -1)
	for _, match := range matcheStrings {
		if len(match) > 0 {
			matches = append(matches, Match{
				KeywordType: "fileExtension",
				Text:        string(match[0]),
				Line:        GetLine(str, match[0]),
			})
		}
	}
	return matches
}

// GetLine grabs the full line of the first instance of a pattern within it
func GetLine(source string, pattern string) Line {
	patternIndex := strings.Index(source, pattern)
	i, j := 0, len(pattern)
	for patternIndex+i > 0 && i > -30 && source[patternIndex+i] != '\n' && source[patternIndex+i] != '\r' {
		i--
	}
	for patternIndex+j < len(source) && j < 10 && source[patternIndex+j] != '\n' && source[patternIndex+j] != '\r' {
		j++
	}
	if patternIndex+i == patternIndex+j {
		fmt.Println("issue: " + pattern)
	}
	return Line{Text: source[patternIndex+i : patternIndex+j], MatchIndex: Abs(i), MatchEndIndex: j + Abs(i)}
}

// PrintContetLine pretty-prints the line of a Match, with the result highlighted.
func PrintContextLine(line Line) {
	red := color.New(color.FgRed).SprintFunc()
	fmt.Printf("%s%s%s\n",
		line.Text[:line.MatchIndex],
		red(line.Text[line.MatchIndex:line.MatchEndIndex]),
		line.Text[line.MatchEndIndex:])
}

// Abs returns the absolute value of x.
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Entropy calculates the Shannon entropy of a string
func Entropy(str string) (entropy float32) {
	if str == "" {
		return entropy
	}
	freq := 1.0 / float32(len(str))
	freqMap := make(map[rune]float32)
	for _, char := range str {
		freqMap[char] += freq
	}
	for _, entry := range freqMap {
		entropy -= entry * float32(math.Log2(float64(entry)))
	}
	return entropy
}

// PrintResultLink prints a link to the result.
func PrintResultLink(result RepoSearchResult, match Match) {
	if match.Commit != "" {
		color.New(color.Faint).Println("https://github.com/" + result.Repo + "/commit/" + match.Commit)
	} else {
		color.New(color.Faint).Println("https://github.com/" + result.Raw)
	}
}
