package app

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// Cache for already scanned files with size limit
	fileCache      = make(map[string]bool)
	fileCacheMutex sync.Mutex
	fileCacheSize  = 0
	maxCacheSize   = 10000 // Limit cache to 10k entries to prevent memory leaks
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
				// Skip files larger than 5MB (but still check extensions in processFile)
				if info.Size() > 5*1024*1024 {
					if GetFlags().Debug {
						fmt.Printf("[DEBUG] File will skip content processing (>5MB): %s (%d bytes)\n", path, info.Size())
					}
					// Still add to files list so extension checks can be performed
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

			// Optimize concurrency based on available threads and file count
			maxConcurrency := GetFlags().Threads
			if maxConcurrency <= 0 {
				maxConcurrency = 10 // Default fallback
			}

			// For large file sets, increase concurrency but cap it to prevent overwhelming the system
			if len(files) > 100 {
				maxConcurrency = min(maxConcurrency*2, 50) // Cap at 50 concurrent operations
			}

			semaphore := make(chan struct{}, maxConcurrency)
			processedFiles := 0

			if GetFlags().Debug {
				fmt.Printf("[DEBUG] Processing %d files with %d concurrent workers\n", len(files), maxConcurrency)
			}

			for _, file := range files {
				wg.Add(1)
				go func(file string) {
					defer wg.Done()
					semaphore <- struct{}{}
					defer func() { <-semaphore }()

					// Check cache first
					if !addToFileCache(file) {
						if GetFlags().Debug {
							fmt.Printf("[DEBUG] Skipping cached file: %s\n", file)
						}
						matchesChan <- nil
						return
					}

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

	// Get file info first to check size before reading
	fileInfo, err := os.Stat(file)
	if err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error getting file info %s: %v\n", file, err)
		}
		return nil
	}

	fileResult := result
	fileResult.File = file
	score := 0
	var newMatches []*Match

	// Always check file extensions first (regardless of content or size)
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

	// Skip content processing for files larger than 5MB to prevent memory issues
	if fileInfo.Size() > 5*1024*1024 {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Skipping content processing for large file (>5MB): %s (%d bytes)\n", file, fileInfo.Size())
		}
		// Return extension matches if any, otherwise nil
		if score > 0 {
			for _, match := range newMatches {
				relPath := strings.Join(strings.Split(file[len("/tmp/githound/"):], "/")[2:], "/")
				match.CommitFile = relPath
				match.File = relPath
			}
			return newMatches
		}
		return nil
	}

	// Use streaming approach for large files, direct reading for small files
	var data []byte
	if fileInfo.Size() > 1024*1024 { // 1MB threshold
		// Stream read for large files in chunks
		data, err = readFileStreamingChunked(file, 1024*1024, 50*1024) // 1MB chunks with 50KB overlap
	} else {
		// Direct read for small files
		data, err = ioutil.ReadFile(file)
	}

	if err != nil {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Error reading file %s: %v\n", file, err)
		}
		// Return extension matches if any, otherwise nil
		if score > 0 {
			for _, match := range newMatches {
				relPath := strings.Join(strings.Split(file[len("/tmp/githound/"):], "/")[2:], "/")
				match.CommitFile = relPath
				match.File = relPath
			}
			return newMatches
		}
		return nil
	}

	// Early binary detection using first 1KB
	isBinary := isBinaryFile(data)
	if isBinary {
		if GetFlags().Debug {
			fmt.Printf("[DEBUG] Skipping content processing for binary file: %s\n", file)
		}
		// Return extension matches if any, otherwise nil
		if score > 0 {
			for _, match := range newMatches {
				relPath := strings.Join(strings.Split(file[len("/tmp/githound/"):], "/")[2:], "/")
				match.CommitFile = relPath
				match.File = relPath
			}
			return newMatches
		}
		return nil
	}

	// Only do full text search if we haven't found anything yet
	if score == 0 {
		// Process content efficiently
		content, binaryRatio := extractTextContent(data)

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

// readFileStreamingChunked reads a file in chunks with overlap to avoid missing matches at chunk boundaries
func readFileStreamingChunked(filePath string, chunkSize int64, overlapSize int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := fileInfo.Size()

	// For files smaller than chunk size, just read the whole thing
	if fileSize <= chunkSize {
		return ioutil.ReadAll(file)
	}

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Streaming file %s in chunks: size=%d, chunk=%d, overlap=%d\n",
			filepath.Base(filePath), fileSize, chunkSize, overlapSize)
	}

	// Pre-allocate buffer with estimated capacity
	estimatedChunks := int(fileSize/(chunkSize-overlapSize)) + 1
	totalCapacity := int(fileSize) + (estimatedChunks * int(overlapSize))
	result := make([]byte, 0, totalCapacity)

	// Read chunks with overlap
	offset := int64(0)
	firstChunk := true
	chunkCount := 0

	for offset < fileSize {
		// Calculate chunk size for this iteration
		currentChunkSize := chunkSize
		if offset+chunkSize > fileSize {
			currentChunkSize = fileSize - offset
		}

		// Seek to the current position
		_, err = file.Seek(offset, 0)
		if err != nil {
			return nil, err
		}

		// Read the chunk
		chunk := make([]byte, currentChunkSize)
		bytesRead, err := io.ReadFull(file, chunk)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}

		// Trim to actual bytes read
		chunk = chunk[:bytesRead]
		chunkCount++

		if firstChunk {
			// First chunk: add entire chunk
			result = append(result, chunk...)
			firstChunk = false
		} else {
			// Subsequent chunks: add only the non-overlapping part
			// Skip the first overlapSize bytes (which were already included from previous chunk)
			if int64(len(chunk)) > overlapSize {
				result = append(result, chunk[overlapSize:]...)
			}
		}

		// Move to next chunk position (accounting for overlap)
		offset += chunkSize - overlapSize

		// Safety check to prevent infinite loops
		if offset >= fileSize {
			break
		}
	}

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Completed streaming %s: %d chunks, %d bytes read\n",
			filepath.Base(filePath), chunkCount, len(result))
	}

	return result, nil
}

