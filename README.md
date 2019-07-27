# GitHound

A batch-catching, pattern-matching, patch-attacking secret snatcher.
**This project is intended to be used for educational purposes.**

![GitHound](assets/logo.png)

GitHound pinpoints exposed API keys on GitHub using pattern matching, commit history searching, and a unique result scoring system. It scrapes GitHub and Gist code-search results for repos, downloads the matching file, and regex searches the file. With the `--dig` flag, GitHound will analyze the commit history of unpopular repos with few stars, finding secrets that were not properly deleted.

## Usage

`echo "tillsongalloway.com" | git-hound` or `git-hound --subdomain-file subdomains.txt`

### Flags

* `--subdomain-file` - The file with the subdomains
* `--dig` - Clone and search the commit histories of unpopular repositories
* `--many-results` - Use result sorting and filtering hack to scrape more than 100 pages of results
* `--results-only` - Print only regexed results to stdout. Useful for piping custom regex matches into another script
* `--no-repos` - Don't search repos
* `--no-gists` - Don't search Gists
* `--threads` - Specify max number of threads for the commit digger to use.
* `--regex-file` - Supply a custom regex file
* `--language-file` - Supply a custom file with languages to search.
* `--config-file` - Custom config file (default is `config.yml`)
* `--pages` - Max pages to search (default is 100, the page maximum)
* `--no-scoring` - Don't use scoring to filter out false positives
* `--no-api-keys` - Don't perform generic API key searching. GitHound uses common API key patterns, context clues, and a Shannon entropy filter to find potential exposed API keys.
* `--no-files` - Don't flag interesting file extensions
* `--only-filtered` - Only search filtered queries (languages)
* `--debug` - Print verbose debug messages.

## Setup

1. Download the [latest release of GitHound](https://github.com/tillson/git-hound/releases)
2. `echo "tillsongalloway.com" | git-hound`

## Related tools

* [GitRob](https://github.com/michenriksen/gitrob) is an excellent tool that specifically targets an organization or user's owned repositories for secrets.
