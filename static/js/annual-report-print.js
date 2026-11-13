"use strict";

// Measure a snapshot using the print styles at A4 width. Measuring the editor
// viewport misses wrapping changes; printing before fonts load misses them too.
(() => {
  const button = document.getElementById("report-print");
  const status = document.getElementById("report-print-status");
  if (!button || !status) return;
  let frame, busy = false;

  async function withDeadline(work) {
    let timer;
    try {
      return await Promise.race([work, new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error("Report assets are still loading. Please try again.")), 30000);
      })]);
    } finally { clearTimeout(timer); }
  }

  // Swap media types only inside the measurement frame. Restore them before
  // native printing so @page, print rules and browser pagination stay intact.
  function usePrintStyles(doc) {
    const changed = [];
    function swap(media) {
      const original = media.mediaText;
      const value = original.replace(/\b(screen|print)\b/g, type => type === "print" ? "screen" : "print");
      if (value !== original) { changed.push([media, original]); media.mediaText = value; }
    }
    function visit(sheet) {
      swap(sheet.media);
      function rules(list) {
        for (const rule of list) {
          if (rule.media) swap(rule.media);
          if (rule.styleSheet) visit(rule.styleSheet);
          else if (rule.cssRules) rules(rule.cssRules);
        }
      }
      rules(sheet.cssRules);
    }
    for (const sheet of doc.styleSheets) visit(sheet);
    return () => changed.forEach(([media, original]) => { media.mediaText = original; });
  }

  button.addEventListener("click", async () => {
    if (busy) return;
    busy = true;
    button.disabled = true;
    status.hidden = false;
    status.textContent = "Checking PDF layout…";
    frame?.remove();
    // Source collection must not replace the report during the check.
    const refresh = document.getElementById("annual-refresh-all");
    const refreshDisabled = refresh?.disabled;
    if (refresh) refresh.disabled = true;
    try {
      const main = document.querySelector("main");
      if (!main?.querySelector(".paper-sheet")) throw new Error("The report is not ready. Reload and try again.");
      const source = main.innerHTML;
      const snapshot = document.implementation.createHTMLDocument(document.title);
      snapshot.documentElement.lang = document.documentElement.lang;
      const base = snapshot.createElement("base");
      base.href = document.baseURI;
      snapshot.head.append(base);
      for (const style of document.head.querySelectorAll('link[rel="stylesheet"], style')) snapshot.head.append(style.cloneNode(true));
      snapshot.body.className = document.body.className;
      snapshot.body.dataset.layout = "paper";
      const previewBanner = document.querySelector(".synthetic-banner");
      if (previewBanner) snapshot.body.append(previewBanner.cloneNode(true));
      snapshot.body.append(main.cloneNode(true));
      for (const img of snapshot.images) img.loading = "eager";
      frame = document.createElement("iframe");
      frame.title = "Prepared annual report";
      frame.setAttribute("aria-hidden", "true");
      frame.tabIndex = -1;
      frame.dataset.reportPrintFrame = "true";
      frame.style.cssText = "position:fixed;left:-10000px;top:0;width:210mm;height:297mm;border:0;";
      const loaded = new Promise(resolve => frame.addEventListener("load", resolve, {once:true}));
      frame.srcdoc = "<!doctype html>" + snapshot.documentElement.outerHTML;
      document.body.append(frame);
      await withDeadline(loaded);
      const doc = frame.contentDocument;
      if ([...doc.querySelectorAll('link[rel="stylesheet"]')].some(link => !link.sheet)) {
        throw new Error("Report styles could not load. Reload and try again.");
      }
      const restoreStyles = usePrintStyles(doc);
      // Trigger layout before waiting for the fonts used by the print styles.
      doc.body.getBoundingClientRect();
      await withDeadline(Promise.all([doc.fonts.ready, ...[...doc.images].map(img => img.decode())]));
      if ([...doc.fonts].some(font => font.status === "error")) throw new Error("Report fonts could not load. Reload and try again.");

      const ruler = doc.createElement("div");
      ruler.style.cssText = "position:absolute;width:210mm;height:297mm;";
      doc.body.append(ruler);
      const paper = ruler.getBoundingClientRect();
      ruler.remove();
      const sheets = [...doc.querySelectorAll("main .paper-sheet")];
      if (!sheets.length) throw new Error("The report is not ready. Reload and try again.");
      const oversized = [];
      sheets.forEach((sheet, index) => {
        const box = sheet.getBoundingClientRect();
        // Leave a small rounding allowance inside A4, never clip overflow.
        if (Math.max(box.height, sheet.scrollHeight) > paper.height - 1 ||
            Math.max(box.width, sheet.scrollWidth) > paper.width + 1) oversized.push(index + 1);
      });
      if (oversized.length) {
        throw new Error(`PDF not opened: ${oversized.length === 1 ? "page" : "pages"} ${oversized.join(", ")} ${oversized.length === 1 ? "needs" : "need"} more room. Shorten the text or split the content onto another page, then try again.`);
      }
      if (!document.dispatchEvent(new Event("report-before-print", {cancelable:true}))) {
        throw new Error("Wait for the narrative previews to update before exporting.");
      }
      if (main !== document.querySelector("main") || main.innerHTML !== source) {
        throw new Error("The report changed while preparing the PDF. Please try again.");
      }
      restoreStyles();
      status.textContent = `${sheets.length} pages fit A4. Save at 100% scale with browser headers and footers off.`;
      const prepared = frame;
      prepared.contentWindow.addEventListener("afterprint", () => prepared.remove(), {once:true});
      prepared.contentWindow.focus();
      prepared.contentWindow.print();
    } catch (error) {
      frame?.remove();
      status.textContent = error.name === "EncodingError"
        ? "Some report images could not load. Reload and try again."
        : error.message;
      status.scrollIntoView({block:"center"});
    } finally {
      busy = false;
      button.disabled = false;
      if (refresh) refresh.disabled = refreshDisabled;
    }
  });
})();
