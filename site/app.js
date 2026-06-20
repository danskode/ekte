// ekte landingpage — tema-toggle, copy-knapper og asciinema-player.
(function () {
  "use strict";

  // --- Tema-toggle (dark default, huskes i localStorage) ---
  var toggle = document.getElementById("theme-toggle");
  if (toggle) {
    toggle.addEventListener("click", function () {
      var cur = document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
      var next = cur === "light" ? "dark" : "light";
      document.documentElement.setAttribute("data-theme", next);
      try { localStorage.setItem("ekte-theme", next); } catch (e) {}
    });
  }

  // --- Copy-knapper ---
  document.querySelectorAll(".copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var el = document.getElementById(btn.getAttribute("data-copy"));
      if (!el) return;
      var text = el.innerText.trim();
      var done = function () {
        var orig = btn.textContent;
        btn.textContent = "Kopieret!";
        btn.classList.add("done");
        setTimeout(function () { btn.textContent = orig; btn.classList.remove("done"); }, 1500);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done).catch(fallback);
      } else { fallback(); }
      function fallback() {
        var ta = document.createElement("textarea");
        ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
        document.body.appendChild(ta); ta.select();
        try { document.execCommand("copy"); done(); } catch (e) {}
        document.body.removeChild(ta);
      }
    });
  });

  // --- asciinema-player ---
  var mount = document.getElementById("demo-player");
  if (mount && window.AsciinemaPlayer) {
    window.AsciinemaPlayer.create("demo.cast", mount, {
      autoPlay: true,
      loop: true,
      preload: true,
      idleTimeLimit: 1.5,
      theme: "asciinema",
      fit: "width"
    });
  }
})();
