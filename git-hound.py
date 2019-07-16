import requests
import sys
import re
import urllib.parse
import time
import hashlib
import random
import json
import yaml
import argparse
import entropy
import fileinput

parser = argparse.ArgumentParser(
    description='Git Hound')
parser.add_argument(
  '--subdomain-file', type=str,
  help='The file with the subdomains.')
parser.add_argument(
    '--output',
    help='The output file.')
parser.add_argument(
    '--output-type', type=str,
    help='The output type. [default, json]')
parser.add_argument(
    '--all', default=False,
    type=bool,
    help='Print all URLs, including ones with no pattern match. Otherwise, the scoring system will do the work.')
parser.add_argument(
    '--api-keys', default=True,
    type=bool,
    help='Search for API keys')
parser.add_argument(
    '--regex-file',
    help='Supply your own regex list')
parser.add_argument(
    '--file-types',
    help='Supply your own list of file types to check')
parser.add_argument(
    '--language-file',
    help='Supply your own list of file types to check')
parser.add_argument(
    '--config-file',
    help='Custom config file location (default is config.yml)')
parser.add_argument(
    '--pages',
    type=int,
    help='Max number of pages to search.')
parser.add_argument(
    '--silent',
    type=bool,
    default=False,
    help='Don\'t print results to stdout (most reasonably used with --output).')
parser.add_argument(
    '--no-antikeywords',
    type=bool,
    default=False,
    help='Don\'t attempt to filter out known mass scan databases')
parser.add_argument(
    '--only-filtered',
    type=bool,
    default=False,
    help='Only search filtered queries (languages and files)')

args = parser.parse_args()


with open((args.config_file if args.config_file else "config.yml"), 'r') as ymlfile:
    config = yaml.load(ymlfile, Loader=yaml.SafeLoader)

GH_USERNAME = config['github_username']
GH_PASSWORD = config['github_password']

class bcolors:
    HEADER = '\033[95m'
    OKBLUE = '\033[94m'
    OKGREEN = '\033[92m'
    WARNING = '\033[93m'
    FAIL = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'
    UNDERLINE = '\033[4m'

def grab_csrf_token(url, session):
  response = session.get(url)
  text = response.text
  csrf = re.search(r"authenticity_token.*\"\s", text).group()[27:]
  csrf = csrf[:len(csrf) - 2]
  return csrf

def login_to_github(session):
  csrf = grab_csrf_token('https://github.com/login', session)
  # print(GH_USER)
  response = session.post('https://github.com/session',
   data = {
     'authenticity_token': csrf,
     'login': GH_USERNAME,
     'password': GH_PASSWORD
   }
  )

def parse_match(match, body):
  print(match)
  s_offset = match.span(0) - 100 if match.span(0) > 100 else match.span(0)
  e_offset = match.span(0) + 100 if match.span(1) + 100 < len(body) else match.span(1)
  context = body[s_offset:e_offset]
  result = {
    'start': match.span()[0],
    'end': match.span()[1],
    'context': context,
    'text': match[0]
  }
  return result

def search_code(query, sessions, language=None):
  query = urllib.parse.quote(query + " fork:false")

  paths = set()
  delay_time = 5
  maximum_pages = args.pages if args.pages else 50
  page = 1
  while page < maximum_pages + 1:
    session = random.choice(sessions)
    url_string = 'https://github.com/search?o=asc&p=' + str(page) + '&q=' + query + '&s=indexed&type=Code'
    if language:
      url_string += '&l=' + language
    response = session.get(url_string)
    if response.status_code == 429:
      delay_time += 5
      print(bcolors.WARNING + '[!] Rate limited by GitHub. Delaying ' + str(delay_time) + 's...' + bcolors.ENDC)
      time.sleep(delay_time)
      continue
    if delay_time > 10:
      delay_time -= 1
    page += 1
    if response.status_code != 200 and response.status_code != 400:
      break
    results = re.findall(r"href=\"\/.*blob.*\">", response.text)
    if len(results) == 0:
      break
    for result in results:
      if re.match(r"(h1domains|bounty\-targets|url_short|url_list|\.csv|alexa)", result[7:len(result) - 2]):
        continue
      paths.add(result[7:len(result) - 2])
    time.sleep(delay_time)
  return paths

def regex_array(array):
  regex = r"("
  for elm in array:
    elm = elm.replace("\\\\", "\\")
    regex += elm + r"|"
    if '.*' in elm:
      print(bcolors.WARNING + "[!] The regex wildcard match .* is slow and may slow down Git Hound." + bcolors.ENDC)
  regex = regex[:-1] + r")"
  return re.compile(regex)

interesting = {}

