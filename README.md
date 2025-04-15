# GitHound

🚀 New in v3.0.0! Try the GitHound Web Dashboard  
Visualize and manage your search results in real-time with the new GitHound Explore dashboard.  
Get started now for free at https://beta.githoundexplore.com or by using the `--dashboard` flag.

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

1. Download latest version of GitHound from https://github.com/tillson/git-hound/releases (with wget [url] or from the web browser).
2. Make sure an API key is set in `config.yml`
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

`echo "api.halcorp.biz" | githound --dig-files --dig-commits --many-results --rules halcorp-api-regexes.txt --results-only | python halapitester.py`

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
GitHound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a robust scoring system.
```
Usage:
  githound [flags]

Flags:
      --query string         A query stiing (alternativ, pass through stdin)
      --config-file string   Supply the path to a config file.
      --json                 Print results in JSON format
      --rules string         Path to a list of regexes or a GitLeaks rules folder.
      -h, --help                 help for githound

      --pages int            Maximum pages to search per query (default 100)
      --github-repo          Search in a specific Github Repo only.
      --fast                 Skip file grepping and only return search preview
      --all-results          Print all results, even if they do not contain secrets
      --many-results         Search >100 pages with results sorting hack

      --dig-commits          Dig through commit history to find more secrets (CPU intensive).
      --dig-files            Dig through the repo's files to find more secrets (CPU intensive).
      --threads int          Threads to dig with (default 20)```

      --results-only         Only print match strings.
      --search-type api      Search interface (api or `ui`). API requires api_key in config, UI requires username/password

      --otp-code string      Github account 2FA token used for sign-in. (Only use if you have 2FA enabled on your account via authenticator app)

      --query-file string    A file containing a list of subdomains (or other queries).
      --filtered-only        Only print filtered results (language files)
      --debug                Enables verbose debug logging.
  
      --no-api-keys          Don't search for generic API keys.
      --no-files             Don't search for interesting files.
      --no-gists             Don't search Gists
      --no-keywords          Don't search for built-in keywords
      --no-repos             Don't search repos
      --no-scoring           Don't use scoring to filter out false positives.
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

---

## Building the Docker Image
To build the Docker image for Git-Hound, use the following command:

```bash
docker build -t my-githound-container .
```

This command builds the Docker image with the tag `my-githound-container`. You can change the tag name to your preference.

#### Running the Container
To run the Git-Hound Docker container, you'll need to provide your `config.yaml` file and any input files (like `subdomains.txt`) via Docker volumes.

#### Mounting `config.yaml`
Place your `config.yaml` file at a known location on your host machine. This file should contain your Git-Hound configuration, including GitHub credentials.

Example `config.yaml`:

```yaml
# config.yaml
github_username: "your_username"
github_password: "your_password"
# Optional: GitHub TOTP seed
# github_totp_seed: "ABCDEF1234567890"
```

#### Mounting Input Files
If you have a file like `subdomains.txt`, place it in a directory on your host machine.

#### Running the Command
Use the following command to run the container with your configuration and input files:

```bash
docker run -v /path/to/config.yaml:/root/.githound/config.yaml -v $(pwd)/data:/data my-githound-container --subdomain-file /data/subdomains.txt
```

Replace `/path/to/config.yaml` with the actual path to your `config.yaml` file. The `-v $(pwd)/data:/data` part mounts a directory containing your input files (`subdomains.txt`) into the container.

---

## References

- [How Bad Can It Git? Characterizing Secret Leakage in Public GitHub Repositories (Meli, McNiece, Reaves)](https://www.ndss-symposium.org/wp-content/uploads/2019/02/ndss2019_04B-3_Meli_paper.pdf)
