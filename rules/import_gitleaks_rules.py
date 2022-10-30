# Import GitLeaks (https://github.com/zricethezav/gitleaks) regex rules into the GitHub rules file
import requests
import sys
import datetime


if len(sys.argv) < 2:
    print("Usage: python import_gitleaks_rules.py <path to rules file>")
    sys.exit(1)

out_file = open(sys.argv[1], "a")
out_file.write("\n# The following rules are from GitLeaks (https://github.com/zricethezav/gitleaks), which is released under an MIT license (https://github.com/zricethezav/gitleaks/blob/master/LICENSE)\n")

now = datetime.datetime.now()
date = now.strftime("%Y-%m-%d")
out_file.write(f"### BEGIN GITLEAKS RULES {date}\n")
data = requests.get("https://raw.githubusercontent.com/zricethezav/gitleaks/master/config/gitleaks.toml").text.split("\n")


for i in range(0, len(data)):
	if data[i] == "[[rules]]":
		out_file.write("[[rules]]\n")
		i += 1
		out_file.write(data[i] + "\n")
		i += 1
		out_file.write(data[i] + "\n")
		i += 1
		out_file.write(data[i] + "\n")

out_file.write("### END GITLEAKS RULES\n")
out_file.close()