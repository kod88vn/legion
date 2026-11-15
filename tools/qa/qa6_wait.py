import time,json,urllib.request,sys
t0=time.time(); lim=float(sys.argv[1]) if len(sys.argv)>1 else 165
u=None
while time.time()-t0<lim:
    try:
        u=json.loads(urllib.request.urlopen('http://127.0.0.1:8090/api/overview',timeout=8).read())['runtime']['uptime_s']
        if u<int(sys.argv[2] if len(sys.argv)>2 else 150): print('RESTARTED uptime',u); break
    except Exception: pass
    time.sleep(15)
else: print('no restart, uptime',u)
print(time.strftime('%H:%M'))
