package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

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

	if _, err = os.Stat("/tmp/githound/" + result.Repo); os.IsNotExist(err) {
		repo, err = git.PlainClone("/tmp/githound/"+result.Repo, false, &git.CloneOptions{
			URL: "https://github.com/" + result.Repo,
		})
	} else {
		repo, err = git.PlainOpen("/tmp/githound/" + result.Repo)
	}
	if err != nil {
		fmt.Println(err)
		return
	}
	reposStored++
	if reposStored%50 == 0 {
		size, err := DirSize("/tmp/githound")
		if err != nil {
			log.Fatal(err)
		}
		if size > 1024*1024*500 {
			ClearFinishedRepos()
		}
	}

	if err != nil {
		log.Fatal(err)
		log.Println("Unable to clone git repo")
		return
	}
	ref, err := repo.Head()
	if err != nil {
		// log.Println(err)
		return
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		log.Println(err)
		return
	}

	commitIter, err := repo.Log(&git.LogOptions{From: commit.Hash})
	lastHash, err := commit.Tree()
	if err != nil {
		log.Fatal(err)
	}
	matchMap := make(map[Match]bool)
	err = commitIter.ForEach(
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
			finishedRepos = append(finishedRepos, result.Repo)
			return nil
		})
	if err != nil {
		log.Println(err)
	}
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
		patch, err := change.Patch()
		if err != nil {
			log.Fatal(err)
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
		apiMatches := MatchAPIKeys(patchStr, result)
		for _, r := range keywordMatches {
			matches = append(matches, r)
		}
		for _, r := range apiMatches {
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
}

// ClearRepoStorage deletes all stored repos from the disk.
func ClearRepoStorage() {
	os.RemoveAll("/tmp/githound")
	fmt.Println("Cleared /tmp/githound")
}
