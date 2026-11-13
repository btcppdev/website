const fs = require('node:fs');
const pathUtil = require('node:path');
// Development-only asset generation; the report serves the resulting SVGs.
// Usage: node ops/generate-report-globes.cjs COUNTRIES.json D3.cjs TOPOJSON.cjs
const [worldFile, d3File, topojsonFile] = process.argv.slice(2);
if (!worldFile || !d3File || !topojsonFile) throw new Error('Supply countries-110m.json, d3.cjs, and topojson-client.cjs');
const d3 = require(pathUtil.resolve(d3File));
const topojson = require(pathUtil.resolve(topojsonFile));
const world = JSON.parse(fs.readFileSync(worldFile));
const countries = topojson.feature(world, world.objects.countries).features;
const land = topojson.feature(world, world.objects.land);
const borders = topojson.mesh(world, world.objects.countries, (a, b) => a !== b);
const locations = [
  ['brazil', 'Brazil', '076', [-53, -15], [-48.55, -27.59]],
  ['united-states', 'United States', '840', [-100, 38], [-115.14, 36.17]],
  ['austria', 'Austria', '040', [14, 38], [16.37, 48.21]],
  ['kenya', 'Kenya', '404', [30, 4], [36.82, -1.29]],
  ['canada', 'Canada', '124', [-95, 48], [-79.38, 43.65]],
  ['germany', 'Germany', '276', [10, 40], [13.4, 52.52]],
  ['south-korea', 'South Korea', '410', [120, 30], [126.98, 37.57]],
];
const out = pathUtil.resolve(__dirname, '../static/img/report-locations');
fs.mkdirSync(out, {recursive:true});
for (const [slug, name, id, center, city] of locations) {
  const country = countries.find(d => d.id === id);
  if (!country) throw new Error(`Missing country ${id}`);
  const projection = d3.geoOrthographic().rotate([-center[0], -center[1]]).scale(142).translate([160,160]).precision(0.3);
  const path = d3.geoPath(projection).digits(1);
  const [x,y] = projection(city).map(v => v.toFixed(1));
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 320" role="img" aria-labelledby="title">
<title id="title">${name} highlighted on the globe; dot marks the event city</title>
<!-- Natural Earth 1:110m via world-atlas 2.0.2 (public domain). Orthographic projection, D3 7.9.0. -->
<g opacity="0.55">
<circle cx="160" cy="160" r="142" fill="#e6deed" stroke="#b6a6c4" stroke-width="1.5"/>
<path d="${path(d3.geoGraticule().step([30,30])())}" fill="none" stroke="#b6a6c4" stroke-width="0.7"/>
<path d="${path(land)}" fill="#c0b3cb" stroke="#a38db5" stroke-width="0.7"/>
<path d="${path(borders)}" fill="none" stroke="#e6deed" stroke-width="0.8"/>
</g>
<path d="${path(country)}" fill="#795396" stroke="#62407f" stroke-width="1.2"/>
<circle cx="${x}" cy="${y}" r="7" fill="none" stroke="#513762" stroke-width="1.2"/>
<circle cx="${x}" cy="${y}" r="3" fill="#513762" stroke="#f3f0e9" stroke-width="1"/>
</svg>\n`;
  fs.writeFileSync(`${out}/${slug}.svg`, svg);
  console.log(`${name}: ${Buffer.byteLength(svg)} bytes`);
}