visited = set()
visited_hashes = set()
def print_paths_highlighted(subdomain, paths, sessions, output_file, regex=None):
  print(bcolors.OKGREEN + subdomain + bcolors.ENDC)
  if len(paths) == 0:
    if not args.silent:
      print('No results.')
  for path in paths:
    raw_path = path.replace('blob/', '').split('#')[0]
    if raw_path in visited:
      continue
    visited.add(raw_path)
    session = random.choice(sessions)
    response = session.get('https://raw.githubusercontent.com/' + raw_path)
    checksum = hashlib.md5(response.text.encode('utf-8'))
    if checksum in visited_hashes:
      continue
    visited_hashes.add(checksum)
    score = 0
    domain = '.'.join(subdomain.split(".")[-2:])

    custom_regex = regex != None
    if not custom_regex:
      regex = r"(sf_username|api_key" \
        + r"|(stage|staging)\." + re.escape(domain) + r"|db_username|db_password" \
        + r"|api_key_admin|x\-api\-key" \
        + r"|jenkins" \
        + r"|id_rsa|pg_pass|[\w\.=-]+@" + re.escape(domain) + r")"
    matches = re.findall(
      regex,
      response.text.lower()
    )
    match_set = set()
    match_text_set = set()
    for match in matches:
      if len(match) == 0:
        continue
      if not match[0] in match_text_set:
        match_set.add(match)
        match_text_set.add(match[0])
        score += 1

    if args.api_keys:
      generic_api_keys = re.findall(r"("
          + r"'[0-9A-z\_\-]{32}'|'[0-9A-z\_\-]{48}'|'[0-9A-z\_\-]{47}'"
          + r"|\"[0-9A-z\_\-]{32}\"|\"[0-9A-z\_\-]{48}\"|\"[0-9A-z\_\-]{47}\""
          + r"|\b[0-9a-z]{8}\-[0-9a-z]{4}\-[0-9a-z]{4}\-[0-9a-z]{4}\-[0-9a-z]{12}\b"
          + r"|\b[0-9A-z]{40}\b"
          + r"|\b[0-9a-z]{64}\b)",
          response.text.lower()
      )

      if len(generic_api_keys) < 24:
        for match in generic_api_keys:
          if not match[0] in match_text_set:
            if entropy.entropy(match[0]) > 4:
              match_set.add(match)
              match_text_set.add(match[0])
              score += 2 if custom_regex else 1

    if not custom_regex:
      keywords = re.findall(r"(.sql|.sublime_session|.env|.yml|.ipynb)$", raw_path.lower())
      if keywords:
        score += len(keywords) * 2

    if not args.no_antikeywords:
      if re.search(r"(\.html|\.csv|hosts\.txt|host\.txt|registry\.json|readme\.md|" + re.escape('.'.join(subdomain.split(".")[-2:])) + r".txt)$", raw_path.lower()):
        score -= 1
      anti_keywords = re.findall(r"(alexa|urls|adblock|domain|dns|top1000|top\-1000|httparchive"
        + r"|blacklist|hosts|ads|whitelist|crunchbase|tweets|tld|output|hosts\.txt"
        + r"|host\.txt|aquatone|recon\-ng|hackerone|bugcrowd|xtreme|list|tracking|malicious|ipv(4|6)|host\.txt)", raw_path.lower())
      if anti_keywords:
        score -= 2 ** len(anti_keywords)
    if score > -5:
      if score > 1:
        if not args.silent:
          print(bcolors.FAIL + 'https://github.com/' + path + bcolors.ENDC)
      else:
        if not args.silent:
          print(bcolors.WARNING + 'https://github.com/' + path + bcolors.ENDC)
      interesting[path] = {
        'url': 'https://github.com/' + path,
        'results': []
       }
      if output_file != None:
        output_file.write('https://github.com/' + path + "\n")
      for match in match_set:
        match_str = match[0] if len(match[0]) > 1 else match
        truncated = match_str
        if len(match_str) > 35:
          truncated = match_str[0][:35]
        if not args.silent:
          print('  > ' + truncated)
        if output_file != None:
          output_file.write('  > ' + match_str + "\n")
    else:
      if args.all:
        interesting[path] = {
          'url': 'https://github.com/' + path,
          'results': []
        }
      if score > -1 or args.all:
        if not args.silent:
          print('https://github.com/' + path)
      if output_file != None:
        output_file.write('https://github.com/' + path + "\n")
  if args.output and args.output_type == "json":
    out_file = open(args.output, 'w+')
    out_file.write(json.dumps(interesting))
    out_file.close()

###

subdomains = []
if not sys.stdin.isatty():
  for line in fileinput.input(files=('-')):
    stripped = line.rstrip()
    if len(stripped) > 0:
      subdomains.append(stripped)
else:
  if args.subdomain_file:
    subdomain_file = args.subdomain_file
    subdomains = open(subdomain_file).read().split("\n")
if len(subdomains) == 0:
  print(bcolors.FAIL + "[!] Please specify a subdomain file." + bcolors.ENDC)
  exit(1)

regex_string = None
if args.regex_file:
  regex_file_array = open(args.regex_file).read().split("\n")
  regex_string = regex_array(regex_file_array)

files = []
if args.file_types:
  ext_filetypes = open(args.file_types).read().split("\n")
  for filetype in ext_filetypes:
    files.append(filetype)
else:
  files = ['.git-credentials', '.bash_history', '*.sublime_session', '*.log', '.env']

languages = []
if args.language_file:
  ext_languages = open(args.language_file).read().split("\n")
  for filetype in ext_languages:
    languages.append(filetype)
else:
  languages = ['SQL', 'Jupyter+Notebook', 'Shell']

sessions = []
session = requests.Session()
login_to_github(session)
sessions.append(session)
print(bcolors.OKBLUE + '[*] Logged into GitHub.com as ' + GH_USERNAME + bcolors.ENDC)

output_file = None
if args.output and args.output_type != "json":
  output_file = open(args.output, 'w+')

for subdomain in subdomains:
  paths = set()
  for file_type in languages:
    for path in search_code('"' + subdomain + '"', sessions, language=file_type):
      paths.add(path)
  if not args.only_filtered:
    for path in search_code('"' + subdomain + '"', sessions):
      paths.add(path)
  for filename in files:
    for path in search_code('filename:' + filename + ' "' + subdomain + '"', sessions):
      paths.add(path)
  print_paths_highlighted(subdomain, paths, sessions, output_file, regex=regex_string)
  time.sleep(5)
  if output_file != None:
    output_file.close()

