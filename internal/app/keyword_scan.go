package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/viper"
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

var customRegexes []*regexp.Regexp
var loadedRegexes = false

// ScanAndPrintResult scans and prints information about a search result.
func ScanAndPrintResult(client *http.Client, repo RepoSearchResult) {
	if !GetFlags().FastMode {
		base := GetRawURLForSearchResult(repo)
		data, err := DownloadRawFile(client, base, repo)
		if err != nil {
			log.Fatal(err)
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
		matches, score := GetMatchesForString(repo.Contents, repo, true)
		if (GetFlags().DigCommits || GetFlags().DigRepo) && RepoIsUnpopular(client, repo) && score > -1 && !scannedRepos[repo.Repo] {
			scannedRepos[repo.Repo] = true
			regex := regexp.MustCompile("(?i)(alexa|urls|adblock|domain|dns|top1000|top\\-1000|httparchive" +
				"|blacklist|hosts|ads|whitelist|crunchbase|tweets|tld|hosts\\.txt" +
				"|host\\.txt|aquatone|recon\\-ng|hackerone|bugcrowd|xtreme|list|tracking|malicious|ipv(4|6)|host\\.txt)")
			fileNameMatches := regex.FindAllString(repo.Repo, -1)
			if len(fileNameMatches) == 0 {
				dig_matches := Dig(repo)
				for _, match := range dig_matches {
					match.Attributes = append(match.Attributes, "dig-files")
					matches = append(matches, match)
				}
			}
		}

		if len(matches) > 0 {
			// fmt.Println((repo.Raw), "score:", score)

			token := viper.GetString("github_access_token")
			client := github.NewClient(nil).WithAuthToken(token)
			if client != nil {
				// gh_repo_obj, _, err := client.Repositories.Get(strings.Split(repo.Repo, "/")[0], strings.Split(repo.Repo, "/")[1])
				// get repo's commits
				commits, _, err := client.Repositories.ListCommits(context.Background(), strings.Split(repo.Repo, "/")[0], strings.Split(repo.Repo, "/")[1], &github.CommitsListOptions{
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
				resultPayload := map[string]interface{}{
					"repo":              resultRepoURL,
					"context":           result.Line.Text,
					"match":             result.Line.Text[result.Line.MatchIndex:result.Line.MatchEndIndex],
					"attributes":        result.Attributes,
					"file_last_updated": repo.SourceFileLastUpdated,
					"file_last_author":  repo.SourceFileLastAuthorEmail,
					"url":               GetResultLink(repo, result),
				}
				matchKey := fmt.Sprintf("%s|%s", resultPayload["match"], resultRepoURL)
				if uniqueMatches[matchKey] {
					continue
				}
				uniqueMatches[matchKey] = true

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
						color.New(color.Faint).Println(GetResultLink(repo, result))
					}
				}
				if GetFlags().Dashboard && InsertKey != "" {
					resultJSON, err := json.Marshal(resultPayload)
					if err == nil {
						SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "result": %s}`, InsertKey, string(resultJSON)))
					} else {
						color.Red("Error marshalling result to JSON: %v", err)
					}
				}
			}
			if GetFlags().Debug {
				fmt.Println("Finished scanning " + repo.Repo + "...")
			}
		}
	}
	SearchWaitGroup.Done()
}

// MatchKeywords takes a string and checks if it contains sensitive information using pattern matching.
func MatchKeywords(source string) (matches []Match) {
	if GetFlags().NoKeywords || source == "" {
		return matches
	}

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
				matches = append(matches, Match{
					Attributes: []string{regex.ID, regex.Description},
					Text:       matchText,
					Expression: expressionStr,
					Line:       line,
				})
			}
		}
	}

	return matches
}

// MatchCustomRegex matches a string against a slice of regexes.
func MatchCustomRegex(source string) (matches []Match) {
	if source == "" {
		return matches
	}

	for _, regex := range customRegexes {
		// Find all match indices instead of just strings
		matchIndices := regex.FindAllIndex([]byte(source), -1)

		for _, matchIndex := range matchIndices {
			matchText := source[matchIndex[0]:matchIndex[1]]
			line := GetLine(source, matchText)

			matches = append(matches, Match{
				Attributes: []string{"regex"},
				Text:       matchText,
				Expression: regex.String(),
				Line:       line,
			})
		}
	}

	return matches
}

// MatchFileExtensions matches interesting file extensions.
func MatchFileExtensions(source string, result RepoSearchResult) (matches []Match) {
	if GetFlags().NoFiles || source == "" {
		return matches
	}

	regexString := "(?i)(vim_settings\\.xml)(\\.(zip|env|docx|xlsx|pptx|pdf))$"
	regex := regexp.MustCompile(regexString)

	// Find all match indices instead of using FindAllStringSubmatch
	matchIndices := regex.FindAllIndex([]byte(source), -1)

	for _, matchIndex := range matchIndices {
		matchText := source[matchIndex[0]:matchIndex[1]]
		line := GetLine(source, matchText)

		matches = append(matches, Match{
			Attributes: []string{"interesting_filename"},
			Text:       matchText,
			Expression: regex.String(),
			Line:       line,
		})
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
func PrintPatternLine(match Match) {
	fmt.Printf("RegEx Pattern: %s\n", match.Expression)
}

func PrintAttributes(match Match) {
	fmt.Printf("Attributes: %v\n", match.Attributes)
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
func GetResultLink(result RepoSearchResult, match Match) string {
	if match.Commit != "" {
		return "https://github.com/" + result.Repo + "/commit/" + match.Commit
	} else {
		file := match.File
		if file == "" {
			file = result.File
		}
		return result.URL
	}
}

// GetMatchesForString runs pattern matching and scoring checks on the given string
// and returns the matches.
func GetMatchesForString(source string, result RepoSearchResult, recursion bool) (matches []Match, score int) {

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
				for _, match := range decodedMatches {
					match.Attributes = append(match.Attributes, "base64")
				}
				matches = append(matches, decodedMatches...)
			}
		}
	}

	if !GetFlags().NoKeywords {
		for _, match := range MatchKeywords(source) {
			matches = append(matches, match)
			score += 2
		}
	}
	if !GetFlags().NoScoring {
		matched, err := regexp.MatchString("(?i)(h1domains|bugbounty|bug\\-bounty|bounty\\-targets|url_short|url_list|alexa)", result.Repo+result.File)
		CheckErr(err)
		if matched {
			score -= 3
		}
		matched, err = regexp.MatchString("(?i)(\\.md|\\.csv)$", result.File)
		CheckErr(err)
		if matched {
			score -= 2
		}
		matched, err = regexp.MatchString("^vim_settings.xml$", result.File)
		CheckErr(err)
		if matched {
			score += 5
		}
		if len(matches) > 0 {
			matched, err = regexp.MatchString("(?i)\\.(json|yml|py|rb|java)$", result.File)
			CheckErr(err)
			if matched {
				score++
			}
			matched, err = regexp.MatchString("(?i)\\.(xlsx|docx|doc)$", result.File)
			CheckErr(err)
			if matched {
				score += 3
			}
		}
		// regex := regexp.MustCompile("(alexa|urls|adblock|domain|dns|top1000|top\\-1000|httparchive" +
		// 	"|blacklist|hosts|ads|whitelist|crunchbase|tweets|tld|hosts\\.txt" +
		// 	"|host\\.txt|aquatone|recon\\-ng|hackerone|bugcrowd|xtreme|list|tracking|malicious|ipv(4|6)|host\\.txt)")
		// fileNameMatches := regex.FindAllString(result.File, -1)
		// CheckErr(err)
		// if len(fileNameMatches) > 0 {
		// 	score -= int(math.Pow(2, float64(len(fileNameMatches))))
		// }
		if score <= 0 && !GetFlags().NoScoring {
			matches = nil
		}
	}
	if GetFlags().NoScoring {
		score = 1000
	}

	// score = score // + (base64_score - len(base64Strings)*score)

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
