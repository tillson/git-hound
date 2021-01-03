# GitHound

A batch-catching, pattern-matching, patch-attacking secret snatcher.

![GitHound](assets/logo.png)

GitHound pinpoints exposed API keys and other sensitive information across all of GitHub using pattern matching, commit history searching, and a unique result scoring system. GitHound has earned me over $7500 applied to Bug Bounty research. Corporate and Bug Bounty Hunter use cases are outlined below.
More information on methodologies is available in the [accompanying blog post](https://tillsongalloway.com/finding-sensitive-information-on-github/).

## Features

- GitHub/Gist code searching. This enables GitHound to locate sensitive information exposed across all of GitHub, uploaded by any user.
- Generic API key detection using pattern matching, context, [Shannon entropy](<https://en.wikipedia.org/wiki/Entropy_(information_theory)>), and other heuristics
- Commit history digging to find improperly deleted sensitive information (for repositories with <6 stars)
- Scoring system to emphasize confident results, filter out common false positives, and to optimize intensive repo digging
- Base64 detection and decoding
- Options to build GitHound into your workflow, like custom regexes and results-only output mode

## Usage

`echo "\"tillsongalloway.com\"" | git-hound` or `git-hound --subdomain-file subdomains.txt`

## Setup

1. Download the [latest release of GitHound](https://github.com/tillson/git-hound/releases)
2. Create a `./config.yml` or `~/.githound/config.yml` with your GitHub username and password. Optionally, include your 2FA TOTP seed. See [config.example.yml](config.example.yml).
   1. If it's your first time using the account on the system, you may receieve an account verification email.
3. `echo "tillsongalloway.com" | git-hound`

### Two-Factor Authentication

If GitHound is logged into your GitHub account, two-factor authentication may kick in. You can pass 2FA codes to GitHound with `--otp-code`.
Otherwise, GitHound will prompt you for it when it starts up.
You can also [supply your 2FA seed](https://github.com/tillson/git-hound/pull/24) in the config and you'll never have to worry about 2FA again.
Grab the 2FA seed by decoding the barcode that GitHub shows during the 2FA setup process.

## Use cases

### Corporate: Searching for exposed customer API keys

Knowing the pattern for a specific service's API keys enables you to search GitHub for these keys. You can then pipe matches for your custom key regex into your own script to test the API key against the service and to identify the at-risk account.

`echo "api.halcorp.biz" | githound --dig-files --dig-commits --many-results --regex-file halcorp-api-regexes.txt --results-only | python halapitester.py`

For detecting future API key leaks, GitHub offers [Push Token Scanning](https://help.github.com/en/articles/about-token-scanning) to immediately detect API keys as they are posted.

### Bug Bounty Hunters: Searching for leaked employee API tokens

My primary use for GitHound is for finding sensitive information for Bug Bounty programs. For high-profile targets, the `--many-results` hack and `--languages` flag are useful for scraping >100 pages of results.

`echo "\"uberinternal.com\"" | githound --dig-files --dig-commits --many-results --languages common-languages.txt --threads 100`

## How does GitHound find API keys?

https://github.com/tillson/git-hound/blob/master/internal/app/keyword_scan.go
GitHound finds API keys with a combination of exact regexes for common services like Slack and AWS and a context-sensitive generic API regex. This finds long strings that look like API keys surrounded by keywords like "Authorization" and "API-Token". GitHound assumes that these are false positives and then proves their legitimacy with Shannon entropy, dictionary word checks, uniqueness calculations, and encoding detection. GitHound then outputs high certainty positives.
For files that encode secrets, decodes base64 strings and searches the encoded strings for API keys.

Check out this [blog post](https://tillsongalloway.com/finding-sensitive-information-on-github/) for more details on use cases and methodologies.

## Flags

- `--subdomain-file` - The file with the subdomains
- `--dig-files` - Clone and search the repo's files for results
- `--dig-commits` - Clone and search the repo's commit history for results
- `--many-results` - Use result sorting and filtering hack to scrape more than 100 pages of results
- `--results-only` - Print only regexed results to stdout. Useful for piping custom regex matches into another script
- `--no-repos` - Don't search repos
- `--no-gists` - Don't search Gists
- `--threads` - Specify max number of threads for the commit digger to use.
- `--regex-file` - Supply a custom regex file
- `--language-file` - Supply a custom file with languages to search.
- `--config-file` - Custom config file (default is `config.yml`)
- `--pages` - Max pages to search (default is 100, the page maximum)
- `--no-scoring` - Don't use scoring to filter out false positives
- `--no-api-keys` - Don't perform generic API key searching. GitHound uses common API key patterns, context clues, and a Shannon entropy filter to find potential exposed API keys.
- `--no-files` - Don't flag interesting file extensions
- `--only-filtered` - Only search filtered queries (languages)
- `--debug` - Print verbose debug messages.
- `--otp-code` - Github account 2FA code for sign-in. (Only use if you have authenticator 2FA setup on your Github account)

### Sending flags on VS Code

On launch.json send the needed flags as args
"args": [
"searchKeyword",
"tillsongalloway.com",
"--regex-file",
"regexes.txt"
]

## Building the project

From the main folder: go build .

## User feedback

These are discussions about how people use GitHound in their workflows and how we can GitHound to fufill those needs. If you use GitHound, consider leaving a note in one of the active issues.
[List of issues requesting user feedback](https://github.com/tillson/git-hound/issues?q=is%3Aissue+is%3Aopen+label%3A%22user+feedback+requested%22)

## Sponsoring

If GitHound helped you earn a big bounty, consider sending me a tip with GitHub Sponsors.

## References

- [How Bad Can It Git? Characterizing Secret Leakage in Public GitHub Repositories (Meli, McNiece, Reaves)](https://www.ndss-symposium.org/wp-content/uploads/2019/02/ndss2019_04B-3_Meli_paper.pdf)
