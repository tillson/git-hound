package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/waigani/diffparser"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

var queue []RepoSearchResult
var reposStored = 0
var finishedRepos []string

// Dig into the secrets of a repo
func Dig(result RepoSearchResult) (matches []Match) {
	var repo *git.Repository
	var err error

	disk := false
	if _, err = os.Stat("/tmp/githound/" + result.Repo); os.IsNotExist(err) {
		repo, err = git.PlainClone("/tmp/githound/"+result.Repo, false, &git.CloneOptions{
			URL:          "https://github.com/" + result.Repo,
			SingleBranch: true,
			Depth:        20,
		})
	} else {
		repo, err = git.PlainOpen("/tmp/githound/" + result.Repo)
		disk = true
	}
	if err != nil {
		if GetFlags().Debug {
			if disk {
				fmt.Println("Error opening repo from disk: " + result.Repo)
			} else {
				fmt.Println("Error cloning repo: " + result.Repo)
			}
			fmt.Println(err)
		}
		return
	}
	reposStored++
	if reposStored%20 == 0 {
		size, err := DirSize("/tmp/githound")
		if err != nil {
			log.Fatal(err)
		}
		if size > 50e+6 {
			ClearFinishedRepos()
		}
	}

	ref, err := repo.Head()
	if err != nil {
		if GetFlags().Debug {
			fmt.Println("Error accessing repo head: " + result.Repo)
			fmt.Println(err)
		}
		return
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		if GetFlags().Debug {
			fmt.Println("Error getting commit object: " + result.Repo)
			fmt.Println(err)
		}
		return
	}

	commitIter, err := repo.Log(&git.LogOptions{From: commit.Hash})

	lastHash, err := commit.Tree()
	if err != nil {
		log.Fatal(err)
	}
	matchMap := make(map[Match]bool)
	var waitGroup sync.WaitGroup

	commitIter.ForEach(
		func(c *object.Commit) error {
			commitTree, err := c.Tree()
			if err != nil {
				return err
			}
			diffMatches := ScanDiff(lastHash, commitTree, result)
			for _, match := range diffMatches {
				if !matchMap[match] {
					matchMap[match] = true
					match.Commit = c.Hash.String()
					matches = append(matches, match)
				}
			}
			lastHash = commitTree
			return nil
		})
	if err != nil {
		log.Println(err)
	}
	waitGroup.Wait()
	// fmt.Println("finished scanning repo " + result.Repo)
	finishedRepos = append(finishedRepos, result.Repo)
	return matches
}

// ScanDiff finds secrets in the diff between two Git trees.
func ScanDiff(from *object.Tree, to *object.Tree, result RepoSearchResult) (matches []Match) {
	if from == to || from == nil || to == nil {
		return
	}
	diff, err := from.Diff(to)
	if err != nil {
		log.Fatal(err)
	}
	for _, change := range diff {
		if change == nil {
			continue
		}
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

		keywordMatches := MatchKeywords(patchStr, result)
		for _, diffFile := range diffData.Files {
			for _, match := range MatchFileExtensions(diffFile.NewName, result) {
				keywordMatches = append(keywordMatches, match)
			}
		}
		keywordMatches = append(keywordMatches)
		if GetFlags().APIKeys {
			apiMatches := MatchAPIKeys(patchStr, result)
			for _, r := range apiMatches {
				matches = append(matches, r)
			}
		}
		for _, r := range keywordMatches {
			matches = append(matches, r)
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

// ClearFinishedRepos deletes the stored repos that have already been analyzed.
func ClearFinishedRepos() {
	for _, repoString := range finishedRepos {
		os.RemoveAll("/tmp/githound/" + repoString)
	}
	finishedRepos = nil
}

// ClearRepoStorage deletes all stored repos from the disk.
func ClearRepoStorage() {
	os.RemoveAll("/tmp/githound")
}
