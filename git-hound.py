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
  help='The file with the subdomains (or other queries).')
parser.add_argument(
    '--output',
    help='The output file.')
parser.add_argument(
    '--output-type', type=str,
    help='The output type. [default, json]')
parser.add_argument(
    '--all',
    default=False,
    action='store_true',
    help='Print all URLs, including ones with no pattern match. Otherwise, the scoring system will do the work.')
parser.add_argument(
    '--api-keys',
    default=False,
    action='store_true',
    help='Search for API keys')
parser.add_argument(
    '--regex-file',
    help='Supply your own regex list')
parser.add_argument(
    '--search-files',
    help='Supply your own list of files to check (*.env, .htpasswd)')
parser.add_argument(
    '--language-file',
    help='Supply your own list of file types to check (java, python)')
parser.add_argument(
    '--config-file',
    help='Custom config file location (default is config.yml)')
parser.add_argument(
    '--pages',
    type=int,
    help='Max number of pages to search.')
parser.add_argument(
    '--silent',
    action='store_true',
    default=False,
    help='Don\'t print results to stdout (most reasonably used with --output).')
parser.add_argument(
    '--no-antikeywords',
    action='store_true',
    default=False,
    help='Don\'t attempt to filter out known mass scan databases')
parser.add_argument(
    '--only-filtered',
    default=False,
    action='store_true',
    help='Only search filtered queries (languages and files)')
parser.add_argument(
    '--gist-only',
    action='store_true',
    default=False,
    help='Only search Gists (default searches both repos and gists)')
parser.add_argument(
    '--no-repeated-matches',
    action='store_true',
    default=False,
    help='Don\'t print repeated matches')
parser.add_argument(
    '--debug',
    default=False,
    action='store_true',
    help='Print debug messages')
parser.add_argument(
    '--many-results',
    default=False,
    action='store_true',
    help='Print debug messages')


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

def debug_log(data):
  print(bcolors.OKBLUE + '[debug] ' + bcolors.ENDC + data)

def grab_csrf_token(url, session):
  response = session.get(url)
  text = response.text
  csrf = re.search(r"authenticity_token.*\"\s", text).group()[27:]
  csrf = csrf[:len(csrf) - 2]
  return csrf

def login_to_github(session):
  csrf = grab_csrf_token('https://github.com/login', session)
  session.post('https://github.com/session',
   data = {
     'authenticity_token': csrf,
     'login': GH_USERNAME,
     'password': GH_PASSWORD
   }
  )

def search_code(query, sessions, language=None, fileName=None):
  query = urllib.parse.quote(query.replace("-", "+") + " fork:false")
  if fileName != None:
    query += " filename:" + fileName
  paths = []
  path_set = set()
  delay_time = 5
  maximum_pages = args.pages if args.pages else 100
  page = 1
  if args.debug:
    debug_log('Querying GitHub projects: `' + query + '`')
  order = ['asc']
  order_index = 0
  search_type = ['indexed']
  search_type_index = 0
  while search_type_index < len(search_type):
    order_index = 0
    while order_index < len(order):
      if search_type[search_type_index] == '':
        order_index += 1
        continue
      page = 0
      while page < maximum_pages + 1:
        session = random.choice(sessions)
        url_string = 'https://github.com/search?o=' + order[order_index] \
          + '&p=' + str(page) + '&q=' + query + '&s=' + search_type[search_type_index] + '&type=Code'
        if language:
          url_string += '&l=' + language
        if args.debug:
          debug_log(url_string)
        response = session.get(url_string)
        if response.status_code == 429:
          delay_time += 5
          print(bcolors.WARNING + '[!] Rate limited by GitHub. Delaying ' + str(delay_time) + 's...' + bcolors.ENDC)
          time.sleep(delay_time)
          continue
        if delay_time > 10:
          delay_time -= 1
        if page == 1 and order[order_index] == 'asc' and  search_type[search_type_index] == 'indexed':
          match = re.search(r"\bdata\-total\-pages\=\"(\d+)\"", response.text)
          if match != None:
            if args.many_results and int(match.group(1)) > maximum_pages - 1:
              print(bcolors.OKBLUE + '[*] Searching ' + str(match.group(1)) + '+ pages of results...' + bcolors.ENDC)
              order.append('desc')
              search_type.append('')
            else:
              print(bcolors.OKBLUE + '[*] Searching ' + str(match.group(1)) + ' pages of results...' + bcolors.ENDC)
          else:
            print(bcolors.OKBLUE + '[*] Searching 1 page of results...' + bcolors.ENDC)
        page += 1
        if args.debug and page % 20 == 0:
          debug_log('  Page ' + str(page))
        if response.status_code != 200 and response.status_code != 400:
          break
        results = re.findall(r"href=\"\/.*blob.*\">", response.text)
        if len(results) == 0:
          break
        for result in results:
          result = result[7:len(result) - 2]
          if result in path_set:
            continue
          path_set.add(result)
          if re.match(r"(h1domains|bounty\-targets|url_short|url_list|\.csv|alexa)", result):
            continue
          raw_path = result.replace('blob/', '').split('#')[0]
          paths.append({ 'source': 'github_repo', 'url': 'https://github.com/' + result, 'data_url': 'https://raw.githubusercontent.com/' + raw_path })
        time.sleep(delay_time)
      order_index += 1
    search_type_index += 1
  return paths

