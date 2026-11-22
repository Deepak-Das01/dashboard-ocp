/**
 * In-console Operators view — embeds into the main dashboard shell.
 * Active only on exact: #/cluster?...&view=operators
 * (Not #/clusterrolebinding etc. — those share a "#/cluster" prefix.)
 * Leaves cleanly when navigating anywhere else.
 */
(function () {
  "use strict";

  var API = "/kubeops/api/operators";
  var VIEW_ID = "lg-operators-view";
  var timer = null;
  var bound = false;
  var lastHash = "";
  var healing = false;

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  /** Exact Cluster overview path only — not clusterrole / clusterrolebinding. */
  function isExactClusterPath(hash) {
    return /^#\/cluster(?:\?|$)/.test(hash || "");
  }

  function hasOperatorsView(hash) {
    return /(?:[?&])view=operators(?:&|$)/.test(hash || "");
  }

  function onOperatorsRoute() {
    var h = location.hash || "";
    return isExactClusterPath(h) && hasOperatorsView(h);
  }

  function contentHost() {
    var hosts = document.querySelectorAll(".kd-scroll-container");
    if (hosts.length) return hosts[hosts.length - 1];
    return (
      document.querySelector("mat-drawer-content.mat-drawer-content") ||
      document.querySelector(".mat-drawer-content")
    );
  }

  function phaseClass(phase) {
    var p = String(phase || "").toLowerCase();
    if (p === "ready" || p === "running") return "is-ready";
    if (p === "error" || p === "failed" || p === "degraded" || p === "unavailable") return "is-error";
    return "is-pending";
  }

  function age(ts) {
    if (!ts) return "—";
    var t = Date.parse(ts);
    if (!t) return "—";
    var sec = Math.max(0, Math.floor((Date.now() - t) / 1000));
    if (sec < 60) return sec + "s";
    if (sec < 3600) return Math.floor(sec / 60) + "m";
    if (sec < 86400) return Math.floor(sec / 3600) + "h";
    return Math.floor(sec / 86400) + "d";
  }

  function ensureView(host) {
    var orphans = document.querySelectorAll(".lg-operators-view");
    for (var i = 0; i < orphans.length; i++) {
      if (orphans[i].parentElement !== host) orphans[i].remove();
    }
    var view = host.querySelector("#" + VIEW_ID);
    if (view) return view;

    view = document.createElement("div");
    view.id = VIEW_ID;
    view.className = "lg-operators-view";
    view.innerHTML =
      '<div class="lg-ops-shell">' +
      '  <div class="lg-ops-panel mat-card">' +
      '    <div class="lg-ops-top kd-card-header">' +
      '      <div class="lg-ops-title kd-card-title">Operators</div>' +
      '      <div class="lg-ops-actions kd-card-actions">' +
      '        <button type="button" class="lg-ops-btn" data-ops-refresh title="Refresh">' +
      '          <span class="material-icons" aria-hidden="true">refresh</span>' +
      "        </button>" +
      "      </div>" +
      "    </div>" +
      '    <div class="lg-ops-loading" data-ops-root>Loading operators…</div>' +
      "  </div>" +
      "</div>";

    host.appendChild(view);
    var btn = view.querySelector("[data-ops-refresh]");
    if (btn) {
      btn.addEventListener("click", function () {
        load(view);
      });
    }
    return view;
  }

  function render(root, items) {
    if (!items.length) {
      root.className = "lg-ops-empty";
      root.textContent = "No Operator resources found.";
      return;
    }

    var rows = items
      .slice()
      .sort(function (a, b) {
        return String(a.metadata.name).localeCompare(String(b.metadata.name));
      })
      .map(function (op) {
        var spec = op.spec || {};
        var status = op.status || {};
        var phase = status.phase || "Unknown";
        var url = status.url || "";
        var msg = status.message || "";
        var version = spec.version || "—";
        var component = spec.component || "—";
        var name = op.metadata.name;
        var detailHref =
          "#/customresourcedefinition/operators.dashboard.ocp/" + encodeURIComponent(name);

        return (
          "<tr>" +
          '<td><div class="lg-ops-name"><a href="' +
          esc(detailHref) +
          '">' +
          esc(name) +
          "</a></div>" +
          '<div class="lg-ops-meta">age ' +
          esc(age(op.metadata.creationTimestamp)) +
          "</div></td>" +
          "<td>" +
          esc(component) +
          "</td>" +
          "<td>" +
          esc(version) +
          "</td>" +
          '<td><span class="lg-ops-phase ' +
          phaseClass(phase) +
          '">' +
          esc(phase) +
          "</span></td>" +
          '<td class="lg-ops-url" title="' +
          esc(url || "") +
          '">' +
          (url
            ? '<a href="' + esc(url) + '" target="_blank" rel="noopener" title="' + esc(url) + '">' + esc(url) + "</a>"
            : "—") +
          "</td>" +
          '<td class="lg-ops-msg" title="' +
          esc(msg || "") +
          '">' +
          esc(msg || "—") +
          "</td>" +
          "</tr>"
        );
      })
      .join("");

    root.className = "lg-ops-table-wrap";
    root.innerHTML =
      '<table class="lg-ops-table">' +
      "<thead><tr>" +
      "<th>Name</th><th>Component</th><th>Version</th><th>Phase</th><th>URL</th><th>Message</th>" +
      "</tr></thead><tbody>" +
      rows +
      "</tbody></table>";
  }

  function load(view) {
    var root = view.querySelector("[data-ops-root]");
    if (!root) return;
    root.className = "lg-ops-loading";
    root.textContent = "Loading operators…";
    fetch(API, { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (res) {
        if (!res.ok) throw new Error("HTTP " + res.status);
        return res.json();
      })
      .then(function (data) {
        if (!onOperatorsRoute()) return;
        render(root, (data && data.items) || []);
      })
      .catch(function (err) {
        if (!onOperatorsRoute()) return;
        root.className = "lg-ops-error";
        root.textContent =
          "Could not load operators (" +
          (err && err.message ? err.message : "error") +
          ").";
      });
  }

  /** Suppress Angular's Cluster-group active while Operators child is selected. */
  function syncClusterGroupActive(opsActive) {
    var clusterLink = document.querySelector("#nav-cluster > a.kd-nav-item-button");
    if (!clusterLink) return;
    if (opsActive) {
      if (clusterLink.classList.contains("kd-nav-item-button-active")) {
        clusterLink.classList.remove("kd-nav-item-button-active");
        clusterLink.setAttribute("data-lg-ops-suppressed-active", "1");
      }
    } else if (clusterLink.getAttribute("data-lg-ops-suppressed-active") === "1") {
      clusterLink.removeAttribute("data-lg-ops-suppressed-active");
      // Let Angular re-apply active on next digest if still on #/cluster overview.
      if (isExactClusterPath(location.hash) && !hasOperatorsView(location.hash)) {
        clusterLink.classList.add("kd-nav-item-button-active");
      }
    }
  }

  function syncNavActive() {
    var opsActive = onOperatorsRoute();
    var link = document.querySelector("#nav-operators a.kd-nav-item-button");
    if (link) link.classList.toggle("kd-nav-item-button-active", opsActive);
    syncClusterGroupActive(opsActive);
  }

  function setRouteClass(active) {
    document.documentElement.classList.toggle("lg-ops-route", active);
    if (document.body) document.body.classList.toggle("lg-ops-route", active);
  }

  function setBreadcrumbs(active) {
    var host = document.querySelector("kd-breadcrumbs .kd-breadcrumbs-container, .kd-breadcrumbs-container");
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-ops-breadcrumbs");
      if (custom) custom.remove();
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-ops-breadcrumbs")) continue;
        kids[i].style.display = "";
      }
      return;
    }
    // Hide Angular's native "Cluster" crumb (CSS also enforces via .lg-ops-route).
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-ops-breadcrumbs")) continue;
      kids2[j].style.display = "none";
    }
    var bar = host.querySelector(".lg-ops-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-ops-breadcrumbs kd-inline-breadcrumb";
      host.appendChild(bar);
    }
    // Parent > child. Angular's native Cluster crumb is hidden via .lg-ops-route CSS
    // so this does not become "ClusterCluster > Operators".
    bar.innerHTML =
      '<a class="kd-faded-breadcrumb" href="#/cluster">Cluster</a>' +
      '<i class="material-icons kd-faded-breadcrumb kd-breadcrumb-divider" aria-hidden="true">keyboard_arrow_right</i>' +
      '<span class="kd-current-breadcrumb">Operators</span>';
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-operators-active");
    setRouteClass(true);
    view.hidden = false;
    view.style.display = "";
    load(view);
    setBreadcrumbs(true);
    if (timer) clearInterval(timer);
    timer = setInterval(function () {
      if (onOperatorsRoute()) load(view);
      else hide();
    }, 15000);
    syncNavActive();
    return true;
  }

  function hide() {
    var hosts = document.querySelectorAll(".kd-scroll-container.lg-operators-active, .lg-operators-active");
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-operators-active");
    }
    var views = document.querySelectorAll(".lg-operators-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
    setRouteClass(false);
    setBreadcrumbs(false);
    syncNavActive();
  }

  function stripOperatorsParam(hash) {
    if (!hash) return hash;
    var cleaned = hash
      .replace(/([?&])view=operators(&|$)/g, function (_, sep, end) {
        if (sep === "?" && end === "&") return "?";
        if (sep === "?" && end === "") return "";
        if (sep === "&" && end === "") return "";
        return end === "&" ? sep : "";
      })
      .replace(/\?&/, "?")
      .replace(/[?&]$/, "");
    return cleaned;
  }

  /** If view=operators leaked onto a non-Operators route, strip it and hide. */
  function healStickyOperatorsParam() {
    var h = location.hash || "";
    if (!hasOperatorsView(h)) return false;
    if (isExactClusterPath(h)) return false;
    if (healing) return true;
    healing = true;
    var cleaned = stripOperatorsParam(h) || "#/workloads";
    hide();
    if (cleaned !== h) {
      lastHash = cleaned;
      location.replace(location.pathname + location.search + cleaned);
    } else {
      hide();
    }
    setTimeout(function () {
      healing = false;
    }, 0);
    return true;
  }

  function sync() {
    if (healStickyOperatorsParam()) return;
    if (onOperatorsRoute()) show();
    else hide();
  }

  function bind() {
    if (bound) return;
    bound = true;
    window.addEventListener("hashchange", sync);

    // Leave Operators: hide immediately and strip sticky view=operators so Angular
    // queryParamsHandling=preserve cannot bounce us back.
    document.addEventListener(
      "click",
      function (evt) {
        var a = evt.target && evt.target.closest && evt.target.closest("a");
        if (!a) return;
        if (a.closest && a.closest("#nav-operators")) return;
        var href = a.getAttribute("href") || "";
        if (href.charAt(0) !== "#") return;

        var leavingOps = onOperatorsRoute() || !!document.querySelector(".lg-operators-view");
        var hrefIsOps = isExactClusterPath(href) && hasOperatorsView(href);

        if (hrefIsOps) return; // staying on / entering Operators

        if (leavingOps || hasOperatorsView(href) || hasOperatorsView(location.hash)) {
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") {
            evt.stopImmediatePropagation();
          }
          hide();
          var next = stripOperatorsParam(href) || href;
          if (!next || next === "#") next = "#/workloads";
          if (next.charAt(0) !== "#") next = "#" + next;
          lastHash = next;
          location.hash = next;
        }
      },
      true
    );
  }

  function boot() {
    bind();
    var tries = 0;
    var t = setInterval(function () {
      tries += 1;
      if (contentHost() || tries > 80) {
        clearInterval(t);
        lastHash = location.hash;
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
