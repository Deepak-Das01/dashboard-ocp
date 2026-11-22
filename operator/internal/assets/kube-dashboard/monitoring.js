/**
 * In-console Monitoring view — embeds Grafana UI.
 *
 * Route: #/workloads?view=monitoring (exact workloads overview only)
 * Nav tile still sits before CRDs; hash is independent so CRD/Settings/etc. leave cleanly.
 *
 * Leave hardening (same class of bugs as Addons):
 * - Angular queryParamsHandling=preserve reattaches view=monitoring
 * - Overlay must hide + strip param before Angular navigates
 * - show() must not remount the iframe on every sync tick
 */
(function () {
  "use strict";

  var VIEW_ID = "lg-mon-view";
  // Land on Enterprise Cluster Observability (default kubeops monitoring page).
  var FRAME_SRC = "/kubeops/mon-ui/d/kubeops-enterprise/?orgId=1";
  var OPS_API = "/kubeops/api/operators/kubeops-monitoring";
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

  function hasMonView(hash) {
    return /(?:[?&])view=monitoring(?:&|$)/.test(hash || "");
  }

  function onMonRoute() {
    var h = location.hash || "";
    return isExactWorkloadsPath(h) && hasMonView(h);
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

  function monHref() {
    return (
      "#/workloads?namespace=" +
      encodeURIComponent(namespaceFromHash()) +
      "&view=monitoring"
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
      if (!hasMonView(h)) return;
      if (!isExactWorkloadsPath(h)) {
        // Stuck on a non-workloads route with view= — strip.
        var cleaned = stripMonParam(h) || workloadsHrefPlain();
        if (cleaned !== h) {
          location.replace(location.pathname + location.search + cleaned);
        }
        return;
      }
      // Angular reattached view= on plain workloads after leave — strip.
      if (onMonRoute()) {
        var plain = stripMonParam(h) || workloadsHrefPlain();
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
    var orphans = document.querySelectorAll(".lg-mon-view");
    for (var i = 0; i < orphans.length; i++) {
      if (orphans[i].parentElement !== host) orphans[i].remove();
    }
    var view = host.querySelector("#" + VIEW_ID);
    if (view) return view;

    view = document.createElement("div");
    view.id = VIEW_ID;
    view.className = "lg-mon-view";
    view.innerHTML =
      '<div class="lg-mon-shell">' +
      '  <div class="lg-mon-panel">' +
      '    <div class="lg-mon-loading" data-mon-root>Loading Monitoring UI…</div>' +
      "  </div>" +
      "</div>";
    host.appendChild(view);
    loadedOnce = false;
    return view;
  }

  function renderUnavailable(root, msg) {
    loadedOnce = false;
    root.className = "lg-mon-loading lg-mon-error";
    root.innerHTML =
      "<p><strong>Monitoring UI is not available.</strong></p>" +
      "<p>" +
      String(msg || "Deploy kubeops-monitoring from Addons first.") +
      "</p>" +
      '<p><a class="lg-mon-link" href="#/settings?view=addons">Open Addons</a></p>';
  }

  function guardIframe(frame) {
    if (!frame || frame.getAttribute("data-lg-mon-guarded") === "1") return;
    frame.setAttribute("data-lg-mon-guarded", "1");

    function prefixOk(pathname) {
      return String(pathname || "").indexOf("/kubeops/mon-ui") === 0;
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
        if (!doc) return;
        // Hide Grafana "Sign in" + Unauthorized toasts (anonymous/proxy embed).
        function hideSignIn() {
          try {
            var nodes = doc.querySelectorAll("a,button,[role='button']");
            for (var i = 0; i < nodes.length; i++) {
              var el = nodes[i];
              var href = el.getAttribute("href") || "";
              var label = (el.getAttribute("aria-label") || "") + " " + (el.textContent || "");
              if (/forceLogin/i.test(href) || /^\s*sign\s*in\s*$/i.test(label.trim()) || /sign\s*in/i.test(el.getAttribute("aria-label") || "")) {
                el.style.setProperty("display", "none", "important");
                el.setAttribute("hidden", "true");
                el.setAttribute("aria-hidden", "true");
                if (el.parentNode) el.parentNode.removeChild(el);
              }
            }
            var alerts = doc.querySelectorAll(
              '[data-testid="data-testid Alert error"],[data-testid="data-testid Alert warning"],[role="alert"],[role="status"]'
            );
            for (var j = 0; j < alerts.length; j++) {
              var a = alerts[j];
              var t = (a.textContent || "").trim();
              if (/unauthorized|login|sign in|forbidden/i.test(t)) {
                a.style.setProperty("display", "none", "important");
                if (a.parentNode) a.parentNode.removeChild(a);
              }
            }
          } catch (e) {
            /* ignore */
          }
        }
        if (doc.documentElement.getAttribute("data-lg-mon-signin-hide") !== "1") {
          doc.documentElement.setAttribute("data-lg-mon-signin-hide", "1");
          var st = doc.createElement("style");
          st.textContent =
            'a[href*="forceLogin"],a[href*="login"],button[aria-label="Sign in"],[aria-label="Sign in"]{display:none!important;visibility:hidden!important;pointer-events:none!important;opacity:0!important}';
          (doc.head || doc.documentElement).appendChild(st);
          hideSignIn();
          try {
            var mo = new doc.defaultView.MutationObserver(hideSignIn);
            mo.observe(doc.body || doc.documentElement, { childList: true, subtree: true });
          } catch (e2) {
            /* ignore */
          }
        } else {
          hideSignIn();
        }
        if (doc.documentElement.getAttribute("data-lg-mon-click") === "1") {
          return;
        }
        doc.documentElement.setAttribute("data-lg-mon-click", "1");
        doc.addEventListener(
          "click",
          function (evt) {
            var a = evt.target && evt.target.closest && evt.target.closest("a");
            if (!a) return;
            var href = a.getAttribute("href") || "";
            // Absolute site-root links break out of /kubeops/mon-ui/ into the console.
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
      if (!document.body.contains(frame) || !onMonRoute()) {
        clearInterval(guardTimer);
        return;
      }
      reclaimIfEscaped();
    }, 500);
  }

  function renderFrame(root) {
    root.className = "lg-mon-frame-wrap";
    root.innerHTML =
      '<iframe class="lg-mon-frame" title="Grafana" src="' +
      FRAME_SRC +
      '" referrerpolicy="same-origin" allow="fullscreen"></iframe>';
    loadedOnce = true;
    var frame = root.querySelector(".lg-mon-frame");
    guardIframe(frame);
  }

  function load(view, force) {
    var root = view.querySelector("[data-mon-root]");
    if (!root) return;
    if (loadedOnce && !force && root.querySelector(".lg-mon-frame")) return;

    root.className = "lg-mon-loading";
    root.textContent = "Loading Monitoring UI…";

    fetch(OPS_API, { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (res) {
        if (res.status === 404) return null;
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.json();
      })
      .then(function (op) {
        if (!onMonRoute()) return;
        var phase = op && op.status && String(op.status.phase || "").toLowerCase();
        if (op && phase && phase !== "ready" && phase !== "progressing") {
          renderUnavailable(
            root,
            (op.status && op.status.message) ||
              "kubeops-monitoring is not Ready. Deploy from Addons."
          );
          return;
        }
        return fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" }).then(function (r) {
          if (!onMonRoute()) return;
          if (r.ok || r.status === 304) renderFrame(root);
          else
            renderUnavailable(
              root,
              "UI proxy returned HTTP " +
                r.status +
                ". Ensure Grafana is running in kubeops-monitoring."
            );
        });
      })
      .catch(function (err) {
        if (!onMonRoute()) return;
        fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" })
          .then(function (r) {
            if (!onMonRoute()) return;
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
            if (!onMonRoute()) return;
            renderUnavailable(root, (err && err.message) || "Could not reach Monitoring UI.");
          });
      });
  }

  function setBreadcrumbs(active) {
    document.documentElement.classList.toggle("lg-mon-route", !!active);
    document.body.classList.toggle("lg-mon-route", !!active);
    var host =
      document.querySelector(".kd-breadcrumbs-container") ||
      document.querySelector("kd-breadcrumbs .kd-breadcrumbs-container");
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-mon-breadcrumbs");
      if (custom) custom.remove();
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-mon-breadcrumbs")) continue;
        kids[i].style.removeProperty("display");
      }
      return;
    }
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-mon-breadcrumbs")) continue;
      kids2[j].style.setProperty("display", "none", "important");
    }
    var bar = host.querySelector(".lg-mon-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-mon-breadcrumbs kd-inline-breadcrumb";
      host.appendChild(bar);
    }
    bar.innerHTML = '<span class="kd-current-breadcrumb">Monitoring</span>';
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
      if (!hasMonView(h)) {
        var next = monHref();
        if (h !== next) {
          location.replace(location.pathname + location.search + next);
        }
      }
      if (!document.getElementById(VIEW_ID) || !document.documentElement.classList.contains("lg-mon-route")) {
        show();
      }
    }, 40);
  }

  /** One-click entry: set hash, show overlay immediately, pin against Angular stripping view=. */
  function enter() {
    clearLeaveGuard();
    try {
      if (typeof window.__lgHideVirt === "function") window.__lgHideVirt();
    } catch (e) {
      /* ignore */
    }
    var next = monHref();
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
    var active = onMonRoute();
    var link = document.querySelector("#nav-monitoring a.kd-nav-item-button");
    if (link) {
      link.href = monHref();
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
          wl.setAttribute("data-lg-mon-suppressed-active", "1");
        }
      } else if (wl.getAttribute("data-lg-mon-suppressed-active") === "1") {
        wl.removeAttribute("data-lg-mon-suppressed-active");
        if (isExactWorkloadsPath(location.hash) && !hasMonView(location.hash)) {
          wl.classList.add("kd-nav-item-button-active");
        }
      }
    }
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-mon-active");
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
      ".kd-scroll-container.lg-mon-active, .mat-drawer-content.lg-mon-active, .lg-mon-active"
    );
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-mon-active");
    }
    // Belt-and-suspenders: never leave the host stuck hiding Angular content.
    var allScroll = document.querySelectorAll(".kd-scroll-container");
    for (var s = 0; s < allScroll.length; s++) {
      allScroll[s].classList.remove("lg-mon-active");
    }
    var views = document.querySelectorAll(".lg-mon-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    setBreadcrumbs(false);
    syncNavActive();
  }

  function stripMonParam(hash) {
    if (!hash) return hash;
    return hash
      .replace(/([?&])view=monitoring(&|$)/g, function (_, sep, end) {
        if (sep === "?" && end === "&") return "?";
        if (sep === "?" && end === "") return "";
        if (sep === "&" && end === "") return "";
        return end === "&" ? sep : "";
      })
      .replace(/\?&/, "?")
      .replace(/[?&]$/, "");
  }

  function healStickyMonParam() {
    var h = location.hash || "";
    if (!hasMonView(h)) return false;
    if (isExactWorkloadsPath(h)) return false;
    if (healing) return true;
    healing = true;
    var cleaned = stripMonParam(h) || workloadsHrefPlain();
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
    if (healStickyMonParam()) return;
    // While pinning after a nav click, keep overlay up even if Angular briefly strips view=.
    if (Date.now() < pinUntil && isExactWorkloadsPath(location.hash || "")) {
      if (!hasMonView(location.hash || "")) {
        var pinned = monHref();
        if (location.hash !== pinned) {
          location.replace(location.pathname + location.search + pinned);
        }
      }
      clearLeaveGuard();
      show();
      return;
    }
    // Leave-guard: Angular reattached view=monitoring on workloads — strip.
    if (leaveGuardUntil > Date.now() && onMonRoute()) {
      var cleaned = stripMonParam(location.hash || "") || workloadsHrefPlain();
      if (cleaned && cleaned !== location.hash) {
        hide();
        location.replace(location.pathname + location.search + cleaned);
        return;
      }
    }
    if (onMonRoute()) {
      clearLeaveGuard();
      show();
    } else {
      hide();
    }
  }

  function leaveTo(href) {
    hide();
    var next = stripMonParam(href) || href;
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
        if (a.closest && a.closest("#nav-monitoring")) {
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          enter();
          return;
        }
        // Sibling overlay tile — do not intercept.
        if (a.closest && a.closest("#nav-virtualization")) {
          clearLeaveGuard();
          return;
        }

        var href = a.getAttribute("href") || "";
        var viaWorkloadsNav = !!(a.closest && a.closest("#nav-workloads"));
        var leaving = onMonRoute() || !!document.querySelector(".lg-mon-view");

        // In-overlay Addons link
        if (a.classList && a.classList.contains("lg-mon-link")) {
          if (!leaving) return;
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          hide();
          location.hash = href.charAt(0) === "#" ? href : "#/settings?view=addons";
          return;
        }

        if (href.charAt(0) !== "#") return;

        var hrefIsMon = isExactWorkloadsPath(href) && hasMonView(href);
        if (hrefIsMon && !viaWorkloadsNav) return;

        if (!(leaving || hasMonView(href) || hasMonView(location.hash))) return;

        // Leaving Monitoring — intercept before Angular preserve sticks the param.
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
          next = stripMonParam(href) || href;
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
      window.__lgMonHref = monHref;
      window.__lgEnterMon = enter;
      window.__lgHideMon = hide;
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
