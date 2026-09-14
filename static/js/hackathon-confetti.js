(function () {
  "use strict";

  const canvas = document.querySelector("[data-ballot-confetti]");
  if (!canvas || window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    return;
  }

  const context = canvas.getContext("2d");
  if (!context) return;

  const colors = ["#ff9f1c", "#111111", "#ffffff", "#4f46e5"];
  const pieces = [];
  const duration = 2800;
  const startedAt = performance.now();
  let width = 0;
  let height = 0;

  function resize() {
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    width = window.innerWidth;
    height = window.innerHeight;
    canvas.width = Math.floor(width * ratio);
    canvas.height = Math.floor(height * ratio);
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function addBurst(originX) {
    for (let index = 0; index < 70; index += 1) {
      const angle = -Math.PI * (0.2 + Math.random() * 0.6);
      const speed = 7 + Math.random() * 10;
      pieces.push({
        x: originX,
        y: height + 12,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed,
        gravity: 0.18 + Math.random() * 0.08,
        rotation: Math.random() * Math.PI,
        spin: (Math.random() - 0.5) * 0.3,
        width: 6 + Math.random() * 7,
        height: 3 + Math.random() * 5,
        color: colors[Math.floor(Math.random() * colors.length)]
      });
    }
  }

  function draw(now) {
    context.clearRect(0, 0, width, height);
    for (const piece of pieces) {
      piece.x += piece.vx;
      piece.y += piece.vy;
      piece.vy += piece.gravity;
      piece.vx *= 0.995;
      piece.rotation += piece.spin;

      context.save();
      context.translate(piece.x, piece.y);
      context.rotate(piece.rotation);
      context.fillStyle = piece.color;
      context.fillRect(-piece.width / 2, -piece.height / 2, piece.width, piece.height);
      context.restore();
    }

    if (now - startedAt < duration) {
      window.requestAnimationFrame(draw);
    } else {
      canvas.remove();
      window.removeEventListener("resize", resize);
    }
  }

  resize();
  addBurst(width * 0.18);
  addBurst(width * 0.82);
  window.addEventListener("resize", resize);
  window.requestAnimationFrame(draw);
})();
