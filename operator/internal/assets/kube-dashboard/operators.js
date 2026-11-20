/**
 * In-console Operators view — embeds into the main dashboard shell.
 * Active only on: #/cluster?...&view=operators
 * Leaves cleanly when navigating anywhere else.
 */
(function () {
  "use strict";

  var API = "/kubeops/api/operators";
  var VIEW_ID = "lg-operators-view";
  var timer = null;
  var bound = false;
  var lastHash = "";

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function onOperatorsRoute() {
    var h = location.hash || "";
    // Must be the Cluster route AND the operators view flag.
    // (Angular preserves query params across nav, so view=operators alone is not enough.)
    if (h.indexOf("#/cluster") !== 0) return false;
    return /(?:[?&])view=operators(?:&|$)/.test(h);
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
    if (p === "error" || p === "failed" || p === "degraded") return "is-error";
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
          '<td class="lg-ops-url">' +
          (url
            ? '<a href="' + esc(url) + '" target="_blank" rel="noopener">' + esc(url) + "</a>"
            : "—") +
          "</td>" +
          '<td class="lg-ops-msg">' +
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
        render(root, (data && data.items) || []);
      })
      .catch(function (err) {
        root.className = "lg-ops-error";
        root.textContent =
          "Could not load operators (" +
          (err && err.message ? err.message : "error") +
          ").";
      });
  }

  function syncNavActive() {
    var link = document.querySelector("#nav-operators a.kd-nav-item-button");
    if (!link) return;
    link.classList.toggle("kd-nav-item-button-active", onOperatorsRoute());
  }

  function setBreadcrumbs(active) {
    var host = document.querySelector("kd-breadcrumbs .kd-breadcrumbs-container, .kd-breadcrumbs-container");
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-ops-breadcrumbs");
      if (custom) custom.remove();
      // Unhide native crumbs if we hid them.
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-ops-breadcrumbs")) continue;
        kids[i].style.display = "";
      }
      return;
    }
    // Hide native Angular crumbs and show Cluster > Operators.
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-ops-breadcrumbs")) continue;
      kids2[j].style.display = "none";
    }
    var bar = host.querySelector(".lg-ops-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-ops-breadcrumbs kd-inline-breadcrumb";
      bar.innerHTML =
        '<a class="kd-faded-breadcrumb" href="#/cluster">Cluster</a>' +
        '<i class="material-icons kd-faded-breadcrumb kd-breadcrumb-divider" aria-hidden="true">keyboard_arrow_right</i>' +
        '<span class="kd-current-breadcrumb">Operators</span>';
      host.appendChild(bar);
    }
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-operators-active");
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
    // Aggressively clear every overlay / lock class so other pages work again.
    var hosts = document.querySelectorAll(".kd-scroll-container.lg-operators-active, .lg-operators-active");
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-operators-active");
    }
    var views = document.querySelectorAll(".lg-operators-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      // Remove from DOM so it cannot block clicks/layout.
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
    setBreadcrumbs(false);
    syncNavActive();
  }

  function sync() {
    if (onOperatorsRoute()) show();
    else hide();
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

  function pollHash() {
    if (location.hash === lastHash) return;
    lastHash = location.hash;
    sync();
  }

  function bind() {
    if (bound) return;
    bound = true;
    window.addEventListener("hashchange", sync);
    setInterval(pollHash, 200);

    // When leaving via sidebar, drop the sticky view=operators query immediately.
    document.addEventListener(
      "click",
      function (evt) {
        var a = evt.target && evt.target.closest && evt.target.closest("a");
        if (!a) return;
        if (a.closest && a.closest("#nav-operators")) return;
        var href = a.getAttribute("href") || "";
        if (href.charAt(0) !== "#") return;
        if (!/(?:[?&])view=operators(?:&|$)/.test(href) && !onOperatorsRoute()) {
          // Navigating elsewhere while overlay is up — force hide now.
          if (document.querySelector(".lg-operators-view")) hide();
          return;
        }
        if (/(?:[?&])view=operators(?:&|$)/.test(href) && href.indexOf("#/cluster") !== 0) {
          // Other routes should never carry view=operators.
          evt.preventDefault();
          location.hash = stripOperatorsParam(href) || "#/workloads";
          hide();
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
