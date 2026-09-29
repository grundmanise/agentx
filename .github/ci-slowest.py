"""TEMPORARY: summarise a go test -json run: the slowest tests, the files
they are in, the serial tests and each package's time."""

import collections
import glob
import json
import re
import sys

elapsed = {}  # (package, test) -> seconds
paused = set()  # tests that called t.Parallel
packages = {}
failed_output = collections.defaultdict(list)
for line in open(sys.argv[1]):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    pkg, test, action = e.get("Package", ""), e.get("Test"), e.get("Action")
    if test and action == "pause":
        paused.add((pkg, test))
    if action in ("pass", "fail"):
        if test:
            elapsed[(pkg, test)] = e.get("Elapsed", 0)
        else:
            packages[pkg] = e.get("Elapsed", 0)
    if action == "output" and test:
        failed_output[(pkg, test)].append(e.get("Output", ""))

short = lambda pkg: pkg.rsplit("/", 1)[-1]
names = set(elapsed)
leaves = {k for k in names if not any(o[0] == k[0] and o[1].startswith(k[1] + "/") for o in names)}
tops = {k for k in names if "/" not in k[1]}

files = {}
for path in glob.glob("internal/**/*_test.go", recursive=True):
    pkg = path.split("/")[-2]
    for m in re.finditer(r"^func (Test\w+)\(", open(path).read(), re.M):
        files[(pkg, m.group(1))] = path.split("/", 1)[1]

print("== packages (wall s)")
for pkg, s in sorted(packages.items(), key=lambda kv: -kv[1]):
    print(f"{s:8.2f}  {short(pkg)}")

print(f"\n== {len(tops)} top-level tests, {len(leaves)} leaves, leaf sum {sum(elapsed[k] for k in leaves):.1f} s")

print("\n== 40 slowest tests at any level (s)")
for k in sorted(names, key=lambda k: -elapsed[k])[:40]:
    print(f"{elapsed[k]:8.2f}  {short(k[0])}.{k[1]}")

print("\n== files by leaf time (s, count)")
per_file = collections.Counter()
count = collections.Counter()
for k in leaves:
    f = files.get((short(k[0]), k[1].split("/")[0]), short(k[0]) + "/?")
    per_file[f] += elapsed[k]
    count[f] += 1
for f, s in per_file.most_common(25):
    print(f"{s:8.2f}  {count[f]:4d}  {f}")

serial = [k for k in tops if k not in paused]
print(f"\n== {len(serial)} serial top-level tests, {sum(elapsed[k] for k in serial):.1f} s in all")
for k in sorted(serial, key=lambda k: -elapsed[k])[:15]:
    print(f"{elapsed[k]:8.2f}  {short(k[0])}.{k[1]}")

bad = [k for k in names if k in elapsed and any("--- FAIL" in o for o in failed_output[k])]
for k in bad:
    print(f"\n== output of {k[1]}")
    print("".join(failed_output[k]))
