# GitHound

A batch-catching, pattern-matching, patch-attacking secret snatcher.

![GitHound](assets/logo.png)

GitHound pinpoints exposed API keys on GitHub using pattern matching, commit history searching, and a unique result scoring system. GitHound has earned me over $3000 applied to Bug Bounty research. Corporate and Bug Bounty Hunter use cases are outlined below.

## Features

* GitHub/Gist code searching. This enables GitHound to locate sensitive information exposed across all of GitHub, uploaded by any user.
* Generic API key detection using pattern matching, context, and [Shannon entropy](<https://en.wikipedia.org/wiki/Entropy_(information_theory)>).
* Commit history digging to find improperly deleted sensitive information (for repositories with <6 stars)..
* Unique scoring system to emphasize confident results, filter out common false positives, and to optimize intensive repo digging.
* Options to build GitHound into your workflow, like custom regexes and results-only output mode.

## Usage

`echo "tillsongalloway.com" | git-hound` or `git-hound --subdomain-file subdomains.txt`

## Setup

1. Download the [latest release of GitHound](https://github.com/tillson/git-hound/releases)
2. Create a `./config.yml` or `~/.githound/config.yml` with your GitHub username and password (2FA accounts are not supported). See [config.example.yml](config.example.yml).
   1. If it's your first time using the account on the system, you may receieve an account verification email.
3. `echo "tillsongalloway.com" | git-hound`

## Use cases

### Corporate: Searching for exposed customer API keys

Knowing the pattern for a specific service's API keys enables you to search GitHub for these keys. You can then pipe matches for your custom key regex into your own script to test the API key against the service and to identify the at-risk account.

`echo "api.halcorp.biz" | githound --dig --many-results --regex-file halcorp-api-regexes.txt --results-only | python halapitester.py`

For detecting future API key leaks, GitHub offers [Push Token Scanning](https://help.github.com/en/articles/about-token-scanning) to immediately detect API keys as they are posted.

### Bug Bounty Hunters: Searching for leaked employee API tokens

My primary use for GitHound is for finding sensitive information for Bug Bounty programs. For high-profile targets, the `--many-results`  hack and `--languages` flag are useful for scraping >100 pages of results.

`echo "uberinternal.com" | githound --dig --many-results --languages common-languages.txt --threads 100`

## Flags

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

## Related tools

* [GitRob](https://github.com/michenriksen/gitrob) is an excellent tool that specifically targets an organization or user's repositories for exposed credentials and displays them on a beautiful web interface.
