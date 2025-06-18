package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/google/go-github/v57/github"
)

// ResultScan is the final scan result.
type ResultScan struct {
	Matches []Match
	RepoSearchResult
}

// Match represents a keyword/API key match
type Match struct {
	Text       string
	Attributes []string
	Line       Line
	Commit     string
	CommitFile string
	File       string
	Expression string
}

// Line represents a text line, the context for a Match.
type Line struct {
	Text          string
	MatchIndex    int
	MatchEndIndex int
}

var scannedRepos = make(map[string]bool)
var uniqueMatches = make(map[string]bool)
var mapMutex = sync.Mutex{} // Add mutex for map synchronization

var customRegexes []*regexp.Regexp
var loadedRegexes = false

// ScanAndPrintResult scans and prints information about a search result.
func ScanAndPrintResult(client *http.Client, repo RepoSearchResult) {
	if !GetFlags().FastMode {
		base := GetRawURLForSearchResult(repo)
		data, err := DownloadRawFile(client, base, repo)
		if err != nil {
			if GetFlags().Debug {
				fmt.Printf("Error downloading %s: %v\n", repo.Raw, err)
			}
			return // Skip this file and continue with others
		}
		repo.Contents = string(data)
	}
	if GetFlags().Debug {
		fmt.Println("downloaded", len(repo.Contents), repo.Repo, repo.File)
	}
	if GetFlags().AllResults {
		if GetFlags().JsonOutput {
			a, _ := json.Marshal(map[string]string{
				"repo":    repo.Repo,
				"file":    repo.File,
				"content": repo.Contents,
			})
			fmt.Println(string(a))
		} else {
			color.New(color.Faint).Println("[" + repo.Repo + "]")
			color.New(color.Faint).Println("[" + repo.File + "]")
			color.New(color.Faint).Println(repo.Contents)
		}
	} else {
		// Get pointer matches
		matches, score := GetMatchesForString(repo.Contents, repo, true)

		// Process potential additional matches from digging
		if (GetFlags().DigCommits || GetFlags().DigRepo) && RepoIsUnpopular(client, repo) && score > -1 {
			// Lock the map for thread-safe access
			mapMutex.Lock()
			repoAlreadyScanned := scannedRepos[repo.Repo]
			if !repoAlreadyScanned {
				scannedRepos[repo.Repo] = true
			}
			mapMutex.Unlock()

			if !repoAlreadyScanned {
				regex := regexp.MustCompile("(?i)(alexa|urls|adblock|domain|dns|top1000|top\\-1000|httparchive" +
					"|blacklist|hosts|ads|whitelist|crunchbase|tweets|tld|hosts\\.txt" +
					"|host\\.txt|aquatone|recon\\-ng|hackerone|bugcrowd|xtreme|list|tracking|malicious|ipv(4|6)|host\\.txt)")
				fileNameMatches := regex.FindAllString(repo.Repo, -1)
				if len(fileNameMatches) == 0 {
					// Get additional matches from Dig function
					dig_matches := Dig(repo)
					for _, match := range dig_matches {
						// Add the dig-files attribute directly to the pointer match
						match.Attributes = append(match.Attributes, "dig-files")

						// Add to matches - no need to copy since Dig now returns []*Match
						matches = append(matches, match)
					}
				}
			}
		}

		// Process and display matches
		if len(matches) > 0 {
			// Fetch GitHub API info about the repo
			token := GetFlags().GithubAccessToken
			client := github.NewClient(nil).WithAuthToken(token)
			if client != nil {
				// gh_repo_obj, _, err := client.Repositories.Get(strings.Split(repo.Repo, "/")[0], strings.Split(repo.Repo, "/")[1])
				// get repo's commits
				owner := strings.Split(repo.Repo, "/")[0]
				repoName := strings.Split(repo.Repo, "/")[1]
				TrackAPIRequest("ListCommits", fmt.Sprintf("Owner: %s, Repo: %s, Path: %s", owner, repoName, repo.File))
				commits, _, err := client.Repositories.ListCommits(context.Background(), owner, repoName, &github.CommitsListOptions{
					Path: repo.File,
				})
				if err != nil {
					fmt.Println(err)
					repo.SourceFileLastUpdated = ""
				} else {
					repo.SourceFileLastUpdated = commits[0].Commit.Author.Date.String()
					repo.SourceFileLastAuthorEmail = *commits[0].Commit.Author.Email
				}
			}

			resultRepoURL := GetRepoURLForSearchResult(repo)
			i := 0
			for _, result := range matches {
				// Create the result payload
				resultPayload := map[string]interface{}{
					"repo":              resultRepoURL,
					"context":           result.Line.Text,
					"match":             result.Line.Text[result.Line.MatchIndex:result.Line.MatchEndIndex],
					"attributes":        result.Attributes,
					"file_last_updated": repo.SourceFileLastUpdated,
					"file_last_author":  repo.SourceFileLastAuthorEmail,
					"url":               GetResultLink(repo, result),
				}

				// For dug matches, update the file information while maintaining the structure
				if len(result.Attributes) > 0 && result.Attributes[0] == "dig-files" {
					resultPayload["file"] = result.File
					// Extract the base URL and commit hash from the original URL
					baseURL := strings.Split(repo.URL, "/blob/")[0]
					commitHash := strings.Split(repo.URL, "/blob/")[1]
					commitHash = strings.Split(commitHash, "/")[0]
					// Construct new URL with the file path from result.File
					resultPayload["url"] = fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, result.File)
				}

				// Use mutex to protect access to uniqueMatches map
				matchKey := fmt.Sprintf("%s|%s", resultPayload["match"], resultRepoURL)
				// For dig-files matches, include the file path in the deduplication key
				if len(result.Attributes) > 0 && result.Attributes[0] == "dig-files" {
					matchKey = fmt.Sprintf("%s|%s|%s", resultPayload["match"], resultRepoURL, result.File)
				}
				mapMutex.Lock()
				isDuplicate := uniqueMatches[matchKey]
				if !isDuplicate {
					uniqueMatches[matchKey] = true
				}
				mapMutex.Unlock()

				if isDuplicate {
					continue
				}

				if i == 0 {
					if !GetFlags().ResultsOnly && !GetFlags().JsonOutput {
						color.Green("[" + resultRepoURL + "]")
					}
				}
				i += 1
				if GetFlags().ResultsOnly {
					fmt.Println(result.Text)
				} else {
					if GetFlags().JsonOutput {
						a, _ := json.Marshal(resultPayload)
						fmt.Println(string(a))
					} else {
						PrintContextLine(result.Line)
						PrintPatternLine(result)
						PrintAttributes(result)
						// Always print the file path
						if len(result.Attributes) > 0 && result.Attributes[0] == "dig-files" {
							color.New(color.Faint).Println("file:     " + result.File)
							// Construct URL for dig-files matches
							baseURL := strings.Split(repo.URL, "/blob/")[0]
							commitHash := strings.Split(repo.URL, "/blob/")[1]
							commitHash = strings.Split(commitHash, "/")[0]
							digURL := fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, result.File)
							color.New(color.Faint).Println(digURL)
						} else {
							color.New(color.Faint).Println("file:     " + repo.File)
							color.New(color.Faint).Println(GetResultLink(repo, result))
						}
					}
				}
				if GetFlags().Dashboard && GetFlags().InsertKey != "" {
					resultJSON, err := json.Marshal(resultPayload)
					if err == nil {
						searchID := GetFlags().SearchID
						if searchID != "" {
							if GetFlags().Trufflehog {
								SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "searchID": "%s", "result": %s}`, GetFlags().InsertKey, searchID, string(resultJSON)))
							} else {
								escapedQuery, _ := json.Marshal(repo.Query)
								// For dig-files matches, ensure the file path and URL are correctly set
								if len(result.Attributes) > 0 && result.Attributes[0] == "dig-files" {
									resultPayload["file"] = result.File
									baseURL := strings.Split(repo.URL, "/blob/")[0]
									commitHash := strings.Split(repo.URL, "/blob/")[1]
									commitHash = strings.Split(commitHash, "/")[0]
									resultPayload["url"] = fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, result.File)
									resultJSON, _ = json.Marshal(resultPayload)
								}
								SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "searchID": "%s", "result": %s, "search_term": %s}`, GetFlags().InsertKey, searchID, string(resultJSON), string(escapedQuery)))
							}
						} else {
							if GetFlags().Trufflehog {
								SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "result": %s}`, GetFlags().InsertKey, string(resultJSON)))
							} else {
								escapedQuery, _ := json.Marshal(repo.Query)
								// For dig-files matches, ensure the file path and URL are correctly set
								if len(result.Attributes) > 0 && result.Attributes[0] == "dig-files" {
									resultPayload["file"] = result.File
									baseURL := strings.Split(repo.URL, "/blob/")[0]
									commitHash := strings.Split(repo.URL, "/blob/")[1]
									commitHash = strings.Split(commitHash, "/")[0]
									resultPayload["url"] = fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, result.File)
									resultJSON, _ = json.Marshal(resultPayload)
								}
								SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "result": %s, "search_term": %s}`, GetFlags().InsertKey, string(resultJSON), string(escapedQuery)))
							}
						}
					} else {
						color.Red("Error marshalling result to JSON: %v", err)
					}
				}
			}
			if GetFlags().Debug {
				fmt.Println("Finished scanning " + repo.Repo + "...")
			}

			// Clean up the matches by returning them to the pool
			PutMatches(matches)
		}
	}
	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Finishing scan for repo: %s, calling SearchWaitGroup.Done()\n", repo.Repo)
	}
	SearchWaitGroup.Done()
}

