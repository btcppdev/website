// Run with Playwright installed: node ops/test-report-print.cjs
// No live database or application server is used. All requests are local fixtures.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

module.exports = async function testReportPrint(page, root = path.resolve(__dirname, ".."), artifacts) {
  const scripts = Object.fromEntries(await Promise.all(["report-print-images.js", "report-print.js"].map(async name =>
    [name, await fs.readFile(path.join(root, "static/js", name), "utf8")])));
  const artwork = await fs.readFile(path.join(root, "static/img/vegas/leading.avif"));
  const transparent = Buffer.from(await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 256;
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "#f00";
    ctx.fillRect(64, 64, 128, 128);
    return canvas.toDataURL().split(",")[1];
  }), "base64");
  const origin = "http://report-print.test";
  let html;
  const route = async route => {
    const url = new URL(route.request().url());
    const headers = url.hostname === "blocked.report-print.test"
      ? {"Access-Control-Allow-Origin": "http://other.test"}
      : {"Access-Control-Allow-Origin": "*"};
    if (url.pathname === "/art.avif") return route.fulfill({contentType: "image/avif", body: artwork, headers});
    if (url.pathname === "/alpha.png") return route.fulfill({contentType: "image/png", body: transparent, headers});
    if (url.pathname === "/globe.svg") return route.fulfill({contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="80" height="80"><circle cx="40" cy="40" r="30" fill="purple"/></svg>'});
    if (scripts[url.pathname.slice(1)]) return route.fulfill({contentType: "text/javascript", body: scripts[url.pathname.slice(1)]});
    if (url.pathname === "/") return route.fulfill({contentType: "text/html", body: html});
    return route.fulfill({status: 404, body: "Missing fixture"});
  };
  await page.route("http://**.test/**", route);
  await page.addInitScript(() => { window.print = () => { window.printCalled = true; }; });
  function fixture({slides = false, event = false, extra = ""} = {}) {
    return `<!doctype html><html><head><title>PDF regression</title><style>
      * { box-sizing:border-box } body { margin:0 } .paper-sheet { width:210mm; min-height:296mm; padding:10mm }
      .report-page { width:1200px; min-height:675px; padding:30px }
      .art { width:180px;height:140px;object-fit:cover } .small { width:90px;height:70px }
      .alpha { width:32px;height:32px } .report-recipient-logo { width:90px;height:90px }
      .responsive { width:120px;height:90px;object-fit:cover }
      @media screen { .art { width:500px;height:300px } }
      @media print { button,#report-print-status {display:none} }
      @page {size:${slides ? "1200px 675px" : "A4"};margin:0}
      </style><script src="/report-print-images.js" defer></script><script src="/report-print.js" defer></script></head>
      <body class="${event ? "" : "annual-report"}" data-layout="${slides ? "slides" : "paper"}">
      <button id="report-print">Print / Save as PDF</button><p id="report-print-status" hidden></p>
      <main><section class="${slides ? "report-page" : "paper-sheet"}"><h1>Searchable report text</h1>
      <a href="https://example.test/recording">Watch recording</a><br>
      <img class="art" src="/art.avif" alt="Artwork"><img class="art small" src="/art.avif" alt="Repeated artwork">
      <img class="alpha" src="/alpha.png" alt="Transparent image"><img class="report-recipient-logo" src="/alpha.png" alt="Sponsor logo">
      <img class="annual-recipient-logo" src="/alpha.png" width="40" height="40" alt="Annual recipient"><img src="/globe.svg" width="80" height="80" alt="Globe">
      <img hidden alt="No sponsor selected"><img hidden src="/missing.png" alt="Hidden broken image">
      <svg width="40" height="40"><circle cx="20" cy="20" r="18" fill="gold"/></svg>${extra}
      </section></main></body></html>`;
  }
  async function load(options) {
    html = fixture(options);
    await page.goto(origin);
  }
  async function clickPrint() {
    await page.getByRole("button", {name: "Print / Save as PDF", exact: true}).click();
    await page.waitForFunction(() => !document.getElementById("report-print").disabled);
    return page.locator("#report-print-status").innerText();
  }
  async function prepared() {
    const iframe = await page.locator("iframe[data-report-print-frame]").elementHandle();
    assert(iframe, "prepared snapshot must exist");
    const frame = await iframe.contentFrame();
    assert.equal(await frame.evaluate(() => window.printCalled), true);
    return frame;
  }
  await load();
  await page.evaluate(async () => {
    await Promise.all([...document.images].filter(img => !img.hidden).map(img => img.decode()));
  });
  const originalHTML = await page.locator("main").innerHTML();
  const beforePDF = await page.pdf({printBackground: true, preferCSSPageSize: true});
  assert.match(await clickPrint(), /1 pages fit A4/);
  let frame = await prepared();
  const imageInfo = await frame.evaluate(() => {
    const art = document.querySelectorAll(".art");
    const alpha = document.querySelector(".alpha");
    const c = document.createElement("canvas"); c.width = c.height = 64;
    c.getContext("2d").drawImage(alpha, 0, 0, 64, 64);
    return {
      jpeg: art[0].src.startsWith("data:image/jpeg"), shared: art[0].src === art[1].src,
      width: art[0].naturalWidth, height: art[0].naturalHeight, layoutWidth: parseFloat(art[0].style.width),
      alpha: c.getContext("2d").getImageData(0, 0, 1, 1).data[3],
      transparentPNG: alpha.src.startsWith("data:image/png"),
      logo: document.querySelector(".report-recipient-logo").getAttribute("src"),
      annualLogo: document.querySelector(".annual-recipient-logo").getAttribute("src"),
      svg: document.querySelector('img[alt="Globe"]').getAttribute("src"),
      link: document.querySelector("a").href, text: document.querySelector("h1").textContent,
    };
  });
  assert.equal(imageInfo.jpeg, true);
  assert.equal(imageInfo.shared, true);
  assert(imageInfo.width <= 400 && imageInfo.height <= 320, "size must follow print dimensions, not screen density");
  assert.equal(imageInfo.layoutWidth, 180);
  assert.equal(imageInfo.alpha, 0);
  assert.equal(imageInfo.transparentPNG, true);
  assert.equal(imageInfo.logo, "/alpha.png");
  assert.equal(imageInfo.annualLogo, "/alpha.png");
  assert.equal(imageInfo.svg, "/globe.svg");
  assert.equal(imageInfo.link, "https://example.test/recording");
  assert.equal(imageInfo.text, "Searchable report text");
  assert.equal(await page.locator("main").innerHTML(), originalHTML, "export must not mutate the editor");
  const preparedHTML = await frame.content();
  await page.evaluate(() => { const win = document.querySelector("iframe[data-report-print-frame]").contentWindow; win.dispatchEvent(new win.Event("afterprint")); });
  assert.equal(await page.locator("iframe[data-report-print-frame]").count(), 0, "afterprint must release snapshot");
  // Render exactly the prepared HTML as Chromium's PDF, without a native dialog.
  html = preparedHTML;
  await page.goto(origin);
  await page.evaluate(async () => { await Promise.all([...document.images].filter(i => !i.hidden).map(i => i.decode())); });
  const afterPDF = await page.pdf({printBackground: true, preferCSSPageSize: true});
  assert(afterPDF.length < beforePDF.length / 4, `expected substantial PDF savings, got ${beforePDF.length} -> ${afterPDF.length}`);
  if (artifacts) {
    await fs.mkdir(artifacts, {recursive: true});
    await fs.writeFile(path.join(artifacts, "before.pdf"), beforePDF);
    await fs.writeFile(path.join(artifacts, "after.pdf"), afterPDF);
    await page.emulateMedia({media: "print"});
    await page.screenshot({path: path.join(artifacts, "after.png"), fullPage: true});
    await page.emulateMedia({media: "screen"});
  }
  // CORS-readable CDN art optimizes; a non-CORS CDN remains visible with a notice.
  await load({extra: '<img class="art" src="http://cdn.report-print.test/art.avif" alt="CDN"><img class="art" src="http://blocked.report-print.test/art.avif" alt="Restricted CDN">'});
  assert.match(await clickPrint(), /Some images kept their original size/);
  frame = await prepared();
  assert.match(await frame.locator('img[alt="CDN"]').getAttribute("src"), /^data:image\/jpeg/);
  assert.equal(await frame.locator('img[alt="Restricted CDN"]').getAttribute("src"), "http://blocked.report-print.test/art.avif");
  // Responsive sources cannot select the full-resolution image again after replacement.
  await load({extra: '<picture><source srcset="/art.avif 2x"><img class="responsive" src="/art.avif" srcset="/art.avif 2x" alt="Responsive artwork"></picture>'});
  await clickPrint(); frame = await prepared();
  assert.equal(await frame.locator("picture source").count(), 0);
  assert.equal(await frame.locator(".responsive").getAttribute("srcset"), null);
  assert.match(await frame.locator(".responsive").getAttribute("src"), /^data:image\/jpeg/);
  // Existing annual safety checks still block overflow, missing assets and stale edits.
  await load({extra: '<div style="height:350mm">Overflow</div>'});
  assert.match(await clickPrint(), /PDF not opened/);
  assert.equal(await page.locator("iframe").count(), 0);
  await load({extra: '<img src="/missing.png" alt="Missing visible artwork">'});
  assert.match(await clickPrint(), /images could not load/);
  assert.equal(await page.locator("iframe").count(), 0);
  await load();
  await page.evaluate(() => document.addEventListener("report-before-print", e => e.preventDefault(), {once: true}));
  assert.match(await clickPrint(), /narrative previews/);
  await load();
  await page.evaluate(() => document.addEventListener("report-before-print", () => { document.querySelector("h1").textContent = "Edited during export"; }, {once: true}));
  assert.match(await clickPrint(), /report changed/);
  // Both event formats use the same optimizer, without the annual fixed-page guard.
  for (const slides of [false, true]) {
    await load({event: true, slides});
    assert.match(await clickPrint(), /PDF ready/);
    frame = await prepared();
    assert.equal(await frame.locator("body").getAttribute("data-layout"), slides ? "slides" : "paper");
    assert.match(await frame.locator('img[alt="Artwork"]').getAttribute("src"), /^data:image\/jpeg/);
  }
  await page.unroute("http://**.test/**", route);
  return {beforeBytes: beforePDF.length, afterBytes: afterPDF.length, checks: "PDF size, print sizing, deduplication, transparency, logos, SVG, editor isolation, cleanup, CORS fallback, responsive sources, overflow, missing assets, stale previews/edits, event paper/slides"};
};

if (require.main === module) (async () => {
  const {chromium} = require("playwright");
  const browser = await chromium.launch();
  try { console.log(await module.exports(await browser.newPage())); }
  finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
