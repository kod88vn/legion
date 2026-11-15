import json,glob,re,sys,collections
since=sys.argv[1]; until=sys.argv[2] if len(sys.argv)>2 else '9'
res={}
pat=re.compile(r'taking the Kafra from (\S+) to (.+?) \(')
for f in glob.glob('*.json'):
    try: d=json.load(open(f,encoding='utf-8'))
    except Exception: continue
    tr=[]
    for x in (d.get('diary') or []):
        if since<=x['t']<until:
            m=pat.search(x['text'])
            if m: tr.append((x['t'][11:16],m.group(1),m.group(2)))
    if tr: res[d.get('player')]=tr
c=collections.Counter(len(v) for v in res.values())
print('bots with kafra tele:',len(res),'dist',sorted(c.items()))
for k,v in sorted(res.items(),key=lambda kv:-len(kv[1])):
    if len(v)>=3: print(k,len(v),v)
