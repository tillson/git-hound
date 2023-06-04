# GitHound

A pattern-matching, patch-attacking, batch-catching secret snatcher.

![GitHound](assets/logo.png)

GitHound hunts down exposed API keys and other sensitive information on GitHub using GitHub code search, pattern matching, and commit history searching. Unlike other secret-finding tools, GitHound's use of of GitHub code search enables it to search all of GitHub and isn't limited to specific repos, users, or orgs.
More information is available in the [accompanying blog post](https://tillsongalloway.com/finding-sensitive-information-on-github/).

## Features

- GitHub/Gist code search. This enables GitHound to locate sensitive information exposed across all of GitHub, uploaded by any user.
- Sensitive data detection using pattern matching, contextual information, and string entropy
- Commit history digging to find improperly deleted sensitive information
- Scoring system that filters common false positives and optimizes intensive repo digging
- Base64 detection and decoding
- Options to build GitHound into larger systems, including JSON output and custom regexes

## Usage

`echo "\"tillsongalloway.com\"" | git-hound` or `git-hound --subdomain-file subdomains.txt`

## Setup

1. Download latest version of GitHound for Linux systems at https://github.com/tillson/git-hound/releases (with wget [url] or from the web browser).
2. Decompress the download with tar -xzf [filename]. You may have to navigate to the Downloads folder with `cd` first.
3. `cd` into the now decompressed folder and configure GitHound by creating a `config.yml` file (either in the same directory as the `git-hound` binary or in `~/.githound`). There's an example config [here](https://github.com/tillson/git-hound/blob/master/config.example.yml). Make sure your username and password is in "quotation marks" and that you rename the `config.example.yml` file to `config.yml`.
4. Run `./git-hound` to test (make sure you're in the correct directory!)

### Two-Factor Authentication

If GitHound is logged into your GitHub account, two-factor authentication may kick in. You can pass 2FA codes to GitHound with `--otp-code`.
Otherwise, GitHound will prompt you for it when it starts up.
You can also [supply your 2FA seed](https://github.com/tillson/git-hound/pull/24) in the config and you'll never have to worry about 2FA again.
Grab the 2FA seed by decoding the barcode that GitHub shows during the 2FA setup process.

## API Key Regexes
GitHound utilizes a database of API key regexes maintained by the [Gitleaks](https://github.com/zricethezav/gitleaks) authors.

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
```
Usage:
  githound [flags]

Flags:
      --config-file string      Supply the path to a config file.
      --debug                   Enables verbose debug logging.
      --dig-commits             Dig through commit history to find more secrets (CPU intensive).
      --dig-files               Dig through the repo's files to find more secrets (CPU intensive).
      --filtered-only           Only print filtered results (language files)
      --github-repo             Search in a specific Github Repo only.
  -h, --help                    help for githound
      --json                    Print results in JSON format
      --language-file string    Supply your own list of languages to search (java, python).
      --legacy                  Use the legacy search method.
      --many-results            Search >100 pages with filtering hack
      --no-api-keys             Don't search for generic API keys.
      --no-files                Don't search for interesting files.
      --no-gists                Don't search Gists
      --no-keywords             Don't search for built-in keywords
      --no-repos                Don't search repos
      --no-scoring              Don't use scoring to filter out false positives.
      --otp-code string         Github account 2FA token used for sign-in. (Only use if you have 2FA enabled on your account via authenticator app)
      --pages int               Maximum pages to search per query (default 100)
      --regex-file string       Path to a list of regexes. (default "rules.toml")
      --results-only            Only print match strings.
      --subdomain-file string   A file containing a list of subdomains (or other queries).
      --threads int             Threads to dig with (default 20)
```

## Development
### Sending flags on VS Code

On launch.json send the needed flags as args
"args": [
"searchKeyword",
"tillsongalloway.com",
"--regex-file",
"regexes.txt"
]

## Building the project

From the main folder: `go build .`

## User feedback

These are discussions about how people use GitHound in their workflows and how we can GitHound to fufill those needs. If you use GitHound, consider leaving a note in one of the active issues.
[List of issues requesting user feedback](https://github.com/tillson/git-hound/issues?q=is%3Aissue+is%3Aopen+label%3A%22user+feedback+requested%22)

## 💰 Premium Monitoring & Engagements
Would you like to gain greater visibility into your company's GitHub presence? We use GitHound as one small part of a larger system that can find credential leaks, and sensitive/proprietary information across open-source websites like GitHub and DockerHub. We offer continuous monitoring services of *all of GitHub* (not just accounts you know are held by employees!) and red-team engagements/consulting services.

Reach out here to learn more: https://secretsurfer.xyz.

## References

- [How Bad Can It Git? Characterizing Secret Leakage in Public GitHub Repositories (Meli, McNiece, Reaves)](https://www.ndss-symposium.org/wp-content/uploads/2019/02/ndss2019_04B-3_Meli_paper.pdf)
