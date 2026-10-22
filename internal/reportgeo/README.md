# Offline report city lookup

`cities.tsv.gz` is a reduced GeoNames cities15000 dataset (city names, countries,
US regions, coordinates), extracted from geonamescache 3.0.2 on 2026-10-22.
GeoNames data is licensed under **CC BY 4.0**:
https://creativecommons.org/licenses/by/4.0/
Source and schema: https://download.geonames.org/export/dump/readme.txt
Distribution: https://pypi.org/project/geonamescache/3.0.2/

Regenerate with `python3 internal/reportgeo/generate.py ARCHIVE.whl`.
The generator verifies the published SHA256 and extracts JSON without executing
package code. The app does not require Python or any network lookup.

The dataset covers cities over 15,000 population and capitals. Matching uses
primary city names, case/accent normalization, and optional country names/codes
or US states. Ambiguous city names, countries alone, smaller towns, multi-origin
answers, and prose stay unresolved for an admin to review. Points indicate
approximate city centres, not personal addresses or verified travel.
