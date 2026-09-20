"use strict";
const printButton = document.getElementById("report-print");
if (printButton) printButton.addEventListener("click", () => window.print());
