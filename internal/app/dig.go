package app

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/waigani/diffparser"
)

var (
	queue         []RepoSearchResult
	reposStored   = 0
	finishedRepos []string
	reposMutex    sync.Mutex
	// Cache for already scanned files
	fileCache      = make(map[string]bool)
	fileCacheMutex sync.Mutex
	// Skip these file extensions
	skipExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true,
		".mp3": true, ".mp4": true, ".mov": true, ".wav": true, ".ogg": true,
		".ttf": true, ".woff": true, ".woff2": true, ".eot": true,
	}
	// Skip these directories
	skipFolders = map[string]bool{
		"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
		"coverage": true, "test-results": true, "logs": true, "tmp": true,
		".cache": true, ".m2": true, ".gradle": true, "site-packages": true,
		".git": true, ".svn": true, ".hg": true, ".bzr": true,
		"__pycache__": true, ".pytest_cache": true, ".tox": true,
		"bower_components": true, "jspm_packages": true, "packages": true,
		".nuget": true, "bin": true, "obj": true, "docs": true, "sdks": true,
	}
)

// Dig into the secrets of a repo
func Dig(result RepoSearchResult) []*Match {
	startTime := time.Now()
	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Starting Dig for repo: %s\n", result.Repo)
	}

	// Execute digHelper directly instead of using worker pool to avoid deadlock
	matches := digHelper(result)

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Completed Dig for repo: %s in %v\n", result.Repo, time.Since(startTime))
	}
	return matches
}

