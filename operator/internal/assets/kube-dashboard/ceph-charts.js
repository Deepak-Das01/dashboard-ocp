/**
 * Ceph embed — remap Chart.js light-theme canvas colors to kubeops palette.
 * Chart.js is webpack-bundled (not on window), so we:
 *  1) patch CanvasRenderingContext2D fillStyle/strokeStyle on getContext
 *  2) patch already-created 2d contexts
 *  3) post-process canvas pixels after draws (Chart redraws wipe patches)
 */
(function () {
  "use strict";
  if (window.__kubeopsCephCharts) return;
  window.__kubeopsCephCharts = true;

  function mapStyle(v) {
    if (typeof v !== "string") return v;
    var s = v.replace(/\s+/g, "").toLowerCase();

    // Doughnut track / free space (white & near-white)
    if (
      s === "#fff" ||
      s === "#ffffff" ||
      s === "white" ||
      s === "#f0f0f0" ||
      s === "#f5f5f5" ||
      s === "#f8f9fa" ||
      s === "#e9ecef" ||
      s === "#dee2e6" ||
      s === "#eee" ||
      s === "#eeeeee" ||
      s === "rgb(255,255,255)" ||
      s === "rgb(240,240,240)" ||
      s === "rgb(248,249,250)" ||
      s === "rgb(233,236,239)" ||
      s === "rgb(222,226,230)" ||
      s === "rgb(215,215,215)" ||
      s === "rgb(247,247,247)" ||
      s === "rgb(253,253,253)" ||
      /^rgba\(255,255,255,/.test(s) ||
      /^rgba\(240,240,240,/.test(s)
    ) {
      // Visible track on dark cards (not near-invisible slate)
      return "rgba(120, 170, 230, 0.38)";
    }

    // Bootstrap / Chart primary blue → kubeops accent
    if (
      s === "#007bff" ||
      s === "#0d6efd" ||
      s === "#00a8e8" ||
      s === "#73c5c5" ||
      s === "rgb(0,123,255)" ||
      s === "rgb(13,110,253)" ||
      s === "rgb(0,168,232)"
    ) {
      return "#3f9cff";
    }

    // Warning
    if (
      s === "#ffc107" ||
      s === "#f0ad4e" ||
      s === "#fd7e14" ||
      s === "rgb(255,193,7)" ||
      s === "rgb(240,173,78)" ||
      s === "rgb(253,126,20)"
    ) {
      return "#FCD34D";
    }

    // Danger
    if (
      s === "#dc3545" ||
      s === "#d9534f" ||
      s === "#c92a2a" ||
      s === "rgb(220,53,69)" ||
      s === "rgb(217,83,79)"
    ) {
      return "#FCA5A5";
    }

    // Success / green
    if (
      s === "#28a745" ||
      s === "#198754" ||
      s === "#20c997" ||
      s === "rgb(40,167,69)" ||
      s === "rgb(25,135,84)"
    ) {
      return "#43D6A0";
    }

    // Mid grays (threshold ticks / muted series)
    if (
      s === "#6c757d" ||
      s === "#adb5bd" ||
      s === "#ced4da" ||
      s === "#999" ||
      s === "#999999" ||
      s === "#aaa" ||
      s === "#ccc" ||
      s === "rgb(108,117,125)" ||
      s === "rgb(173,181,189)" ||
      s === "rgb(175,175,175)" ||
      s === "rgb(114,118,123)" ||
      s === "rgb(206,212,218)"
    ) {
      return "rgba(180, 205, 235, 0.42)";
    }

    // Near-black text/lines → light
    if (
      s === "#000" ||
      s === "#000000" ||
      s === "black" ||
      s === "#212529" ||
      s === "#343a40" ||
      s === "#495057" ||
      s === "rgb(0,0,0)" ||
      s === "rgb(33,37,41)" ||
      s === "rgb(52,58,64)" ||
      /^rgba\(0,0,0,/.test(s)
    ) {
      return "#F0F5FC";
    }

    // Light grid lines
    if (
      s === "#ddd" ||
      s === "#dddddd" ||
      s === "#e0e0e0" ||
      s === "rgb(221,221,221)" ||
      s === "rgb(224,224,224)"
    ) {
      return "rgba(180, 205, 235, 0.16)";
    }

    return v;
  }

  function patchContext(ctx) {
    if (!ctx || ctx.__kubeopsStylePatched) return;
    ctx.__kubeopsStylePatched = true;
    try {
      var fs = mapStyle(String(ctx.fillStyle));
      var ss = mapStyle(String(ctx.strokeStyle));
      Object.defineProperty(ctx, "fillStyle", {
        configurable: true,
        enumerable: true,
        get: function () {
          return fs;
        },
        set: function (v) {
          fs = mapStyle(v);
        },
      });
      Object.defineProperty(ctx, "strokeStyle", {
        configurable: true,
        enumerable: true,
        get: function () {
          return ss;
        },
        set: function (v) {
          ss = mapStyle(v);
        },
      });
    } catch (e) {
      /* ignore */
    }
  }

  // Patch future contexts
  try {
    var _getContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (type, attrs) {
      var ctx = _getContext.call(this, type, attrs);
      if (ctx && (type === "2d" || type === "2D")) patchContext(ctx);
      return ctx;
    };
  } catch (e) {
    /* ignore */
  }

  function remapPixels(cv) {
    if (!cv || !cv.width || !cv.height) return;
    var ctx;
    try {
      ctx = cv.getContext("2d", { willReadFrequently: true });
    } catch (e) {
      ctx = cv.getContext("2d");
    }
    if (!ctx) return;
    patchContext(ctx);
    var W = cv.width;
    var H = cv.height;
    var img;
    try {
      img = ctx.getImageData(0, 0, W, H);
    } catch (e) {
      return;
    }
    var d = img.data;
    var changed = false;
    for (var i = 0; i < d.length; i += 4) {
      var a = d[i + 3];
      if (a < 8) continue;
      var r = d[i];
      var g = d[i + 1];
      var b = d[i + 2];

      // Near-white track → luminous sapphire ring (must read on dark cards)
      if (r >= 200 && g >= 200 && b >= 200) {
        d[i] = 110;
        d[i + 1] = 160;
        d[i + 2] = 220;
        d[i + 3] = Math.max(140, Math.round(a * 0.72));
        changed = true;
        continue;
      }

      // Already-remapped ultra-dark slate track (prior bad remap) → lift it
      if (r >= 40 && r <= 60 && g >= 55 && g <= 80 && b >= 85 && b <= 115 && a > 80) {
        d[i] = 110;
        d[i + 1] = 160;
        d[i + 2] = 220;
        d[i + 3] = Math.max(150, a);
        changed = true;
        continue;
      }

      // Bootstrap blue → bright kubeops accent (used arc)
      if (b > 200 && r < 60 && g > 85 && g < 170) {
        d[i] = 63;
        d[i + 1] = 156;
        d[i + 2] = 255;
        d[i + 3] = 255;
        changed = true;
        continue;
      }

      // Cyan-ish Ceph defaults
      if (g > 160 && b > 160 && r < 140 && b >= g - 30) {
        d[i] = 63;
        d[i + 1] = 156;
        d[i + 2] = 255;
        changed = true;
        continue;
      }

      // Mid gray marks
      if (Math.abs(r - g) < 18 && Math.abs(g - b) < 18 && r > 95 && r < 200) {
        d[i] = 180;
        d[i + 1] = 205;
        d[i + 2] = 235;
        d[i + 3] = Math.round(a * 0.55);
        changed = true;
        continue;
      }

      // Near-black strokes → light text color
      if (r < 45 && g < 45 && b < 45 && a > 80) {
        d[i] = 240;
        d[i + 1] = 245;
        d[i + 2] = 252;
        changed = true;
        continue;
      }

      // Very faint gray grid / zero lines → visible muted grid
      if (
        Math.abs(r - g) < 12 &&
        Math.abs(g - b) < 12 &&
        r > 60 &&
        r < 120 &&
        a > 20 &&
        a < 120
      ) {
        d[i] = 140;
        d[i + 1] = 170;
        d[i + 2] = 210;
        d[i + 3] = Math.max(70, a);
        changed = true;
      }
    }
    if (changed) ctx.putImageData(img, 0, 0);
  }

  function remapAll() {
    var list = document.querySelectorAll("canvas");
    for (var i = 0; i < list.length; i++) remapPixels(list[i]);
  }

  function isFlatCanvas(cv) {
    if (!cv || !cv.width || !cv.height) return true;
    try {
      var ctx = cv.getContext("2d", { willReadFrequently: true });
      if (!ctx) return true;
      var stepX = Math.max(4, Math.floor(cv.width / 40));
      var stepY = Math.max(2, Math.floor(cv.height / 30));
      var colorful = 0;
      var inkRows = 0;
      var seenRow = Object.create(null);
      for (var y = 0; y < cv.height; y += stepY) {
        for (var x = 0; x < cv.width; x += stepX) {
          var p = ctx.getImageData(x, y, 1, 1).data;
          if (p[3] < 40) continue;
          // Non-gray / non-near-black pixels count as series/chrome ink
          var max = Math.max(p[0], p[1], p[2]);
          var min = Math.min(p[0], p[1], p[2]);
          var isInk =
            (max - min > 25 && max > 70) || (max > 160 && p[3] > 120);
          if (!isInk) continue;
          colorful++;
          if (!seenRow[y]) {
            seenRow[y] = 1;
            inkRows++;
          }
        }
      }
      // Empty utilization charts are a flat baseline (1–2 sample rows),
      // even after pixel remap turns that baseline sapphire.
      if (colorful < 4) return true;
      return inkRows < 3;
    } catch (e) {
      return true;
    }
  }

  function ensureEmptyState(host, cv) {
    if (!host || !cv) return;
    // Prefer the chart plot host (inner .chart) so overlay sits on the canvas
    var plot =
      (host.classList && host.classList.contains("chart") && host) ||
      host.querySelector(".chart") ||
      host;
    var existing = null;
    var kids = plot.children || [];
    for (var ei = 0; ei < kids.length; ei++) {
      if (kids[ei].classList && kids[ei].classList.contains("kubeops-chart-empty")) {
        existing = kids[ei];
        break;
      }
    }
    var flat = isFlatCanvas(cv);
    if (flat) {
      if (!existing) {
        existing = document.createElement("div");
        existing.className = "kubeops-chart-empty";
        existing.setAttribute("role", "status");
        existing.innerHTML =
          '<span class="kubeops-chart-empty__label">No metrics for this range</span>';
        var cs = window.getComputedStyle(plot);
        if (cs.position === "static") plot.style.position = "relative";
        plot.appendChild(existing);
      }
      existing.style.display = "flex";
      plot.classList.add("kubeops-chart--empty");
      cv.style.opacity = "0.28";
    } else if (existing) {
      existing.style.display = "none";
      plot.classList.remove("kubeops-chart--empty");
      cv.style.opacity = "";
    }
  }

  function ensureChartWidth() {
    // Only outer area-chart hosts — avoid double-processing nested .chart
    var charts = document.querySelectorAll("cd-dashboard-area-chart");
    for (var i = 0; i < charts.length; i++) {
      var el = charts[i];
      if (el && el.style) {
        el.style.maxWidth = "100%";
        el.style.width = "100%";
      }
      var cv = el && el.querySelector && el.querySelector("canvas");
      if (cv) {
        cv.style.maxWidth = "100%";
        cv.style.width = "100%";
        cv.style.display = "block";
        ensureEmptyState(el, cv);
      }
    }
    // Generic non-pie charts elsewhere
    var generic = document.querySelectorAll(".chart:not(cd-dashboard-pie .chart)");
    for (var g = 0; g < generic.length; g++) {
      var gel = generic[g];
      if (gel.closest && gel.closest("cd-dashboard-area-chart")) continue;
      if (gel.closest && gel.closest("cd-dashboard-pie")) continue;
      var gcv = gel.querySelector && gel.querySelector("canvas");
      if (gcv) ensureEmptyState(gel, gcv);
    }
    // Nudge Chart.js responsive resize when parent size settles
    try {
      window.dispatchEvent(new Event("resize"));
    } catch (e) {
      /* ignore */
    }
  }

  function start() {
    ensureChartWidth();
    remapAll();
    // Chart.js redraws wipe pixels — keep them tinted
    setInterval(function () {
      ensureChartWidth();
      remapAll();
    }, 500);
    // Also on resize / visibility
    window.addEventListener(
      "resize",
      function () {
        ensureChartWidth();
        remapAll();
      },
      { passive: true }
    );
    document.addEventListener("visibilitychange", function () {
      if (!document.hidden) {
        ensureChartWidth();
        remapAll();
      }
    });
    // New canvases
    try {
      var mo = new MutationObserver(function (muts) {
        for (var i = 0; i < muts.length; i++) {
          var nodes = muts[i].addedNodes;
          for (var j = 0; j < nodes.length; j++) {
            var n = nodes[j];
            if (n && n.tagName === "CANVAS") remapPixels(n);
            else if (n && n.querySelectorAll) {
              var cvs = n.querySelectorAll("canvas");
              for (var k = 0; k < cvs.length; k++) remapPixels(cvs[k]);
            }
          }
        }
      });
      mo.observe(document.documentElement, { childList: true, subtree: true });
    } catch (e) {
      /* ignore */
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
