(function () {
  "use strict";

  document.querySelectorAll("[data-merch-social-source]").forEach(function (input) {
    var form = input.closest("form");
    var preview = form && form.querySelector("[data-merch-social-preview]");
    var image = preview && preview.querySelector("img");
    var empty = preview && preview.querySelector("p");
    var objectURL = "";
    if (!preview || !image) return;

    input.addEventListener("change", function () {
      if (objectURL) URL.revokeObjectURL(objectURL);
      objectURL = "";
      var file = input.files && input.files[0];
      if (!file) {
        image.hidden = true;
        image.removeAttribute("src");
        if (empty) empty.hidden = false;
        preview.classList.remove("has-image");
        return;
      }
      objectURL = URL.createObjectURL(file);
      image.src = objectURL;
      image.hidden = false;
      if (empty) empty.hidden = true;
      preview.classList.add("has-image");
    });
  });
})();
