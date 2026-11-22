/**
 * In-console Ceph view — embeds Ceph dashboard UI.
 *
 * Route: #/workloads?view=ceph (exact workloads overview only)
 * Nav tile still sits before CRDs; hash is independent so CRD/Settings/etc. leave cleanly.
 *
 * Leave hardening (same class of bugs as Addons):
 * - Angular queryParamsHandling=preserve reattaches view=ceph
 * - Overlay must hide + strip param before Angular navigates
 * - show() must not remount the iframe on every sync tick
 */
(function () {
  "use strict";

  var VIEW_ID = "lg-ceph-view";
  // Land on the authenticated dashboard (skip login route).
  var FRAME_SRC = "/kubeops/ceph-ui/#/dashboard";
  var CEPH_SESSION_API = "/kubeops/api/ceph-session";
  var OPS_API = "/kubeops/api/operators/kubeops-ceph";
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

  function hasCephView(hash) {
    return /(?:[?&])view=ceph(?:&|$)/.test(hash || "");
  }

  function onCephRoute() {
    var h = location.hash || "";
    return isExactWorkloadsPath(h) && hasCephView(h);
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

  function cephHref() {
    return (
      "#/workloads?namespace=" +
      encodeURIComponent(namespaceFromHash()) +
      "&view=ceph"
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

  function armCephLeaveGuard() {
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
      if (!hasCephView(h)) return;
      if (!isExactWorkloadsPath(h)) {
        // Stuck on a non-workloads route with view= — strip.
        var cleaned = stripCephParam(h) || workloadsHrefPlain();
        if (cleaned !== h) {
          location.replace(location.pathname + location.search + cleaned);
        }
        return;
      }
      // Angular reattached view= on plain workloads after leave — strip.
      if (onCephRoute()) {
        var plain = stripCephParam(h) || workloadsHrefPlain();
        if (plain !== h) {
          hide();
          location.replace(location.pathname + location.search + plain);
        }
      }
    }, 50);
  }

  function clearCephLeaveGuard() {
    leaveGuardUntil = 0;
    if (leaveGuardTimer) {
      clearInterval(leaveGuardTimer);
      leaveGuardTimer = null;
    }
  }

  function ensureView(host) {
    var orphans = document.querySelectorAll(".lg-ceph-view");
    for (var i = 0; i < orphans.length; i++) {
      if (orphans[i].parentElement !== host) orphans[i].remove();
    }
    var view = host.querySelector("#" + VIEW_ID);
    if (view) return view;

    view = document.createElement("div");
    view.id = VIEW_ID;
    view.className = "lg-ceph-view";
    view.innerHTML =
      '<div class="lg-ceph-shell">' +
      '  <div class="lg-ceph-panel">' +
      '    <div class="lg-ceph-loading" data-ceph-root>Loading Ceph UI…</div>' +
      "  </div>" +
      "</div>";
    host.appendChild(view);
    loadedOnce = false;
    return view;
  }

  function renderUnavailable(root, msg) {
    loadedOnce = false;
    root.className = "lg-ceph-loading lg-ceph-error";
    root.innerHTML =
      "<p><strong>Ceph UI is not available.</strong></p>" +
      "<p>" +
      String(msg || "Deploy kubeops-ceph from Addons first.") +
      "</p>" +
      '<p><a class="lg-ceph-link" href="#/settings?view=addons">Open Addons</a></p>';
  }

  function guardIframe(frame) {
    if (!frame || frame.getAttribute("data-lg-ceph-guarded") === "1") return;
    frame.setAttribute("data-lg-ceph-guarded", "1");

    function prefixOk(pathname) {
      return String(pathname || "").indexOf("/kubeops/ceph-ui") === 0;
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
        // CSS-only: hide force-login chrome. Do NOT MutationObserver/removeChild —
        // Ceph top-level nav toggles are <a href="" role="button"> and a DOM walker
        // that touches them (or stopPropagation on empty href) kills the menu.
        if (doc.documentElement.getAttribute("data-lg-ceph-signin-hide") !== "1") {
          doc.documentElement.setAttribute("data-lg-ceph-signin-hide", "1");
          var st = doc.createElement("style");
          st.textContent =
            'a[href*="forceLogin"]{display:none!important;visibility:hidden!important;pointer-events:none!important}';
          (doc.head || doc.documentElement).appendChild(st);
        }
        if (doc.documentElement.getAttribute("data-lg-ceph-click") === "1") {
          return;
        }
        doc.documentElement.setAttribute("data-lg-ceph-click", "1");
        doc.addEventListener(
          "click",
          function (evt) {
            var a = evt.target && evt.target.closest && evt.target.closest("a");
            if (!a) return;
            var href = a.getAttribute("href");
            // Ceph dropdown toggles use href="" / href="#" / role=button — never
            // intercept those or Angular/ngbCollapse never sees the click.
            if (href == null || href === "" || href === "#") return;
            // Hash routes stay inside the SPA.
            if (href.charAt(0) === "#") return;
            // Absolute site-root (brand) would load the kube console inside the iframe.
            if (href === "/") {
              evt.preventDefault();
              evt.stopPropagation();
              try {
                frame.contentWindow.location.href = FRAME_SRC;
              } catch (e) {
                /* ignore */
              }
              return;
            }
            // Same-origin absolute URL that left /kubeops/ceph-ui/.
            try {
              if (/^https?:\/\//i.test(href) || href.charAt(0) === "/") {
                var u = new URL(href, frame.contentWindow.location.href);
                if (
                  u.origin === frame.contentWindow.location.origin &&
                  u.pathname.indexOf("/kubeops/ceph-ui") !== 0
                ) {
                  evt.preventDefault();
                  evt.stopPropagation();
                  frame.contentWindow.location.href = FRAME_SRC;
                  return;
                }
              }
            } catch (e2) {
              /* ignore */
            }
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

    // Reclaim only if the iframe pathname escaped the Ceph prefix (rare).
    var guardTimer = setInterval(function () {
      if (!document.body.contains(frame) || !onCephRoute()) {
        clearInterval(guardTimer);
        return;
      }
      reclaimIfEscaped();
    }, 1500);
  }

  // Ceph AuthStorageService.set() wraps API arrays via Permissions/Permission
  // classes before JSON.stringify. Storing the raw API shape
  // ({"osd":["read",...]}) makes .osd.read undefined and blanks the SPA.
  function normalizeCephPermissions(raw) {
    raw = raw || {};
    function perm(arr) {
      arr = Array.isArray(arr) ? arr : [];
      return {
        read: arr.indexOf("read") >= 0,
        create: arr.indexOf("create") >= 0,
        update: arr.indexOf("update") >= 0,
        delete: arr.indexOf("delete") >= 0,
      };
    }
    return {
      hosts: perm(raw.hosts),
      configOpt: perm(raw["config-opt"]),
      pool: perm(raw.pool),
      osd: perm(raw.osd),
      monitor: perm(raw.monitor),
      rbdImage: perm(raw["rbd-image"]),
      iscsi: perm(raw.iscsi),
      nvmeof: perm(raw["nvme-of"]),
      rbdMirroring: perm(raw["rbd-mirroring"]),
      rgw: perm(raw.rgw),
      cephfs: perm(raw.cephfs),
      manager: perm(raw.manager),
      log: perm(raw.log),
      user: perm(raw.user),
      grafana: perm(raw.grafana),
      prometheus: perm(raw.prometheus),
      nfs: perm(raw["nfs-ganesha"]),
    };
  }

  function seedCephSession(data) {
    try {
      if (!data || !data.username) return;
      localStorage.setItem("dashboard_username", data.username);
      localStorage.setItem(
        "dashboard_permissions",
        JSON.stringify(normalizeCephPermissions(data.permissions))
      );
      localStorage.setItem("sso", String(!!data.sso));
      if (data.pwdExpirationDate != null) {
        localStorage.setItem("user_pwd_expiration_date", String(data.pwdExpirationDate));
      } else {
        localStorage.removeItem("user_pwd_expiration_date");
      }
      localStorage.setItem(
        "user_pwd_update_required",
        String(!!data.pwdUpdateRequired)
      );
    } catch (e) {
      /* ignore quota / private mode */
    }
  }

  function ensureCephSession() {
    return fetch(CEPH_SESSION_API + "?namespace=kubeops-ceph-cluster", {
      credentials: "same-origin",
      headers: { Accept: "application/json" },
    }).then(function (res) {
      if (!res.ok) throw new Error("session HTTP " + res.status);
      return res.json();
    }).then(function (data) {
      seedCephSession(data);
      return data;
    });
  }

  function renderFrame(root) {
    root.className = "lg-ceph-frame-wrap";
    root.innerHTML =
      '<iframe class="lg-ceph-frame" title="Ceph dashboard" src="' +
      FRAME_SRC +
      '" referrerpolicy="same-origin" allow="fullscreen"></iframe>';
    loadedOnce = true;
    var frame = root.querySelector(".lg-ceph-frame");
    guardIframe(frame);
  }

  function load(view, force) {
    var root = view.querySelector("[data-ceph-root]");
    if (!root) return;
    if (loadedOnce && !force && root.querySelector(".lg-ceph-frame")) return;

    root.className = "lg-ceph-loading";
    root.textContent = "Loading Ceph UI…";

    fetch(OPS_API, { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (res) {
        if (res.status === 404) return null;
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.json();
      })
      .then(function (op) {
        if (!onCephRoute()) return;
        var phase = op && op.status && String(op.status.phase || "").toLowerCase();
        if (op && phase && phase !== "ready" && phase !== "progressing") {
          renderUnavailable(
            root,
            (op.status && op.status.message) ||
              "kubeops-ceph is not Ready. Deploy from Addons."
          );
          return;
        }
        return fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" }).then(function (r) {
          if (!onCephRoute()) return;
          if (r.ok || r.status === 304) {
            return ensureCephSession().then(function () { renderFrame(root); });
          }
          else
            renderUnavailable(
              root,
              "UI proxy returned HTTP " +
                r.status +
                ". Ensure Ceph dashboard is running in kubeops-ceph-cluster."
            );
        });
      })
      .catch(function (err) {
        if (!onCephRoute()) return;
        fetch(FRAME_SRC, { credentials: "same-origin", method: "GET" })
          .then(function (r) {
            if (!onCephRoute()) return;
            if (r.ok || r.status === 304) {
            return ensureCephSession().then(function () { renderFrame(root); });
          }
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
            if (!onCephRoute()) return;
            renderUnavailable(root, (err && err.message) || "Could not reach Ceph UI.");
          });
      });
  }

  function setBreadcrumbs(active) {
    document.documentElement.classList.toggle("lg-ceph-route", !!active);
    document.body.classList.toggle("lg-ceph-route", !!active);
    var host =
      document.querySelector(".kd-breadcrumbs-container") ||
      document.querySelector("kd-breadcrumbs .kd-breadcrumbs-container");
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-ceph-breadcrumbs");
      if (custom) custom.remove();
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-ceph-breadcrumbs")) continue;
        kids[i].style.removeProperty("display");
      }
      return;
    }
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-ceph-breadcrumbs")) continue;
      kids2[j].style.setProperty("display", "none", "important");
    }
    var bar = host.querySelector(".lg-ceph-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-ceph-breadcrumbs kd-inline-breadcrumb";
      host.appendChild(bar);
    }
    bar.innerHTML = '<span class="kd-current-breadcrumb">Ceph Cluster</span>';
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
      if (!hasCephView(h)) {
        var next = cephHref();
        if (h !== next) {
          location.replace(location.pathname + location.search + next);
        }
      }
      if (!document.getElementById(VIEW_ID) || !document.documentElement.classList.contains("lg-ceph-route")) {
        show();
      }
    }, 40);
  }

  /** One-click entry: set hash, show overlay immediately, pin against Angular stripping view=. */
  function enter() {
    clearCephLeaveGuard();
    try {
      if (typeof window.__lgHideVirt === "function") window.__lgHideVirt();
    } catch (e) {
      /* ignore */
    }
    var next = cephHref();
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
    var active = onCephRoute();
    var link = document.querySelector("#nav-ceph a.kd-nav-item-button");
    if (link) {
      link.href = cephHref();
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
        if (isExactWorkloadsPath(location.hash) && !hasCephView(location.hash)) {
          wl.classList.add("kd-nav-item-button-active");
        }
      }
    }
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-ceph-active");
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
    clearCephLeaveGuard();
    var hosts = document.querySelectorAll(
      ".kd-scroll-container.lg-ceph-active, .mat-drawer-content.lg-ceph-active, .lg-ceph-active"
    );
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-ceph-active");
    }
    // Belt-and-suspenders: never leave the host stuck hiding Angular content.
    var allScroll = document.querySelectorAll(".kd-scroll-container");
    for (var s = 0; s < allScroll.length; s++) {
      allScroll[s].classList.remove("lg-ceph-active");
    }
    var views = document.querySelectorAll(".lg-ceph-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    setBreadcrumbs(false);
    syncNavActive();
  }

  function stripCephParam(hash) {
    if (!hash) return hash;
    return hash
      .replace(/([?&])view=ceph(&|$)/g, function (_, sep, end) {
        if (sep === "?" && end === "&") return "?";
        if (sep === "?" && end === "") return "";
        if (sep === "&" && end === "") return "";
        return end === "&" ? sep : "";
      })
      .replace(/\?&/, "?")
      .replace(/[?&]$/, "");
  }

  function healStickyCephParam() {
    var h = location.hash || "";
    if (!hasCephView(h)) return false;
    if (isExactWorkloadsPath(h)) return false;
    if (healing) return true;
    healing = true;
    var cleaned = stripCephParam(h) || workloadsHrefPlain();
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
    if (healStickyCephParam()) return;
    // While pinning after a nav click, keep overlay up even if Angular briefly strips view=.
    if (Date.now() < pinUntil && isExactWorkloadsPath(location.hash || "")) {
      if (!hasCephView(location.hash || "")) {
        var pinned = cephHref();
        if (location.hash !== pinned) {
          location.replace(location.pathname + location.search + pinned);
        }
      }
      clearCephLeaveGuard();
      show();
      return;
    }
    // Leave-guard: Angular reattached view=ceph on workloads — strip.
    if (leaveGuardUntil > Date.now() && onCephRoute()) {
      var cleaned = stripCephParam(location.hash || "") || workloadsHrefPlain();
      if (cleaned && cleaned !== location.hash) {
        hide();
        location.replace(location.pathname + location.search + cleaned);
        return;
      }
    }
    if (onCephRoute()) {
      clearCephLeaveGuard();
      show();
    } else {
      hide();
    }
  }

  function leaveTo(href) {
    hide();
    var next = stripCephParam(href) || href;
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
        if (a.closest && a.closest("#nav-ceph")) {
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          enter();
          return;
        }
        // Sibling overlay tile — do not intercept.
        if (a.closest && a.closest("#nav-virtualization")) {
          clearCephLeaveGuard();
          return;
        }

        var href = a.getAttribute("href") || "";
        var viaWorkloadsNav = !!(a.closest && a.closest("#nav-workloads"));
        var leaving = onCephRoute() || !!document.querySelector(".lg-ceph-view");

        // In-overlay Addons link
        if (a.classList && a.classList.contains("lg-ceph-link")) {
          if (!leaving) return;
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          hide();
          location.hash = href.charAt(0) === "#" ? href : "#/settings?view=addons";
          return;
        }

        if (href.charAt(0) !== "#") return;

        var hrefIsCeph = isExactWorkloadsPath(href) && hasCephView(href);
        if (hrefIsCeph && !viaWorkloadsNav) return;

        if (!(leaving || hasCephView(href) || hasCephView(location.hash))) return;

        // Leaving Ceph — intercept before Angular preserve sticks the param.
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
          next = stripCephParam(href) || href;
        }
        if (!next || next === "#") next = workloadsHrefPlain();
        if (next.charAt(0) !== "#") next = "#" + next;
        location.hash = next;

        if (viaWorkloadsNav || isExactWorkloadsPath(next)) {
          armCephLeaveGuard();
        }
      },
      true
    );
  }

  function boot() {
    bind();
    // Expose href helper for nav-accordion (optional).
    try {
      window.__lgCephHref = cephHref;
      window.__lgEnterCeph = enter;
      window.__lgHideCeph = hide;
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