func digHelper(result RepoSearchResult) []*Match {
	startTime := time.Now()
	matches := make([]*Match, 0, 10)
	matchMap := make(map[string]bool)

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Starting digHelper for repo: %s\n", result.Repo)
	}

	// Check if this repo has already been processed
	reposMutex.Lock()
	for _, finishedRepo := range finishedRepos {
		if finishedRepo == result.Repo {
			reposMutex.Unlock()
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Skipping already processed repo: %s\n", result.Repo)
			}
			return matches
		}
	}
	reposMutex.Unlock()

	var repo *git.Repository
	var err error
	if _, err = os.Stat("/tmp/githound/" + result.Repo); os.IsNotExist(err) {
		cloneStart := time.Now()
		context, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Cloning repo: %s\n", result.Repo)
		}

		repo, err = git.PlainCloneContext(context, "/tmp/githound/"+result.Repo, false, &git.CloneOptions{
			URL:          "https://github.com/" + result.Repo,
			SingleBranch: true,
			Depth:        1, // Only get the current state, no history
		})

		if err != nil {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Error cloning repo %s: %v\n", result.Repo, err)
			}
			return matches
		}

		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Clone completed in %v\n", time.Since(cloneStart))
		}

		// Update repo storage stats
		reposMutex.Lock()
		reposStored++
		if reposStored%10 == 0 {
			size, err := DirSize("/tmp/githound")
			if err != nil {
				log.Fatal(err)
			}
			if size > 20e+6 {
				reposMutex.Unlock()
				if GetFlags().Debug {
					fmt.Printf("[DEBUG] Storage size exceeded, clearing finished repos\n")
				}
				ClearFinishedRepos()
				reposMutex.Lock()
			}
		}
		reposMutex.Unlock()

		ref, err := repo.Head()
		if err != nil {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Error accessing repo head %s: %v\n", result.Repo, err)
			}
			return matches
		}

		if GetFlags().DigRepo {
			scanStart := time.Now()
			root := "/tmp/githound/" + result.Repo
			var files []string
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Starting file walk for %s\n", result.Repo)
			}

			err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					// Check if this directory should be skipped
					dirName := filepath.Base(path)
					if skipFolders[dirName] {
						if GetFlags().Debug {
							fmt.Printf("[DEBUG] Skipping directory: %s\n", path)
						}
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasPrefix(path, root+"/.git/") {
					return nil
				}
				// Skip files larger than 10MB
				if info.Size() > 10*1024*1024 {
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Skipping large file (>10MB): %s (%d bytes)\n", path, info.Size())
					}
					return nil
				}
				ext := strings.ToLower(filepath.Ext(path))
				if skipExtensions[ext] {
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Skipping file with blacklisted extension: %s\n", path)
					}
					return nil
				}
				files = append(files, path)
				return nil
			})
			if err != nil {
				fmt.Printf("[DEBUG] Error walking directory: %v\n", err)
			}

			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Found %d files to scan in %v\n", len(files), time.Since(scanStart))
			}

			// Process files in parallel
			var wg sync.WaitGroup
			matchesChan := make(chan []*Match, len(files))
			semaphore := make(chan struct{}, 10) // Limit concurrent file processing
			processedFiles := 0

			for _, file := range files {
				wg.Add(1)
				go func(file string) {
					defer wg.Done()
					semaphore <- struct{}{}
					defer func() { <-semaphore }()

					// Check cache first
					fileCacheMutex.Lock()
					if fileCache[file] {
						fileCacheMutex.Unlock()
						if GetFlags().Debug {
							fmt.Printf("[DEBUG] Skipping cached file: %s\n", file)
						}
						matchesChan <- nil
						return
					}
					fileCache[file] = true
					fileCacheMutex.Unlock()

					fileStart := time.Now()
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Scanning file: %s\n", file)
					}
					fileMatches := processFile(file, result)
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] Processed file %s in %v\n", file, time.Since(fileStart))
					}
					matchesChan <- fileMatches
				}(file)
			}

			// Collect results
			go func() {
				wg.Wait()
				close(matchesChan)
			}()

			for fileMatches := range matchesChan {
				processedFiles++
				if fileMatches != nil {
					for _, match := range fileMatches {
						// For dug files, we want to show each file separately
						// Only deduplicate exact matches from the same file
						matchKey := fmt.Sprintf("%s|%s|%s", match.Text, match.File, match.Line.Text)
						if !matchMap[matchKey] {
							matchMap[matchKey] = true
							// Add dig-files attribute if not already present
							hasDigFiles := false
							for _, attr := range match.Attributes {
								if attr == "dig-files" {
									hasDigFiles = true
									break
								}
							}
							if !hasDigFiles {
								match.Attributes = append(match.Attributes, "dig-files")
							}
							matches = append(matches, match)
						}
					}
				}
			}

			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Processed %d files in %v\n", processedFiles, time.Since(scanStart))
			}
		}

		if GetFlags().DigCommits {
			commitStart := time.Now()
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Starting commit scanning for %s\n", result.Repo)
			}

			commit, err := repo.CommitObject(ref.Hash())
			if err != nil {
				if GetFlags().Debug {
					fmt.Printf("[DEBUG] Error getting commit object %s: %v\n", result.Repo, err)
				}
				return matches
			}

			commitIter, err := repo.Log(&git.LogOptions{From: commit.Hash})
			if err != nil {
				fmt.Printf("[DEBUG] Error getting commit log: %v\n", err)
				return matches
			}

			lastHash, err := commit.Tree()
			if err != nil {
				fmt.Printf("[DEBUG] Error getting commit tree: %v\n", err)
				return matches
			}

			number := 0
			commitIter.ForEach(func(c *object.Commit) error {
				if number > 30 {
					return nil
				}
				number++
				commitTree, err := c.Tree()
				if err != nil {
					return err
				}
				diffMatches := ScanDiff(lastHash, commitTree, result)
				for _, match := range diffMatches {
					match.Commit = c.Hash.String()
					matchKey := fmt.Sprintf("%s|%s|%s|%s", match.Text, match.File, match.Line.Text, match.Commit)
					if !matchMap[matchKey] {
						matchMap[matchKey] = true
						matches = append(matches, match)
					}
				}
				lastHash = commitTree
				return nil
			})

			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Commit scanning completed in %v\n", time.Since(commitStart))
			}
		}

		finishedRepos = append(finishedRepos, result.Repo)
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Total processing time for %s: %v\n", result.Repo, time.Since(startTime))
		}

		// Clean up the repository from local storage
		repoPath := "/tmp/githound/" + result.Repo
		if err := os.RemoveAll(repoPath); err != nil {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Error cleaning up repo %s: %v\n", result.Repo, err)
			}
		} else {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Cleaned up repo from local storage: %s\n", result.Repo)
			}
		}
	}

	return matches
}