def search_gist(query, sessions, language=None, fileName=None):
  query = urllib.parse.quote(query.replace("-", "+") + " stars:<5 fork:false")
  if fileName != None:
    query += " filename:" + fileName
  paths = []
  delay_time = 5
  maximum_pages = args.pages if args.pages else 100
  page = 1
  while page < maximum_pages + 1:
    session = random.choice(sessions)
    url_string = 'https://gist.github.com/search?o=asc&p=' + str(page) + '&q=' + query + '&s=indexed'
    if args.debug:
      debug_log('Querying Gist: `' + query + '`')
    if args.debug and page % 20 == 0:
      debug_log(' . Page ' + str(page))
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
    results = re.findall(r"href=\"\/(\w+\/[0-9a-z]{5,})\">", response.text)
    if len(results) == 0:
      break
    result_set = set()
    for result in results:
      if result in result_set:
        continue
      result_set.add(result)
      project_page = session.get('https://gist.github.com/' + result)
      escaped_path = re.escape(result)
      match = re.search(r"href\=\"(\/" + escaped_path + r"\/raw\/[0-9a-z]{40}\/[\w_\-\.\/\%]{1,255})\"\>", project_page.text)
      if match != None:
        paths.append({ 'source': 'gist', 'url': 'https://gist.github.com/' + result, 'data_url': 'https://gist.githubusercontent.com' + match.group(1) })
    time.sleep(delay_time)
  return paths


def regex_array(array):
  regex = r"("
  for elm in array:
    if elm == "":
      continue
    regex += elm + r"|"
    if '.*' in elm:
      print(bcolors.WARNING + "[!] The regex wildcard match .* can be slow if used improperly and may slow down Git Hound." + bcolors.ENDC)
  regex = regex[:-1] + r")"
  return re.compile(regex)

interesting = {}

