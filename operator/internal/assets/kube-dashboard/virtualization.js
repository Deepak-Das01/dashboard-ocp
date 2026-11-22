/**
 * In-console Virtualization view — embeds kubevirt-manager UI.
 *
 * Route: #/workloads?view=virtualization (exact workloads overview only)
 * Nav tile still sits before CRDs; hash is independent so CRD/Settings/etc. leave cleanly.
 *
 * Leave hardening (same class of bugs as Addons):
 * - Angular queryParamsHandling=preserve reattaches view=virtualization
 * - Overlay must hide + strip param before Angular navigates
 * - show() must not remount the iframe on every sync tick
 */
(function () {
  "use strict";

  var VIEW_ID = "lg-virt-view";
  var FRAME_SRC = "/kubeops/virt-ui/";
  var OPS_API = "/kubeops/api/operators/kubeops-virtualization";
  var bound = false;
  var healing = false;
  var loadedOnce = false;
  var leaveGuardTimer = null;
  var leaveGuardUntil = 0;
  var pinTimer = null;
  var pinUntil = 0;

  function isExactWorkloadsPath(hash) {
    return /^#\/workloads(?:\?|$)/i.test(hash || "");
  }

  function hasVirtView(hash) {
    return /(?:[?&])view=virtualization(?:&|$)/.test(hash || "");
  }

  function onVirtRoute() {
    var h = location.hash || "";
    return isExactWorkloadsPath(h) && hasVirtView(h);
  }

  function namespaceFromHash(hash) {
    try {
      var m = (hash || location.hash || "").match(/[?&]namespace=([^&]*)/);
      if (m && m[1]) return decodeURIComponent(m[1]) || "default";
    } catch (e) {
      /* ignore */
    }
    return "default";
  }

  function virtHref() {
    return (
      "#/workloads?namespace=" +
      encodeURIComponent(namespaceFromHash()) +
      "&view=virtualization"
    );
  }

  function workloadsHrefPlain() {
    return "#/workloads?namespace=" + encodeURIComponent(namespaceFromHash());
  }

  function contentHost() {
    var hosts = document.querySelectorAll(".kd-scroll-container");
    if (hosts.length) return hosts[hosts.length - 1];
    return (
      document.querySelector("mat-drawer-content.mat-drawer-content") ||
      document.querySelector(".mat-drawer-content")
    );
  }

  function armLeaveGuard() {
    leaveGuardUntil = Date.now() + 2000;
    if (leaveGuardTimer) clearInterval(leaveGuardTimer);
    leaveGuardTimer = setInterval(function () {
      if (Date.now() > leaveGuardUntil) {
        clearInterval(leaveGuardTimer);
        leaveGuardTimer = null;
        leaveGuardUntil = 0;
        return;
      }
      var h = location.hash || "";
      if (!hasVirtView(h)) return;
      if (!isExactWorkloadsPath(h)) {
        // Stuck on a non-workloads route with view= — strip.
        var cleaned = stripVirtParam(h) || workloadsHrefPlain();
        if (cleaned !== h) {
          location.replace(location.pathname + location.search + cleaned);
        }
        return;
      }
      // Angular reattached view= on plain workloads after leave — strip.
      if (onVirtRoute()) {
        var plain = stripVirtParam(h) || workloadsHrefPlain();
        if (plain !== h) {
          hide();
          location.replace(location.pathname + location.search + plain);
        }
      }
    }, 50);
  }

  function clearLeaveGuard() {
    leaveGuardUntil = 0;
    if (leaveGuardTimer) {
      clearInterval(leaveGuardTimer);
      leaveGuardTimer = null;
    }
  }

  function ensureView(host) {
    var orphans = document.querySelectorAll(".lg-virt-view");
    for (var i = 0; i < orphans.length; i++) {
      if (orphans[i].parentElement !== host) orphans[i].remove();
    }
    var view = host.querySelector("#" + VIEW_ID);
    if (view) return view;

    view = document.createElement("div");
    view.id = VIEW_ID;
    view.className = "lg-virt-view";
    view.innerHTML =
      '<div class="lg-virt-shell">' +
      '  <div class="lg-virt-panel">' +
      '    <div class="lg-virt-loading" data-virt-root>Loading Virtualization UI…</div>' +
      "  </div>" +
      "</div>";
    host.appendChild(view);
    loadedOnce = false;
    return view;
  }

  function renderUnavailable(root, msg) {
    loadedOnce = false;
    root.className = "lg-virt-loading lg-virt-error";
    root.innerHTML =
      "<p><strong>Virtualization UI is not available.</strong></p>" +
      "<p>" +
      String(msg || "Deploy kubeops-virtualization from Addons first.") +
      "</p>" +
      '<p><a class="lg-virt-link" href="#/settings?view=addons">Open Addons</a></p>';
  }

  function guardIframe(frame) {
    if (!frame || frame.getAttribute("data-lg-virt-guarded") === "1") return;
    frame.setAttribute("data-lg-virt-guarded", "1");

    function prefixOk(pathname) {
      return String(pathname || "").indexOf("/kubeops/virt-ui") === 0;
    }

    function fixBrand(doc) {
      if (!doc) return;
      var brands = doc.querySelectorAll('a.brand-link, a[href="/"]');
      for (var i = 0; i < brands.length; i++) {
        var href = brands[i].getAttribute("href");
        if (href === "/" || href === "") {
          brands[i].setAttribute("href", FRAME_SRC);
        }
      }
    }

    function reclaimIfEscaped() {
      try {
        var win = frame.contentWindow;
        if (!win || !win.location) return;
        // Same-origin only.
        if (win.location.origin !== window.location.origin) return;
        var path = win.location.pathname || "";
        if (!prefixOk(path)) {
          // Brand "/" (and similar) escaped into the main dashboard inside the iframe.
          win.location.replace(FRAME_SRC);
          return;
        }
        fixBrand(frame.contentDocument);
      } catch (e) {
        /* ignore cross-origin */
      }
    }

    frame.addEventListener("load", function () {
      reclaimIfEscaped();
      try {
        var doc = frame.contentDocument;
        if (!doc || doc.documentElement.getAttribute("data-lg-virt-click") === "1") {
          return;
        }
        doc.documentElement.setAttribute("data-lg-virt-click", "1");
        doc.addEventListener(
          "click",
          function (evt) {
            var a = evt.target && evt.target.closest && evt.target.closest("a");
            if (!a) return;
            var href = a.getAttribute("href") || "";
            // Absolute site-root links break out of /kubeops/virt-ui/ into the console.
            if (href === "/" || href === "") {
              evt.preventDefault();
              evt.stopPropagation();
              try {
                frame.contentWindow.location.href = FRAME_SRC;
              } catch (e) {
                /* ignore */
              }
              return;
            }
            // Never allow breaking out of the iframe into the parent shell.
            if (a.target === "_top" || a.target === "_parent") {
              a.target = "_self";
            }
          },
          true
        );
      } catch (e) {
        /* ignore */
      }
    });

    // Angular may re-render the brand link; reclaim periodically while visible.
    var guardTimer = setInterval(function () {
      if (!document.body.contains(frame) || !onVirtRoute()) {
        clearInterval(guardTimer);
        return;
      }
      reclaimIfEscaped();
    }, 500);
  }

  function renderFrame(root) {
    root.className = "lg-virt-frame-wrap";
    root.innerHTML =
      '<iframe class="lg-virt-frame" title="kubevirt-manager" src="' +
      FRAME_SRC +
      '" referrerpolicy="same-origin" allow="fullscreen"></iframe>';
    loadedOnce = true;
    var frame = root.querySelector(".lg-virt-frame");
    guardIframe(frame);
  }

  function load(view, force) {
    var root = view.querySelector("[data-virt-root]");
    if (!root) return;
    if (loadedOnce && !force && root.querySelector(".lg-virt-frame")) return;

    root.className = "lg-virt-loading";
    root.textContent = "Loading Virtualization UI…";

    fetch(OPS_API, { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (res) {
        if (res.status === 404) return null;
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.json();
      })
      .then(function (op) {
        if (!onVirtRoute()) return;
        var phase = op && op.status && String(op.status.phase || "").toLowerCase();
        if (op && phase && phase !== "ready" && phase !== "progressing") {
          renderUnavailable(
            root,
            (op.status && op.status.message) ||
              "kubeops-virtualization is not Ready. Deploy from Addons."
          );
          return;
        }
        return fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" }).then(function (r) {
          if (!onVirtRoute()) return;
          if (r.ok || r.status === 304) renderFrame(root);
          else
            renderUnavailable(
              root,
              "UI proxy returned HTTP " +
                r.status +
                ". Ensure kubevirt-manager is running in kubeops-virtualization."
            );
        });
      })
      .catch(function (err) {
        if (!onVirtRoute()) return;
        fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" })
          .then(function (r) {
            if (!onVirtRoute()) return;
            if (r.ok || r.status === 304) renderFrame(root);
            else
              renderUnavailable(
                root,
                (err && err.message ? err.message + " — " : "") +
                  "UI not reachable (HTTP " +
                  r.status +
                  ")."
              );
          })
          .catch(function () {
            if (!onVirtRoute()) return;
            renderUnavailable(root, (err && err.message) || "Could not reach Virtualization UI.");
          });
      });
  }

  function setBreadcrumbs(active) {
    document.documentElement.classList.toggle("lg-virt-route", !!active);
    document.body.classList.toggle("lg-virt-route", !!active);
    var host =
      document.querySelector(".kd-breadcrumbs-container") ||
      document.querySelector("kd-breadcrumbs .kd-breadcrumbs-container");
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-virt-breadcrumbs");
      if (custom) custom.remove();
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-virt-breadcrumbs")) continue;
        kids[i].style.removeProperty("display");
      }
      return;
    }
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-virt-breadcrumbs")) continue;
      kids2[j].style.setProperty("display", "none", "important");
    }
    var bar = host.querySelector(".lg-virt-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-virt-breadcrumbs kd-inline-breadcrumb";
      host.appendChild(bar);
    }
    bar.innerHTML = '<span class="kd-current-breadcrumb">Virtualization</span>';
  }


  function clearPin() {
    pinUntil = 0;
    if (pinTimer) {
      clearInterval(pinTimer);
      pinTimer = null;
    }
  }

  function armPin() {
    pinUntil = Date.now() + 3500;
    if (pinTimer) clearInterval(pinTimer);
    pinTimer = setInterval(function () {
      if (Date.now() > pinUntil) {
        clearPin();
        return;
      }
      var h = location.hash || "";
      if (!isExactWorkloadsPath(h)) return;
      if (!hasVirtView(h)) {
        var next = virtHref();
        if (h !== next) {
          location.replace(location.pathname + location.search + next);
        }
      }
      if (!document.getElementById(VIEW_ID) || !document.documentElement.classList.contains("lg-virt-route")) {
        show();
      }
    }, 40);
  }

  /** One-click entry: set hash, show overlay immediately, pin against Angular stripping view=. */
  function enter() {
    clearLeaveGuard();
    try {
      if (typeof window.__lgHideMon === "function") window.__lgHideMon();
    } catch (e) {
      /* ignore */
    }
    var next = virtHref();
    if (location.hash !== next) {
      location.hash = next;
    }
    armPin();
    show();
    try {
      window.dispatchEvent(new Event("hashchange"));
    } catch (e) {
      /* ignore */
    }
  }

  function syncNavActive() {
    var active = onVirtRoute();
    var link = document.querySelector("#nav-virtualization a.kd-nav-item-button");
    if (link) {
      link.href = virtHref();
      link.classList.toggle("kd-nav-item-button-active", active);
    }
    // Workloads parent tile — suppress while overlay is open (like Settings/Addons).
    var wl =
      document.querySelector("#nav-workloads > a.kd-nav-item-button") ||
      document.querySelector('#nav-workloads a.kd-nav-item-button');
    if (wl) {
      if (active) {
        if (wl.classList.contains("kd-nav-item-button-active")) {
          wl.classList.remove("kd-nav-item-button-active");
          wl.setAttribute("data-lg-virt-suppressed-active", "1");
        }
      } else if (wl.getAttribute("data-lg-virt-suppressed-active") === "1") {
        wl.removeAttribute("data-lg-virt-suppressed-active");
        if (isExactWorkloadsPath(location.hash) && !hasVirtView(location.hash)) {
          wl.classList.add("kd-nav-item-button-active");
        }
      }
    }
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-virt-active");
    view.hidden = false;
    view.style.display = "";
    load(view, false);
    setBreadcrumbs(true);
    syncNavActive();
    return true;
  }

  function hide() {
    loadedOnce = false;
    clearPin();
    clearLeaveGuard();
    var hosts = document.querySelectorAll(
      ".kd-scroll-container.lg-virt-active, .mat-drawer-content.lg-virt-active, .lg-virt-active"
    );
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-virt-active");
    }
    // Belt-and-suspenders: never leave the host stuck hiding Angular content.
    var allScroll = document.querySelectorAll(".kd-scroll-container");
    for (var s = 0; s < allScroll.length; s++) {
      allScroll[s].classList.remove("lg-virt-active");
    }
    var views = document.querySelectorAll(".lg-virt-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    setBreadcrumbs(false);
    syncNavActive();
  }

  function stripVirtParam(hash) {
    if (!hash) return hash;
    return hash
      .replace(/([?&])view=virtualization(&|$)/g, function (_, sep, end) {
        if (sep === "?" && end === "&") return "?";
        if (sep === "?" && end === "") return "";
        if (sep === "&" && end === "") return "";
        return end === "&" ? sep : "";
      })
      .replace(/\?&/, "?")
      .replace(/[?&]$/, "");
  }

  function healStickyVirtParam() {
    var h = location.hash || "";
    if (!hasVirtView(h)) return false;
    if (isExactWorkloadsPath(h)) return false;
    if (healing) return true;
    healing = true;
    var cleaned = stripVirtParam(h) || workloadsHrefPlain();
    hide();
    if (cleaned !== h) {
      location.replace(location.pathname + location.search + cleaned);
    }
    setTimeout(function () {
      healing = false;
    }, 0);
    return true;
  }

  function sync() {
    if (healStickyVirtParam()) return;
    // While pinning after a nav click, keep overlay up even if Angular briefly strips view=.
    if (Date.now() < pinUntil && isExactWorkloadsPath(location.hash || "")) {
      if (!hasVirtView(location.hash || "")) {
        var pinned = virtHref();
        if (location.hash !== pinned) {
          location.replace(location.pathname + location.search + pinned);
        }
      }
      clearLeaveGuard();
      show();
      return;
    }
    // Leave-guard: Angular reattached view=virtualization on workloads — strip.
    if (leaveGuardUntil > Date.now() && onVirtRoute()) {
      var cleaned = stripVirtParam(location.hash || "") || workloadsHrefPlain();
      if (cleaned && cleaned !== location.hash) {
        hide();
        location.replace(location.pathname + location.search + cleaned);
        return;
      }
    }
    if (onVirtRoute()) {
      clearLeaveGuard();
      show();
    } else {
      hide();
    }
  }

  function leaveTo(href) {
    hide();
    var next = stripVirtParam(href) || href;
    if (!next || next === "#") next = workloadsHrefPlain();
    if (next.charAt(0) !== "#") next = "#" + next;
    // If destination is workloads overview, arm guard against preserve reattach.
    if (isExactWorkloadsPath(next) || /nav-workloads/.test(href)) {
      /* handled below via viaWorkloadsNav */
    }
    location.hash = next;
  }

  function bind() {
    if (bound) return;
    bound = true;
    window.addEventListener("hashchange", sync);

    document.addEventListener(
      "click",
      function (evt) {
        var a = evt.target && evt.target.closest && evt.target.closest("a");
        if (!a) return;

        // Own tile: enter immediately (capture) so one click always works.
        if (a.closest && a.closest("#nav-virtualization")) {
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          enter();
          return;
        }
        // Sibling overlay tile — do not intercept.
        if (a.closest && a.closest("#nav-monitoring")) {
          clearLeaveGuard();
          return;
        }

        var href = a.getAttribute("href") || "";
        var viaWorkloadsNav = !!(a.closest && a.closest("#nav-workloads"));
        var leaving = onVirtRoute() || !!document.querySelector(".lg-virt-view");

        // In-overlay Addons link
        if (a.classList && a.classList.contains("lg-virt-link")) {
          if (!leaving) return;
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          hide();
          location.hash = href.charAt(0) === "#" ? href : "#/settings?view=addons";
          return;
        }

        if (href.charAt(0) !== "#") return;

        var hrefIsVirt = isExactWorkloadsPath(href) && hasVirtView(href);
        if (hrefIsVirt && !viaWorkloadsNav) return;

        if (!(leaving || hasVirtView(href) || hasVirtView(location.hash))) return;

        // Leaving Virtualization — intercept before Angular preserve sticks the param.
        evt.preventDefault();
        evt.stopPropagation();
        if (typeof evt.stopImmediatePropagation === "function") {
          evt.stopImmediatePropagation();
        }
        hide();

        var next;
        if (viaWorkloadsNav) {
          next = workloadsHrefPlain();
        } else {
          next = stripVirtParam(href) || href;
        }
        if (!next || next === "#") next = workloadsHrefPlain();
        if (next.charAt(0) !== "#") next = "#" + next;
        location.hash = next;

        if (viaWorkloadsNav || isExactWorkloadsPath(next)) {
          armLeaveGuard();
        }
      },
      true
    );
  }

  function boot() {
    bind();
    // Expose href helper for nav-accordion (optional).
    try {
      window.__lgVirtHref = virtHref;
      window.__lgEnterVirt = enter;
      window.__lgHideVirt = hide;
    } catch (e) {
      /* ignore */
    }
    var tries = 0;
    var t = setInterval(function () {
      tries += 1;
      if (contentHost() || tries > 80) {
        clearInterval(t);
        sync();
      }
    }, 200);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