// MatchKeywords takes a string and checks if it contains sensitive information using pattern matching.
func MatchKeywords(source string) (matches []*Match) {
	if GetFlags().NoKeywords || source == "" {
		return matches
	}

	// Pre-allocate the matches slice to reduce reallocations
	// Start with a reasonable capacity based on typical match counts
	matches = make([]*Match, 0, 10)

	// Loop over regexes from database
	for _, regex := range GetFlags().TextRegexes {
		// Skip if no pattern available
		if regex.Pattern == nil {
			continue
		}

		// Find all matches in the source
		matchIndices := regex.Pattern.FindAllIndex([]byte(source), -1)
		expressionStr := regex.Pattern.String()

		for _, matchIndex := range matchIndices {
			matchText := source[matchIndex[0]:matchIndex[1]]

			shouldMatch := !regex.SmartFiltering
			if regex.SmartFiltering {
				if Entropy(matchText) > 3.5 {
					shouldMatch = !(containsSequence(matchText) || containsCommonWord(matchText))
				}
			}

			if shouldMatch {
				line := GetLine(source, matchText)

				// Get a Match from the pool instead of creating a new one
				match := GetMatch()
				match.Text = matchText
				match.Expression = expressionStr
				match.Line = line

				// Add attributes - reuse existing slice
				match.Attributes = append(match.Attributes, regex.ID, regex.Description)

				matches = append(matches, match)
			}
		}
	}

	return matches
}

