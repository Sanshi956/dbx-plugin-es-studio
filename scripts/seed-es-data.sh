#!/usr/bin/env bash
# Seed the local ES (127.0.0.1:19200) with realistic sample data so the P2 UI
# pages (Nodes / Shards / Indices / Aliases / Browser / Query / Analyzer / REST)
# have something to show. Idempotent: drops and recreates each index.
set -u
ES="${ES_URL:-http://127.0.0.1:19200}"
J="-H Content-Type:application/json -s -o /dev/null -w %{http_code}"

drop() { curl -X DELETE "$ES/$1" $J 2>/dev/null | tail -1 >/dev/null; }

echo "== products =="
drop products
curl -X PUT "$ES/products?pretty" $J -d '{
  "settings":{"number_of_shards":2,"number_of_replicas":0},
  "mappings":{"properties":{
    "name":{"type":"text","fields":{"kw":{"type":"keyword"}}},
    "category":{"type":"keyword"},
    "price":{"type":"double"},
    "stock":{"type":"integer"},
    "created":{"type":"date"},
    "tags":{"type":"keyword"}
  }}
}' >/dev/null
for d in \
  '{"name":"Mechanical Keyboard","category":"peripheral","price":399.0,"stock":120,"created":"2026-01-12","tags":["hot","rgb"]}' \
  '{"name":"Wireless Mouse","category":"peripheral","price":129.0,"stock":300,"created":"2026-02-03","tags":["sale"]}' \
  '{"name":"4K Monitor","category":"display","price":1899.0,"stock":45,"created":"2026-01-28","tags":["hot"]}' \
  '{"name":"USB-C Hub","category":"accessory","price":199.0,"stock":210,"created":"2026-03-15","tags":["new"]}' \
  '{"name":"Laptop Stand","category":"accessory","price":159.0,"stock":0,"created":"2026-02-20","tags":["sale"]}' \
  '{"name":"Webcam 1080p","category":"peripheral","price":249.0,"stock":88,"created":"2026-03-01","tags":["hot","new"]}' ; do
  curl -X POST "$ES/products/_doc?refresh=wait_for" $J -d "$d" >/dev/null
done

echo "== orders (replica=1 -> UNASSIGNED on single node, exercises topology) =="
drop orders
curl -X PUT "$ES/orders?pretty" $J -d '{
  "settings":{"number_of_shards":1,"number_of_replicas":1},
  "mappings":{"properties":{
    "order_id":{"type":"keyword"},"product":{"type":"keyword"},
    "qty":{"type":"integer"},"amount":{"type":"double"},"status":{"type":"keyword"}
  }}
}' >/dev/null
for d in \
  '{"order_id":"O-1001","product":"products","qty":2,"amount":798.0,"status":"paid"}' \
  '{"order_id":"O-1002","product":"products","qty":1,"amount":1899.0,"status":"shipped"}' \
  '{"order_id":"O-1003","product":"products","qty":5,"amount":995.0,"status":"paid"}' \
  '{"order_id":"O-1004","product":"products","qty":1,"amount":249.0,"status":"refunded"}' ; do
  curl -X POST "$ES/orders/_doc?refresh=wait_for" $J -d "$d" >/dev/null
done

echo "== metrics-daily (replicas=2 -> TWO unassigned replicas per shard) =="
# Regression fixture for the topology matrix: shard 0 gets two UNASSIGNED
# replicas, i.e. two identical "0r:UNASSIGNED" chips. That used to collide on
# the Svelte keyed-each key and blow up the whole Shards page.
drop metrics-daily
curl -X PUT "$ES/metrics-daily?pretty" $J -d '{
  "settings":{"number_of_shards":2,"number_of_replicas":2},
  "mappings":{"properties":{"host":{"type":"keyword"},"cpu":{"type":"double"},"ts":{"type":"date"}}}
}' >/dev/null
for d in \
  '{"host":"web-01","cpu":0.42,"ts":"2026-04-02T00:00:00Z"}' \
  '{"host":"web-02","cpu":0.77,"ts":"2026-04-02T00:00:00Z"}' ; do
  curl -X POST "$ES/metrics-daily/_doc?refresh=wait_for" $J -d "$d" >/dev/null
done

echo "== customers =="
drop customers
curl -X PUT "$ES/customers?pretty" $J -d '{
  "settings":{"number_of_shards":1,"number_of_replicas":0},
  "mappings":{"properties":{
    "name":{"type":"keyword"},"city":{"type":"keyword"},"vip":{"type":"boolean"}
  }}
}' >/dev/null
for d in \
  '{"name":"Alice","city":"Beijing","vip":true}' \
  '{"name":"Bob","city":"Shanghai","vip":false}' \
  '{"name":"Carol","city":"Shenzhen","vip":true}' ; do
  curl -X POST "$ES/customers/_doc?refresh=wait_for" $J -d "$d" >/dev/null
done

echo "== logs-app =="
drop logs-app
curl -X PUT "$ES/logs-app?pretty" $J -d '{
  "settings":{"number_of_shards":1,"number_of_replicas":0},
  "mappings":{"properties":{"level":{"type":"keyword"},"msg":{"type":"text"},"ts":{"type":"date"}}}
}' >/dev/null
for d in \
  '{"level":"INFO","msg":"service started","ts":"2026-04-01T08:00:00Z"}' \
  '{"level":"WARN","msg":"high latency on /search","ts":"2026-04-01T08:05:00Z"}' \
  '{"level":"ERROR","msg":"failed to connect to db","ts":"2026-04-01T08:06:30Z"}' ; do
  curl -X POST "$ES/logs-app/_doc?refresh=wait_for" $J -d "$d" >/dev/null
done

echo "== archived-2023 (then CLOSE to test open/close UI) =="
drop archived-2023
curl -X PUT "$ES/archived-2023?pretty" $J -d '{
  "settings":{"number_of_shards":1,"number_of_replicas":0},
  "mappings":{"properties":{"note":{"type":"text"}}}
}' >/dev/null
curl -X POST "$ES/archived-2023/_doc?refresh=wait_for" $J -d '{"note":"old data"}' >/dev/null
curl -X POST "$ES/archived-2023/_close" $J >/dev/null

echo "== aliases =="
curl -X POST "$ES/_aliases" $J -d '{"actions":[
  {"add":{"index":"products","alias":"prod-view"}},
  {"add":{"index":"orders","alias":"active-orders"}}
]}' >/dev/null

echo "== done; summary =="
curl -s "$ES/_cat/indices?format=json&bytes=b" | D:/software/dev/Python/Python313/python.exe -c "
import sys,json
for i in json.load(sys.stdin):
    print(f\"{i['index']:16s} docs={i.get('docs.count') or '-':>4} pri={i['pri']} rep={i['rep']} health={i['health']} status={i['status']}\")"
curl -s "$ES/_cat/aliases?format=json" | D:/software/dev/Python/Python313/python.exe -c "
import sys,json
for a in json.load(sys.stdin): print('alias', a['alias'], '->', a['index'])"
echo "== shard chips (duplicate labels are expected & now safe) =="
curl -s "$ES/_cat/shards/metrics-daily?format=json&h=index,shard,prirep,state,node" | D:/software/dev/Python/Python313/python.exe -c "
import sys,json,collections
rows=json.load(sys.stdin)
c=collections.Counter(f\"{r['shard']}{r['prirep']}:{r['state']}\" for r in rows)
for k,v in sorted(c.items()): print(f'  {k:20s} x{v}')"
