curl -s 127.0.0.1:8090/api/economy | python -c "
import json,sys;d=json.load(sys.stdin);c=d['counts']
ks=('refine','mail items','magnifier','restock','pitch','shop open','guide','pharm','brew','vend')
[print(k,v) for k,v in sorted(c.items()) if any(x in k for x in ks)]
print('vend_sales',d.get('vend_sales'),d.get('vend_sales_zeny'),'shops',len(d.get('shops') or []),'trades',d.get('trades_total'))"
