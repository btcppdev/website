"""Extract only static place data from a checksum-verified geonamescache wheel.
Usage: python3 internal/reportgeo/generate.py /path/to/geonamescache-3.0.2.whl
No code from the wheel is executed.
"""
import gzip, hashlib, json, pathlib, sys, zipfile
raw = pathlib.Path(sys.argv[1]).read_bytes()
assert hashlib.sha256(raw).hexdigest() == 'b830e8942f2d58c7e68782dcf4dff2ffe8c4104a35ee881ed1ad4023cefcdba4'
with zipfile.ZipFile(sys.argv[1]) as z:
    cities = json.loads(z.read('geonamescache/data/cities15000.json'))
    countries = json.loads(z.read('geonamescache/data/countries.json'))
    states = json.loads(z.read('geonamescache/data/us_states.json'))
rows = []
for key, c in sorted(cities.items()):
    country = countries.get(c['countrycode'])
    if not country:
        continue
    region = states.get(c['admin1code'], {}).get('name', '') if c['countrycode'] == 'US' else ''
    fields = [c['name'], country['name'], country['iso'], country['iso3'], region, c['admin1code'] if region else '', str(c['latitude']), str(c['longitude'])]
    assert all('\t' not in x and '\n' not in x for x in fields)
    rows.append('\t'.join(fields))
path = pathlib.Path(__file__).with_name('cities.tsv.gz')
path.write_bytes(gzip.compress(('\n'.join(rows)+'\n').encode(), mtime=0))
print(len(rows), 'cities;', path.stat().st_size, 'bytes')