// readFileStreaming reads a file in chunks to avoid loading large files entirely into memory
func readFileStreaming(filePath string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read only up to maxBytes
	reader := io.LimitReader(file, maxBytes)
	return ioutil.ReadAll(reader)
}

// isBinaryFile efficiently detects if a file is binary by checking the first 1KB
func isBinaryFile(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Quick check for single byte files
	if len(data) == 1 && data[0] == 0 {
		return true
	}

	// Check first 1KB for null bytes
	checkSize := 1024
	if len(data) < checkSize {
		checkSize = len(data)
	}

	nullCount := 0
	for i := 0; i < checkSize; i++ {
		if data[i] == 0 {
			nullCount++
		}
	}

	// If more than 10% are null bytes, consider it binary
	return float32(nullCount)/float32(checkSize) > 0.1
}

// extractTextContent extracts ASCII text content from binary data and calculates the binary ratio.
// It performs a single pass through the data to filter ASCII characters and count the ratio.
func extractTextContent(data []byte) (string, float32) {
	if len(data) == 0 {
		return "", 1.0
	}

	// Pre-allocate slice with capacity to avoid reallocations
	ascii := make([]byte, 0, len(data))
	asciiCount := 0

	// Single pass through data to count ASCII characters and build string
	for _, b := range data {
		if b > 0 && b < 127 {
			ascii = append(ascii, b)
			asciiCount++
		}
	}

	binaryRatio := float32(asciiCount) / float32(len(data))
	content := string(ascii)

	return content, binaryRatio
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

// addToFileCache adds a file to the cache with size management
func addToFileCache(file string) bool {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	// Check if already in cache
	if fileCache[file] {
		return false
	}

	// Add to cache
	fileCache[file] = true
	fileCacheSize++

	// Cleanup if cache is too large
	if fileCacheSize > maxCacheSize {
		cleanupFileCache()
	}

	return true
}

// cleanupFileCache removes old entries from the file cache
func cleanupFileCache() {
	// Simple cleanup: clear half the cache when it gets too large
	entriesToRemove := maxCacheSize / 2

	// Remove random entries (in practice, this will remove older entries due to map iteration order)
	for file := range fileCache {
		delete(fileCache, file)
		fileCacheSize--
		entriesToRemove--
		if entriesToRemove <= 0 {
			break
		}
	}

	if GetFlags().Debug {
		fmt.Printf("[DEBUG] Cleaned file cache, remaining entries: %d\n", fileCacheSize)
	}
}

// isFileCached checks if a file is in the cache
func isFileCached(file string) bool {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()
	return fileCache[file]
}
