"use strict";

// Chromium expands PNG/AVIF/WebP artwork into large PDF image streams. Prepare
// bounded JPEGs in the print snapshot, leaving the editor and source assets alone.
async function optimizeReportPrintImages(doc, signal) {
  const pixelsPerCSSPixel = 2; // 192 dpi, independent of the screen's pixel density.
  const maxDimension = 1600;
  const groups = new Map();
  for (const img of doc.querySelectorAll("main img")) {
    if (!img.getClientRects().length || !img.naturalWidth ||
        img.closest("[data-print-original], .report-recipient-logo, .annual-recipient-logo, .report-sponsor-mark")) continue;
    const src = img.currentSrc || img.src;
    if (!src || /(?:\.svg|\.gif)(?:[?#]|$)|^data:image\/(?:svg\+xml|gif)/i.test(src)) continue;
    if (!groups.has(src)) groups.set(src, []);
    const style = doc.defaultView.getComputedStyle(img);
    groups.get(src).push({img, width: parseFloat(style.width), height: parseFloat(style.height), fit: style.objectFit});
  }

  const result = {optimized: 0, original: 0};
  async function optimize(src, uses) {
    const controller = new AbortController();
    const abort = () => controller.abort();
    signal.addEventListener("abort", abort, {once: true});
    const timer = setTimeout(abort, 5000);
    let url, canvas;
    try {
      signal.throwIfAborted();
      const response = await fetch(src, {credentials: "same-origin", signal: controller.signal});
      if (!response.ok) throw new Error("Image unavailable for optimization");
      const blob = await response.blob();
      if (/image\/(svg\+xml|gif)/i.test(blob.type)) return;
      // A separate CORS-readable image also handles portraits on the asset CDN.
      const image = new doc.defaultView.Image();
      url = URL.createObjectURL(blob);
      image.src = url;
      await image.decode();
      let scale = 0;
      for (const use of uses) {
        const widthScale = use.width / image.naturalWidth;
        const heightScale = use.height / image.naturalHeight;
        const fitted = use.fit === "contain" || use.fit === "scale-down"
          ? Math.min(widthScale, heightScale) : Math.max(widthScale, heightScale);
        scale = Math.max(scale, fitted * pixelsPerCSSPixel);
      }
      scale = Math.min(1, scale, maxDimension / Math.max(image.naturalWidth, image.naturalHeight));
      if (!(scale > 0)) return;
      const width = Math.max(1, Math.round(image.naturalWidth * scale));
      const height = Math.max(1, Math.round(image.naturalHeight * scale));
      const resized = width !== image.naturalWidth || height !== image.naturalHeight;
      if (blob.type === "image/jpeg" && !resized) return;
      canvas = doc.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const context = canvas.getContext("2d", {willReadFrequently: true});
      context.imageSmoothingQuality = "high";
      context.drawImage(image, 0, 0, width, height);
      const pixels = context.getImageData(0, 0, width, height).data;
      let transparent = false;
      for (let i = 3; i < pixels.length; i += 4) {
        if (pixels[i] !== 255) { transparent = true; break; }
      }
      if (transparent && !resized) return;
      // Preserve alpha instead of flattening logos onto white or black.
      const encoded = canvas.toDataURL(transparent ? "image/png" : "image/jpeg", 0.9);
      if (!encoded.startsWith("data:image/")) throw new Error("Image encoding failed");
      // Small existing JPEGs/PNGs can already be cheaper than the new encoding.
      if ((blob.type === "image/jpeg" || (transparent && blob.type === "image/png")) &&
          (encoded.length - encoded.indexOf(",") - 1) * 0.75 >= blob.size) return;
      const check = new doc.defaultView.Image();
      check.src = encoded;
      await check.decode();
      signal.throwIfAborted();
      for (const {img, width, height} of uses) {
        // Keep the measured print layout even when CSS uses intrinsic sizing.
        img.style.width = `${width}px`;
        img.style.height = `${height}px`;
        img.removeAttribute("srcset");
        img.removeAttribute("sizes");
        for (const source of img.closest("picture")?.querySelectorAll("source") || []) source.remove();
        img.src = encoded; // The same URL for every occurrence lets PDF reuse it.
      }
      await Promise.all(uses.map(({img}) => img.decode()));
      result.optimized++;
    } catch (error) {
      if (signal.aborted) throw error;
      // A CDN without CORS must not erase a portrait or block a usable report.
      result.original++;
    } finally {
      clearTimeout(timer);
      signal.removeEventListener("abort", abort);
      if (url) URL.revokeObjectURL(url);
      if (canvas) canvas.width = canvas.height = 0;
    }
  }

  // Limit decoded images in memory; large source artwork can exceed 100 MB.
  const queue = groups.entries();
  async function worker() {
    for (const [src, uses] of queue) {
      signal.throwIfAborted();
      await optimize(src, uses);
    }
  }
  await Promise.all([worker(), worker()]);
  return result;
}
