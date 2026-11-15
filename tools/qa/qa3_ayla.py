import json,urllib.request,time,datetime,sys
LOG=open("qa3_ayla_log.txt","a")
last=None; streak=0
while True:
    try:
        P=json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/parties/detail",timeout=15).read())
        p=[p for p in P if p['name'].startswith('Ayla')][0]; M=p['members']; A=M[0]
        now=datetime.datetime.utcnow().strftime("%H:%M:%S")
        row=" ".join(f"{m['name'][:6]}:{m['map']}:{m['x']},{m['y']}:{round(100*m['weight']/max(1,m['max_weight']))}" for m in M)
        LOG.write(now+" "+row+"\n"); LOG.flush()
        far=sum(1 for m in M[1:] if not m['online'] or m['map']!=A['map'] or max(abs(m['x']-A['x']),abs(m['y']-A['y']))>20)
        streak=streak+1 if far else 0
        key=(A['map'],streak>=4)
        if key!=last: print(now,"AYLA",A['map'],A['x'],A['y'],"w%",round(100*A['weight']/A['max_weight']),"apart:",far,"|",row,flush=True)
        last=key
    except Exception as e: pass
    time.sleep(20)