// MatchCustomRegex matches a string against a slice of regexes.
func MatchCustomRegex(source string) (matches []*Match) {
	if source == "" {
		return matches
	}

	// Pre-allocate the matches slice
	matches = make([]*Match, 0, 5)

	for _, regex := range customRegexes {
		// Find all match indices instead of just strings
		matchIndices := regex.FindAllIndex([]byte(source), -1)

		for _, matchIndex := range matchIndices {
			matchText := source[matchIndex[0]:matchIndex[1]]
			line := GetLine(source, matchText)

			// Get a Match from the pool instead of creating a new one
			match := GetMatch()
			match.Text = matchText
			match.Expression = regex.String()
			match.Line = line

			// Add attributes - reuse existing slice
			match.Attributes = append(match.Attributes, "regex")

			matches = append(matches, match)
		}
	}

	return matches
}

// MatchFileExtensions matches interesting file extensions.
func MatchFileExtensions(source string, result RepoSearchResult) (matches []*Match) {
	if GetFlags().NoFiles || source == "" {
		return matches
	}

	// Pre-allocate the matches slice
	matches = make([]*Match, 0, 3)

	// Default extensions if no file is specified
	defaultExtensions := []string{
		"ipynb", "zip", "xlsx", "pptx", "docx", "pdf", "csv", "sql", "db", "sqlite",
		"env", "properties", "config", "conf", "ini",
		"bak", "backup", "old", "tmp", "temp", "log", "logs", "pkl",
	}

	// Get extensions from file if specified
	var extensions []string
	if GetFlags().FileExtensions != "" {
		extensions = GetFileLines(GetFlags().FileExtensions)
	} else {
		extensions = defaultExtensions
	}

	// Build regex pattern from extensions
	extPattern := strings.Join(extensions, "|")
	regexString := fmt.Sprintf("(?i)\\.(%s)$", extPattern)
	regex := regexp.MustCompile(regexString)

	// Find all match indices
	matchIndices := regex.FindAllIndex([]byte(source), -1)

	for _, matchIndex := range matchIndices {
		matchText := source[matchIndex[0]:matchIndex[1]]
		line := GetLine(source, matchText)

		// Get a Match from the pool instead of creating a new one
		match := GetMatch()
		match.Text = matchText
		match.Expression = regex.String()
		match.Line = line

		// Add attributes - reuse existing slice
		match.Attributes = append(match.Attributes, "interesting_filename")

		matches = append(matches, match)
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
	return Line{Text: source[patternIndex+i : patternIndex+j], MatchIndex: Abs(i), MatchEndIndex: j + Abs(i)}
}

// PrintContextLine pretty-prints the line of a Match, with the result highlighted.
func PrintContextLine(line Line) {
	red := color.New(color.FgRed).SprintFunc()
	fmt.Printf("%s%s%s\n",
		line.Text[:line.MatchIndex],
		red(line.Text[line.MatchIndex:line.MatchEndIndex]),
		line.Text[line.MatchEndIndex:])
}

// PrintPatternLine pretty-prints the regex used to find the leak
func PrintPatternLine(match *Match) {
	color.New(color.Faint).Println("pattern:   " + match.Expression)
}

func PrintAttributes(match *Match) {
	color.New(color.Faint).Printf("tags:      %s\n", strings.Join(match.Attributes, ", "))
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

// GetResultLink returns a link to the result.
func GetResultLink(result RepoSearchResult, match *Match) string {
	if result.Source == "gist" {
		return "https://gist.github.com/" + result.Raw
	}
	return result.URL
}

// GetMatchesForString runs pattern matching and scoring checks on the given string
// and returns the matches.
func GetMatchesForString(source string, result RepoSearchResult, recursion bool) ([]*Match, int) {
	// Pre-allocate the matches slice to reduce reallocations
	matches := make([]*Match, 0, 10)
	score := 0

	// Undecode any base64 and run again
	base64Regex := "\\b[a-zA-Z0-9/+]{8,}={0,2}\\b"
	regex := regexp.MustCompile(base64Regex)
	// fmt.Println(result)
	base64_score := 0
	var base64Strings [][]int
	// fmt.Println("RECURSION", recursion)
	if recursion {
		base64Strings = regex.FindAllStringIndex(source, -1)
		for _, indices := range base64Strings {
			match := source[indices[0]:indices[1]]
			decodedBytes, err := base64.StdEncoding.DecodeString(match)
			if err == nil && isPrintable(decodedBytes) {
				decodedStr := string(decodedBytes)
				const contextSize = 20
				start := max(0, indices[0]-contextSize)
				end := min(len(source), indices[1]+contextSize)

				contextSource := source[start:indices[0]] + decodedStr + source[indices[1]:end]
				decodedMatches, new_score := GetMatchesForString(contextSource, result, false)
				base64_score += new_score
				for _, decodedMatch := range decodedMatches {
					decodedMatch.Attributes = append(decodedMatch.Attributes, "base64")
				}
				matches = append(matches, decodedMatches...)
			}
		}
	}

	if !GetFlags().NoKeywords {
		// Get pointer matches from MatchKeywords
		keywordMatches := MatchKeywords(source)
		// No need to convert to value matches, just append directly
		matches = append(matches, keywordMatches...)
		score += len(keywordMatches) * 2
	}

	if !GetFlags().NoScoring {
		for _, blacklistRegex := range []string{
			"github.com/docker/docker",
			"google.golang.org/appengine",
			"google.golang.org/grpc",
			"^package ",
			"^import ",
			"^module ",
			"\"github.com/",
			"\"golang.org/",
			"\"google.golang.org/",
		} {
			regex := regexp.MustCompile(blacklistRegex)
			if regex.MatchString(source) {
				score -= 1
			}
		}
	}
	if !GetFlags().NoScoring && base64_score > 0 {
		score += 1
	}
	if !GetFlags().NoScoring && strings.Contains(result.File, ".go") {
		score -= 1
	}
	if !GetFlags().NoScoring && result.Repo != "" && strings.Contains(strings.ToLower(result.Repo), "demo") {
		score -= 1
	}
	if !GetFlags().NoScoring && result.Repo != "" && strings.Contains(strings.ToLower(result.Repo), "tutorial") {
		score -= 1
	}
	if !GetFlags().NoScoring && strings.HasSuffix(result.File, ".java") || strings.HasSuffix(result.File, ".cs") {
		score += 1
	}
	if !GetFlags().NoScoring && (strings.Contains(strings.ToLower(result.File), "secret") || strings.Contains(strings.ToLower(result.File), "password")) {
		score += 1
	}
	if !GetFlags().NoScoring && (strings.Contains(source, "BEGIN RSA") || strings.Contains(source, "BEGIN DSA") || strings.Contains(source, "BEGIN EC")) {
		score += 2
	}
	return matches, score
}

// Additional filters based off of https://www.ndss-symposium.org/wp-content/uploads/2019/02/ndss2019_04B-3_Meli_paper.pdf

var r *regexp.Regexp

func containsCommonWord(str string) bool {
	if r == nil {
		r = regexp.MustCompile("(?i)(" + strings.Join(getProgrammingWords(), "|") + ")")
	}
	matches := r.FindAllString(str, -1)
	sumOfLengths := 0
	for _, match := range matches {
		sumOfLengths += len(match)
	}

	return float64(sumOfLengths)/float64(len(str)) < 0.5
}

func containsSequence(str string) bool {
	b := []byte(strings.ToLower(str))
	matches := 0
	for i := 1; i < len(b); i++ {
		if b[i] == b[i-1] || b[i] == b[i-1]-1 || b[i] == b[i-1]+1 {
			matches++
		}
	}
	// fmt.Println(float64(matches) / float64(len(b)))
	// over half of the characters in the string were a sequence
	return float64(matches)/float64(len(b)) > 0.5
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isPrintable(data []byte) bool {
	for _, b := range data {
		if (b < 32 || b > 126) && b != '\n' && b != '\t' { // Allow \n and \t
			return false
		}
	}
	return true
}
