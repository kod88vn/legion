import time,urllib.request
t0=time.time()
while time.time()-t0<165:
    try:
        urllib.request.urlopen('http://127.0.0.1:8090/api/bots/kirafire',timeout=3).read(); ok=True
    except Exception: ok=False
    if not ok: print('legion down at',time.strftime('%H:%M:%S')); break
    time.sleep(5)
else: print('still up',time.strftime('%H:%M'))
