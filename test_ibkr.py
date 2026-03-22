import urllib3
import json
import ssl

urllib3.disable_warnings()

headers = {
    "User-Agent": "Console",
    "Cookie": "x-sess-uuid=0.44182117.1772258287.f8d4434a"
}

http = urllib3.PoolManager(cert_reqs='CERT_NONE')
r = http.request('GET', 'https://localhost:5000/v1/api/portfolio/DUP200062/summary', headers=headers)

print(f"Status: {r.status}")
print(f"Body: {r.data.decode('utf-8')}")
