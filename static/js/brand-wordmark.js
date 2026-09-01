(function () {
  'use strict';

  if (window.__btcppWordmarkInstalled) return;
  window.__btcppWordmarkInstalled = true;

  const wordmarkPattern = /bitcoin\+\+/gi;
  const ignoredParent = 'script, style, noscript, textarea, option, code, pre, kbd, samp, svg, math, [contenteditable], [data-btcpp-wordmark-skip], btcpp-wordmark';

  function makeWordmark(value) {
    const wordmark = document.createElement('btcpp-wordmark');
    const pluses = document.createElement('btcpp-pluses');
    const firstPlus = document.createElement('btcpp-plus');
    const secondPlus = document.createElement('btcpp-plus');

    wordmark.append(document.createTextNode(value.slice(0, -2)));
    firstPlus.textContent = '+';
    secondPlus.textContent = '+';
    pluses.append(firstPlus, secondPlus);
    wordmark.append(pluses);
    return wordmark;
  }

  function replaceTextNode(node) {
    if (!node.nodeValue || !wordmarkPattern.test(node.nodeValue)) return;
    wordmarkPattern.lastIndex = 0;
    if (!node.parentElement || node.parentElement.closest(ignoredParent)) return;

    const fragment = document.createDocumentFragment();
    let cursor = 0;
    let match;

    while ((match = wordmarkPattern.exec(node.nodeValue)) !== null) {
      if (match.index > cursor) {
        fragment.append(document.createTextNode(node.nodeValue.slice(cursor, match.index)));
      }
      fragment.append(makeWordmark(match[0]));
      cursor = match.index + match[0].length;
    }

    if (cursor < node.nodeValue.length) {
      fragment.append(document.createTextNode(node.nodeValue.slice(cursor)));
    }
    node.replaceWith(fragment);
  }

  function applyWordmarks(root) {
    if (root.nodeType === Node.TEXT_NODE) {
      replaceTextNode(root);
      return;
    }
    if (root.nodeType !== Node.ELEMENT_NODE || root.matches(ignoredParent)) return;

    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const textNodes = [];
    while (walker.nextNode()) textNodes.push(walker.currentNode);
    textNodes.forEach(replaceTextNode);
  }

  function install() {
    applyWordmarks(document.body);
    new MutationObserver(function (mutations) {
      mutations.forEach(function (mutation) {
        mutation.addedNodes.forEach(applyWordmarks);
      });
    }).observe(document.body, { childList: true, subtree: true });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', install, { once: true });
  } else {
    install();
  }
})();
