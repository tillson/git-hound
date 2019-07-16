# Git Hound
A pattern-matching, batch-catching secret snatcher.
**For educational use only.**

![Git Hound][assets/logo.png]

## Usage

`echo "tillsongalloway.com" | python git-hound.oy` or `python git-hound.py --subdomain-file subdomains.txt`

### Flags

* `--subdomain-file` - The file with the subdomains
* `--output` - The output file (default is stdout)
* `--all` - Print all URLs, including ones with no pattern match. Otherwise, the scoring system will do the work.
* `--regex-file` - Supply a custom regex file
* `--api-keys` - Enable generic API key searching. This uses common API key patterns and Shannon entropy to find potential exposed API keys.
* `--language-file` - Supply a custom file with languages to search.
* `--config-file` - Custom config file (default is `config.yml`)
* `--pages` - Max pages to search (default is 100, the page maximum)
* `--silent` - Don't print results to stdout (most reasonably used with --output).
* `--no-antikeywords` - Don't attempt to filter out known mass scans
* `--only-filtered` - Only search filtered queries (languages, file extensions)


Thanks for reading!
