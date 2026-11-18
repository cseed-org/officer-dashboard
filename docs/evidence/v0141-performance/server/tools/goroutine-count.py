import re,sys
ids=set();active=False
with open(sys.argv[1],encoding='utf8') as f:
 for line in f:
  if line.startswith('SIGQUIT:'):ids=set();active=True
  if active and (m:=re.match(r'^goroutine (\d+)\b',line)) and int(m.group(1))>0:ids.add(int(m.group(1)))
if not ids:raise SystemExit('No SIGQUIT goroutine dump')
print(len(ids))