visited = set()
visited_hashes = set()
match_string_set = set()
def print_paths_highlighted(subdomain, paths, sessions, output_file, regex=None):
  print(bcolors.OKGREEN + subdomain + bcolors.ENDC)
  if len(paths) == 0:
    if not args.silent:
      print('No results.')
  custom_regex = regex != None
  for result in paths:
    if result['data_url'] in visited:
      continue
    visited.add(result['data_url'])
    session = random.choice(sessions)
    response = session.get(result['data_url'])
    checksum = hashlib.md5(response.text.encode('utf-8'))
    if checksum in visited_hashes:
      continue
    visited_hashes.add(checksum)
    score = 0
    domain = '.'.join(subdomain.split(".")[-2:])
    if not custom_regex:
      regex = re.compile(r"\b(sf_username" \
        + r"|(stage|staging|atlassian|jira|conflence|zendesk)\." + re.escape(domain) + r"|db_username|db_password" \
        + r"|hooks\.slack\.com|pt_token|full_resolution_time_in_minutes" \
        + r"|xox[a-zA-Z]-[a-zA-Z0-9-]+" \
        + r"|s3\.console\.aws\.amazon\.com\/s3\/buckets|" \
        + r"|id_rsa|pg_pass|[\w\.=-]+@" + re.escape(domain) + r")\b", flags=re.IGNORECASE)
    s_time = 0
    if args.debug:
      s_time = time.time()
    matches = re.finditer(
      regex,
      response.text
    )
    match_set = set()
    match_text_set = set()
    for match in matches:
      if match.start(0) == match.end(0):
        continue
      if not match.group(0) in match_text_set:
        match_set.add(match.group(0))
        match_text_set.add(match.group(0))
        if custom_regex:
          score += 2
        else:
          score += 1
    if args.debug:
      debug_log(result['data_url'])
      debug_log("Time to check definite regexes: " + str(time.time() - s_time) + ".")

    if args.api_keys:
      if args.debug:
        s_time = time.time()
      generic_api_keys = re.finditer(
        re.compile(r"(ACCESS|SECRET|LICENSE|CRYPT|PASS|KEY|ADMIn|TOKEN|PWD|Authorization|Bearer)[\w\s:=\"']{0,20}[=:\s'\"]([\w\-+=]{32,})\b", flags=re.IGNORECASE),
          response.text
      )
      for match in generic_api_keys:
        if not match.group(2) in match_text_set:
          if entropy.entropy(match.group(2)) > 3.25:
            match_set.add(match.group(2))
            match_text_set.add(match.group(2))
            score += 2
      if args.debug:
        debug_log("Time to find API key regexes: " + str(time.time() - s_time) + ".")

    if not custom_regex:
      keywords = re.findall(r"(.sql|.sublime_session|.env|.yml|.ipynb)$", result['data_url'].lower())
      if keywords:
        score += len(keywords) * 2

    if not args.no_antikeywords:
      if re.search(r"(\.html|\.csv|hosts\.txt|host\.txt|registry\.json|readme\.md|" + re.escape('.'.join(subdomain.split(".")[-2:])) + r".txt)$", result['data_url'].lower()):
        score -= 1
      anti_keywords = re.findall(r"(alexa|urls|adblock|domain|dns|top1000|top\-1000|httparchive"
        + r"|blacklist|hosts|ads|whitelist|crunchbase|tweets|tld|hosts\.txt"
        + r"|host\.txt|aquatone|recon\-ng|hackerone|bugcrowd|xtreme|list|tracking|malicious|ipv(4|6)|host\.txt)", result['data_url'].lower())
      if anti_keywords:
        score -= 2 ** len(anti_keywords)
    if score > 0:
      if args.no_repeated_matches:
        unique_matches = len(match_set)
        for match in match_set:
          if match in match_string_set:
            unique_matches -= 1
          else:
            match_string_set.add(match)
        if unique_matches == 0:
          continue
      if score > 1:
        if not args.silent:
          print(bcolors.FAIL + result['url'] + bcolors.ENDC)
      else:
        if not args.silent:
          print(bcolors.WARNING + result['url'] + bcolors.ENDC)
      interesting[result['url']] = {
        'url': result['url'],
        'results': []
       }
      if output_file != None:
        output_file.write(result['url'] + "\n")
      for match in match_set:
        truncated = match
        interesting[result['url']]['results'].append(match)
        if len(match) == 0:
          continue
        if not args.silent:
          print('  > ' + truncated)
        if output_file != None:
          output_file.write('  > ' + match + "\n")
    else:
      if args.all:
        interesting[result['url']] = {
          'url': result['url'],
          'results': []
        }
        if not args.silent:
          print(result['url'])
        if output_file != None:
          output_file.write(result['url'] + "\n")
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
  print(bcolors.FAIL + "[!] Please specify some queries (either with stdin or the --subdomain-file flag)." + bcolors.ENDC)
  exit(1)

regex_string = None
if args.regex_file:
  regex_file_array = open(args.regex_file).read().split("\n")
  regex_string = regex_array(regex_file_array)

files = []
if args.search_files:
  ext_filetypes = open(args.search_files).read().split("\n")
  for filetype in ext_filetypes:
    files.append(filetype)

languages = []
if args.language_file:
  ext_languages = open(args.language_file).read().split("\n")
  for filetype in ext_languages:
    languages.append(filetype)

sessions = []
session = requests.Session()
login_to_github(session)
sessions.append(session)
print(bcolors.OKBLUE + '[*] Logged into GitHub.com as ' + GH_USERNAME + bcolors.ENDC)

output_file = None
if args.output and args.output_type != "json":
  output_file = open(args.output, 'w+')

for subdomain in subdomains:
  paths = []
  # results = []

  github_results = 0
  if not args.only_filtered:
    if not args.gist_only:
      for path in search_code('"' + subdomain + '"', sessions):
        paths.append(path)
      github_results = len(paths)
    for path in search_gist('"' + subdomain + '"', sessions):
      paths.append(path)

    for file_type in languages:
      if not args.gist_only:
        for path in search_code('"' + subdomain + '"', sessions, language=file_type):
          paths.append(path)
      for path in search_gist('"' + subdomain + '"', sessions, language=file_type):
        paths.append(path)


    for filename in files:
      for path in search_code('"' + subdomain + '"', sessions, fileName=filename):
        paths.append(path)
      for path in search_gist('"' + subdomain + '"', sessions, fileName=filename):
        paths.append(path)
  if args.debug:
    debug_log('Finished scraping GitHub search results. Will now search for secrets in ' + str(len(paths)) + ' files.')
  print_paths_highlighted(subdomain, paths, sessions, output_file, regex=regex_string)
  time.sleep(5)
  if output_file != None:
    output_file.close()