func processFile(file string, result RepoSearchResult) []*Match {
	readStart := time.Now()
	data, err := ioutil.ReadFile(file)
	if err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error reading file %s: %v\n", file, err)
		}
		return nil
	}

	fileResult := result
	fileResult.File = file
	score := 0
	var newMatches []*Match

	// Always check file extensions first (regardless of content)
	extStart := time.Now()
	fileExtMatches := MatchFileExtensions(file, fileResult)
	for _, match := range fileExtMatches {
		newMatches = append(newMatches, match)
		score += 5
	}
	PutMatches(fileExtMatches)
	if GetFlags().Debug {
		fmt.Printf("[DEBUG] File extension check for %s took %v\n", file, time.Since(extStart))
	}

	// Improved binary detection - check first 1KB for null bytes
	isBinary := false
	if len(data) > 1024 {
		binaryCount := 0
		checkSize := 1024
		if len(data) < checkSize {
			checkSize = len(data)
		}
		for i := 0; i < checkSize; i++ {
			if data[i] == 0 {
				binaryCount++
			}
		}
		if float32(binaryCount)/float32(checkSize) > 0.1 {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Skipping text processing for binary file (too many null bytes): %s\n", file)
			}
			isBinary = true
		}
	} else if len(data) > 0 && data[0] == 0 {
		// Quick check for single byte files
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Skipping text processing for binary file (starts with null): %s\n", file)
		}
		isBinary = true
	}

	// Only do full text search if file is not binary and we haven't found anything yet
	if !isBinary && score == 0 {
		// Convert to ASCII efficiently - only if necessary
		var content string
		binaryRatio := float32(0)
		if len(data) > 0 {
			ascii := make([]byte, 0, len(data))
			for _, b := range data {
				if b > 0 && b < 127 {
					ascii = append(ascii, b)
				}
			}
			binaryRatio = float32(len(ascii)) / float32(len(data))
			content = string(ascii)
		} else {
			content = ""
			binaryRatio = 1.0
		}

		// Skip text processing if too much binary content
		if binaryRatio < 0.9 {
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Skipping text processing for file with too much binary content: %s (ratio: %.2f)\n", file, binaryRatio)
			}
		} else {
			searchStart := time.Now()
			searchMatches, searchScore := GetMatchesForString(content, result, true)
			score += searchScore
			if searchScore > -1 {
				newMatches = append(newMatches, searchMatches...)
			}
			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Text search for %s took %v\n", file, time.Since(searchStart))
			}
		}
	}

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Read and processed file %s in %v\n", file, time.Since(readStart))
	}

	if score > 1 {
		for _, match := range newMatches {
			relPath := strings.Join(strings.Split(file[len("/tmp/githound/"):], "/")[2:], "/")
			match.CommitFile = relPath
			match.File = relPath
		}
		return newMatches
	}

	return nil
}

// ScanDiff finds secrets in the diff between two Git trees.
func ScanDiff(from *object.Tree, to *object.Tree, result RepoSearchResult) (matches []*Match) {
	if from == to || from == nil || to == nil {
		return matches
	}
	diff, err := from.Diff(to)
	if err != nil {
		log.Fatal(err)
	}
	for _, change := range diff {
		if change == nil {
			continue
		}
		//temporary response to:  https://github.com/sergi/go-diff/issues/89
		//reference: https://github.com/codeEmitter/gitrob/commit/c735767e86d40a0015756a299e4daeb136c7126b
		defer func() error {
			if err := recover(); err != nil {
				return errors.New(fmt.Sprintf("Panic occurred while retrieving change content: %s", err))
			}
			return nil
		}()

		patch, err := change.Patch()
		if err != nil {
			if GetFlags().Debug {
				fmt.Println("Diff scan error: Patch error.")
				fmt.Println(err)
			}
			continue
		}
		patchStr := patch.String()
		diffData, err := diffparser.Parse(patchStr)
		if err != nil {
			log.Fatal(err)
		}
		matches, _ = GetMatchesForString(patchStr, result, true)
		for _, diffFile := range diffData.Files {
			fileExtMatches := MatchFileExtensions(diffFile.NewName, result)
			// Convert pointer matches to value matches before appending
			for _, ptrMatch := range fileExtMatches {
				matches = append(matches, ptrMatch)
			}
			// Don't forget to return the matches to the pool
			PutMatches(fileExtMatches)
		}
	}
	return matches
}

// DirSize gets the size of a diretory.
func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// ClearFinishedRepos clears finished repos from disk
func ClearFinishedRepos() {
	// Lock for thread safety
	reposMutex.Lock()
	defer reposMutex.Unlock()

	// More aggressive cleanup - remove all repos
	err := os.RemoveAll("/tmp/githound")
	if err != nil {
		fmt.Println(err)
	}

	// Reset counters
	reposStored = 0
	finishedRepos = []string{}

	// Recreate the base directory
	err = os.MkdirAll("/tmp/githound", 0755)
	if err != nil {
		fmt.Println(err)
	}
}

// ClearRepoStorage deletes all stored repos from the disk.
func ClearRepoStorage() {
	os.RemoveAll("/tmp/githound")
}
