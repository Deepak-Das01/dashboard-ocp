/**
 * In-console Addons catalog — kubeops-nfs install form.
 * Active only on exact: #/settings?...&view=addons
 *
 * Hardening notes:
 * - Polling must NOT wipe in-progress form edits.
 * - Native <form> submit / Enter must not navigate away.
 * - In-view links (StorageClass / Pods) must not be treated as "leave Addons" glitches
 *   unless navigating outside; leave handler ignores clicks inside the addons view for
 *   status links by allowing them after hide, and never re-renders over typed values.
 */
(function () {
  "use strict";

  var NFS_API = "/kubeops/api/nfsprovisioners";
  var VIRT_API = "/kubeops/api/virtualizations";
  var MON_API = "/kubeops/api/monitorings";
  var OPS_API = "/kubeops/api/operators";
  var VIEW_ID = "lg-addons-view";
  var DEFAULT_NS = "kubeops-nfs";
  var DEFAULT_SC = "kubeops-nfs-storage";
  var DEFAULT_RELEASE = "kubeops-nfs-provisioner";
  var DEFAULT_VIRT_RELEASE = "kubeops-virtualization";
  var DEFAULT_MON_RELEASE = "kubeops-monitoring";
  var DEFAULT_MON_NS = "kubeops-monitoring";
  var DEFAULT_KV_VERSION = "v1.10.0";
  var DEFAULT_CDI_VERSION = "v1.66.1";

  var timer = null;
  var bound = false;
  var healing = false;
  var leaveGuardTimer = null;
  var leaveGuardUntil = 0;
  var loading = false;
  var submitting = false;
  var panel = "catalog"; // catalog | form
  var formKind = "nfs"; // nfs | virt | mon
  var editingName = "";
  var draft = null; // preserved form values while on the form panel
  var lastStatusKey = "";
  var catalogCache = { nfs: null, virt: null, mon: null, nfsOp: false, virtOp: false, monOp: false };

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function isExactSettingsPath(hash) {
    return /^#\/settings(?:\?|$)/.test(hash || "");
  }

  function hasAddonsView(hash) {
    return /(?:[?&])view=addons(?:&|$)/.test(hash || "");
  }

  function onAddonsRoute() {
    var h = location.hash || "";
    return isExactSettingsPath(h) && hasAddonsView(h);
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
    if (p === "ready") return "is-ready";
    if (
      p === "degraded" ||
      p === "error" ||
      p === "failed" ||
      p === "unavailable" ||
      p === "operator missing"
    ) {
      return "is-error";
    }
    if (p === "not installed") return "is-idle";
    return "is-pending";
  }

  function addonStatus(cr, opReady, kind) {
    kind = kind || "nfs";
    if (!opReady) {
      return {
        label: "Unavailable",
        cls: phaseClass("unavailable"),
        msg:
          kind === "virt"
            ? "kubeops-virtualization-controller is not installed (ns kubeops-addon-controller)."
            : "kubeops-nfs-controller is not installed (ns kubeops-addon-controller).",
      };
    }
    if (!cr) {
      return {
        label: "Unavailable",
        cls: phaseClass("unavailable"),
        msg:
          kind === "virt"
            ? "Not installed — click Install to deploy KubeVirt + CDI."
            : "Not installed — click Install and Deploy with your NFS server details.",
      };
    }
    var phase = String((cr.status && cr.status.phase) || "Progressing");
    var msg = (cr.status && cr.status.message) || "";
    if (phase === "Ready") {
      return {
        label: "Ready",
        cls: phaseClass("ready"),
        msg:
          msg ||
          (kind === "virt"
            ? "KubeVirt + CDI installed successfully."
            : "NFS provisioner installed successfully."),
      };
    }
    if (phase === "Degraded") {
      return {
        label: "Degraded",
        cls: phaseClass("degraded"),
        msg:
          msg ||
          (kind === "virt"
            ? "Install failed — check virtualization controller logs."
            : "Install failed — check operator logs and NFS reachability."),
      };
    }
    return {
      label: "Progressing",
      cls: phaseClass("progressing"),
      msg:
        msg ||
        (kind === "virt" ? "Installing KubeVirt + CDI…" : "Installing NFS provisioner…"),
    };
  }

  function fetchJSON(url, opts) {
    opts = opts || {};
    var headers = Object.assign({ Accept: "application/json" }, opts.headers || {});
    return fetch(url, {
      credentials: "same-origin",
      method: opts.method || "GET",
      headers: headers,
      body: opts.body,
    }).then(function (res) {
      if (res.status === 404) return null;
      if (!res.ok) {
        return res.text().then(function (t) {
          var msg = t || "HTTP " + res.status;
          try {
            var j = JSON.parse(t);
            if (j && j.message) msg = j.message;
            else if (j && j.details && j.details.causes && j.details.causes[0]) {
              msg = j.details.causes[0].message || msg;
            }
          } catch (e) {
            /* keep raw */
          }
          throw new Error(String(msg).slice(0, 300));
        });
      }
      if (res.status === 204) return null;
      var ct = res.headers.get("content-type") || "";
      if (ct.indexOf("json") === -1) return res.text().then(function () {
        return null;
      });
      return res.json();
    });
  }

  function captureDraft() {
    if (formKind === "virt") {
      if (!document.getElementById("virt-release")) return;
    } else if (formKind === "mon") {
      if (!document.getElementById("mon-release")) return;
    } else if (!document.getElementById("nfs-server")) {
      return;
    }
    draft = readForm();
  }

  function ensureView(host) {
    var orphans = document.querySelectorAll(".lg-addons-view");
    for (var i = 0; i < orphans.length; i++) {
      if (orphans[i].parentElement !== host) orphans[i].remove();
    }
    var view = host.querySelector("#" + VIEW_ID);
    if (view) return view;

    view = document.createElement("div");
    view.id = VIEW_ID;
    view.className = "lg-addons-view";
    view.innerHTML =
      '<div class="lg-addons-shell">' +
      '  <div class="lg-addons-panel mat-card">' +
      '    <div class="lg-addons-top kd-card-header">' +
      '      <div class="lg-addons-title kd-card-title">Addons</div>' +
      '      <div class="lg-addons-actions kd-card-actions">' +
      '        <button type="button" class="lg-ops-btn" data-addons-refresh title="Refresh">' +
      '          <span class="material-icons" aria-hidden="true">refresh</span>' +
      "        </button>" +
      "      </div>" +
      "    </div>" +
      '    <div class="lg-addons-body" data-addons-root>Loading addons…</div>' +
      "  </div>" +
      "</div>";

    host.appendChild(view);
    var btn = view.querySelector("[data-addons-refresh]");
    if (btn) {
      btn.addEventListener("click", function () {
        load(view, true);
      });
    }
    view.addEventListener("click", function (evt) {
      var t = evt.target && evt.target.closest && evt.target.closest("[data-addons-action]");
      if (!t || t.disabled) return;
      evt.preventDefault();
      var action = t.getAttribute("data-addons-action");
      if (action === "install" || action === "manage") {
        captureDraft();
        panel = "form";
        formKind = t.getAttribute("data-kind") || "nfs";
        editingName = t.getAttribute("data-name") || "";
        if (action === "install") {
          if (formKind === "virt") {
            draft = {
              kubevirtVersion: DEFAULT_KV_VERSION,
              cdiVersion: DEFAULT_CDI_VERSION,
              useEmulation: false,
              release: DEFAULT_VIRT_RELEASE,
            };
          } else if (formKind === "mon") {
            draft = {
              namespace: DEFAULT_MON_NS,
              storageClassName: DEFAULT_SC,
              retention: "15d",
              grafanaAdminUser: "admin",
              enableAlertmanager: true,
              release: DEFAULT_MON_RELEASE,
            };
          } else {
            draft = {
              server: "",
              path: "",
              namespace: DEFAULT_NS,
              storageClassName: DEFAULT_SC,
              release: DEFAULT_RELEASE,
              defaultClass: false,
            };
          }
        }
        load(view, true);
      } else if (action === "back") {
        captureDraft();
        panel = "catalog";
        formKind = "nfs";
        editingName = "";
        draft = null;
        load(view, true);
      } else if (action === "deploy") {
        submitForm(view, false);
      } else if (action === "redeploy") {
        submitForm(view, true);
      } else if (action === "uninstall") {
        uninstall(view);
      }
    });
    view.addEventListener("submit", function (evt) {
      evt.preventDefault();
      evt.stopPropagation();
      if (submitting) return;
      var isEdit = !!view.querySelector('[data-addons-action="redeploy"]');
      submitForm(view, isEdit);
    });
    view.addEventListener("input", function () {
      captureDraft();
    });
    view.addEventListener("change", function () {
      captureDraft();
    });
    return view;
  }

  function tileHTML(kind, title, desc, cr, opReady) {
    var st = addonStatus(cr, opReady, kind);
    var cta = cr ? "Manage" : "Install";
    var action = cr ? "manage" : "install";
    var name = (cr && cr.metadata && cr.metadata.name) || "";
    return (
      '  <div class="lg-addon-tile">' +
      '    <div class="lg-addon-tile-head">' +
      '      <div class="lg-addon-tile-name">' +
      esc(title) +
      "</div>" +
      '      <span class="lg-ops-phase ' +
      esc(st.cls) +
      '">' +
      esc(st.label) +
      "</span>" +
      "    </div>" +
      '    <p class="lg-addon-tile-desc">' +
      esc(desc) +
      "</p>" +
      '<p class="lg-addon-tile-msg">' +
      esc(st.msg) +
      "</p>" +
      '    <div class="lg-addon-tile-cta">' +
      '      <button type="button" class="lg-addon-btn primary" data-addons-action="' +
      action +
      '" data-kind="' +
      esc(kind) +
      '" data-name="' +
      esc(name) +
      '">' +
      esc(cta) +
      "</button>" +
      "    </div>" +
      "  </div>"
    );
  }

  function renderCatalog(root, nfsCr, nfsOp, virtCr, virtOp, monCr, monOp) {
    lastStatusKey = "";
    root.className = "lg-addons-body";
    root.innerHTML =
      '<div class="lg-addons-catalog">' +
      tileHTML(
        "nfs",
        "kubeops-nfs",
        "NFS subdir external provisioner — creates a StorageClass from your NFS server and export path.",
        nfsCr,
        nfsOp
      ) +
      tileHTML(
        "virt",
        "kubeops-virtualization",
        "KubeVirt + CDI + kubevirt-manager UI — install the virtualization stack and open the VM console.",
        virtCr,
        virtOp
      ) +
      tileHTML(
        "mon",
        "kubeops-monitoring",
        "Prometheus + Grafana + Alertmanager + exporters — cluster metrics with curated dashboards.",
        monCr,
        monOp
      ) +
      "</div>";
  }

  function field(id, label, required, placeholder, value) {
    var req = required ? ' <span class="lg-addon-req">*</span>' : "";
    return (
      '<label class="lg-addon-field" for="' +
      id +
      '">' +
      '<span class="lg-addon-label">' +
      esc(label) +
      req +
      "</span>" +
      '<input class="lg-addon-input" id="' +
      id +
      '" name="' +
      id +
      '" type="text" value="' +
      esc(value || "") +
      '" placeholder="' +
      esc(placeholder || "") +
      '" autocomplete="off" spellcheck="false" />' +
      '<span class="lg-addon-error" id="' +
      id +
      '-err" hidden></span>' +
      "</label>"
    );
  }

  function statusHTML(st, status) {
    return (
      '<div class="lg-addon-status" data-addons-status>' +
      '<span class="lg-ops-phase ' +
      esc(st.cls) +
      '">' +
      esc(st.label) +
      "</span> " +
      '<span class="lg-addon-status-msg">' +
      esc(st.msg) +
      "</span>" +
      (status && status.storageClass
        ? ' <a class="lg-addon-status-link" href="#/storageclass/' +
          encodeURIComponent(status.storageClass) +
          '">StorageClass</a>'
        : "") +
      (status && status.namespace
        ? ' <a class="lg-addon-status-link" href="#/pod?namespace=' +
          encodeURIComponent(status.namespace) +
          '">Pods</a>'
        : "") +
      (status && status.kubevirtPhase
        ? ' <span class="lg-addon-status-msg">KubeVirt=' + esc(status.kubevirtPhase) + "</span>"
        : "") +
      (status && status.cdiPhase
        ? ' <span class="lg-addon-status-msg">CDI=' + esc(status.cdiPhase) + "</span>"
        : "") +
      (status && status.uiURL
        ? ' <a class="lg-addon-status-link" href="' +
          esc(status.uiURL) +
          '" target="_blank" rel="noopener">Open UI</a>'
        : "") +
      (status && status.grafanaURL
        ? ' <a class="lg-addon-status-link" href="' +
          esc(status.grafanaURL) +
          '" target="_blank" rel="noopener">Grafana</a>'
        : "") +
      "</div>"
    );
  }

  function currentNamespace() {
    try {
      var m = (location.hash || "").match(/[?&]namespace=([^&]*)/);
      if (m && m[1]) return decodeURIComponent(m[1]) || "default";
    } catch (e) {
      /* ignore */
    }
    return "default";
  }

  function settingsHrefPlain() {
    return "#/settings?namespace=" + encodeURIComponent(currentNamespace());
  }

  /**
   * Angular Settings links use queryParamsHandling=preserve, so while on Addons their
   * href becomes #/settings?view=addons&…. After we navigate to plain Settings, Angular
   * may reattach view=addons — strip it for a short window (same class of bug as Operators).
   */
  function armAddonsLeaveGuard() {
    leaveGuardUntil = Date.now() + 1500;
    if (leaveGuardTimer) clearInterval(leaveGuardTimer);
    leaveGuardTimer = setInterval(function () {
      if (Date.now() > leaveGuardUntil) {
        clearInterval(leaveGuardTimer);
        leaveGuardTimer = null;
        return;
      }
      var h = location.hash || "";
      if (!hasAddonsView(h) || !isExactSettingsPath(h)) return;
      var cleaned = stripAddonsParam(h);
      if (cleaned && cleaned !== h) {
        location.replace(location.pathname + location.search + cleaned);
      }
    }, 40);
  }

  function clearAddonsLeaveGuard() {
    leaveGuardUntil = 0;
    if (leaveGuardTimer) {
      clearInterval(leaveGuardTimer);
      leaveGuardTimer = null;
    }
  }

  function formValuesFrom(cr) {
    var spec = (cr && cr.spec) || {};
    var release = (cr && cr.metadata && cr.metadata.name) || editingName || "";
    if (formKind === "virt") {
      if (draft && draft.kubevirtVersion != null) {
        return {
          kubevirtVersion: draft.kubevirtVersion || DEFAULT_KV_VERSION,
          cdiVersion: draft.cdiVersion || DEFAULT_CDI_VERSION,
          useEmulation: !!draft.useEmulation,
          release: draft.release || release || DEFAULT_VIRT_RELEASE,
        };
      }
      return {
        kubevirtVersion: spec.kubevirtVersion || DEFAULT_KV_VERSION,
        cdiVersion: spec.cdiVersion || DEFAULT_CDI_VERSION,
        useEmulation: !!spec.useEmulation,
        release: release || DEFAULT_VIRT_RELEASE,
      };
    }
    if (formKind === "mon") {
      if (draft && draft.grafanaAdminUser != null) {
        return {
          namespace: draft.namespace || DEFAULT_MON_NS,
          storageClassName: draft.storageClassName != null ? draft.storageClassName : DEFAULT_SC,
          retention: draft.retention || "15d",
          grafanaAdminUser: draft.grafanaAdminUser || "admin",
          enableAlertmanager: draft.enableAlertmanager !== false,
          release: draft.release || release || DEFAULT_MON_RELEASE,
        };
      }
      return {
        namespace: spec.namespace || DEFAULT_MON_NS,
        storageClassName: spec.storageClassName != null ? spec.storageClassName : DEFAULT_SC,
        retention: spec.retention || "15d",
        grafanaAdminUser: spec.grafanaAdminUser || "admin",
        enableAlertmanager: spec.enableAlertmanager !== false,
        release: release || DEFAULT_MON_RELEASE,
      };
    }
    if (draft && draft.server != null) {
      return {
        server: draft.server,
        path: draft.path,
        namespace: draft.namespace || DEFAULT_NS,
        storageClassName: draft.storageClassName || DEFAULT_SC,
        release: draft.release || release || DEFAULT_RELEASE,
        defaultClass: !!draft.defaultClass,
      };
    }
    return {
      server: spec.server || "",
      path: spec.path || "",
      namespace: spec.namespace || DEFAULT_NS,
      storageClassName: spec.storageClassName || DEFAULT_SC,
      release: release || DEFAULT_RELEASE,
      defaultClass: !!spec.defaultClass,
    };
  }

  function formActionsHTML(isEdit, opReady) {
    return isEdit
      ? '<button type="submit" class="lg-addon-btn primary" data-addons-action="redeploy"' +
          (!opReady || submitting ? " disabled" : "") +
          ">Redeploy</button>" +
          '<button type="button" class="lg-addon-btn danger" data-addons-action="uninstall"' +
          (submitting ? " disabled" : "") +
          ">Uninstall</button>"
      : '<button type="submit" class="lg-addon-btn primary" data-addons-action="deploy"' +
          (!opReady || submitting ? " disabled" : "") +
          ">Deploy</button>";
  }

  function renderForm(root, cr, opReady) {
    var vals = formValuesFrom(cr);
    var status = (cr && cr.status) || {};
    var isEdit = !!cr;
    var st = addonStatus(cr, opReady, formKind);
    lastStatusKey =
      formKind +
      "|" +
      st.label +
      "|" +
      st.msg +
      "|" +
      (status.storageClass || "") +
      "|" +
      (status.kubevirtPhase || "") +
      "|" +
      (status.cdiPhase || "");

    var fields;
    var title;
    var callout;
    var missingCtrl;
    var releaseId;
    if (formKind === "virt") {
      title = "kubeops-virtualization";
      missingCtrl =
        "Install kubeops-virtualization-controller (shipped with install-operators.sh) before Deploy.";
      callout =
        "Installs pinned KubeVirt + CDI operators. First Deploy pulls many images and can take several minutes. Enable emulation on labs without /dev/kvm.";
      releaseId = "virt-release";
      fields =
        field("virt-kv", "KubeVirt version", true, DEFAULT_KV_VERSION, vals.kubevirtVersion) +
        field("virt-cdi", "CDI version", true, DEFAULT_CDI_VERSION, vals.cdiVersion) +
        '<label class="lg-addon-field lg-addon-check">' +
        '<input type="checkbox" id="virt-emulation" name="virt-emulation"' +
        (vals.useEmulation ? " checked" : "") +
        " />" +
        "<span>Use software emulation (no KVM)</span>" +
        "</label>" +
        field("virt-release", "Release name", false, DEFAULT_VIRT_RELEASE, vals.release);
    } else if (formKind === "mon") {
      title = "kubeops-monitoring";
      missingCtrl =
        "Install kubeops-monitoring-controller (shipped with install-operators.sh) before Deploy.";
      callout =
        "Installs Prometheus, Grafana, Alertmanager, kube-state-metrics, and node-exporter. Prefers StorageClass kubeops-nfs-storage; leave blank for ephemeral emptyDir.";
      releaseId = "mon-release";
      fields =
        field("mon-namespace", "Namespace", false, DEFAULT_MON_NS, vals.namespace) +
        field("mon-sc", "StorageClass name", false, DEFAULT_SC, vals.storageClassName) +
        field("mon-retention", "Retention", false, "15d", vals.retention) +
        field("mon-admin", "Grafana admin user", false, "admin", vals.grafanaAdminUser) +
        '<label class="lg-addon-field lg-addon-check">' +
        '<input type="checkbox" id="mon-alertmanager" name="mon-alertmanager"' +
        (vals.enableAlertmanager ? " checked" : "") +
        " />" +
        "<span>Enable Alertmanager</span>" +
        "</label>" +
        field("mon-release", "Release name", false, DEFAULT_MON_RELEASE, vals.release);
    } else {
      title = "kubeops-nfs";
      missingCtrl =
        "Install kubeops-nfs-controller (shipped with install-operators.sh in kubeops-addon-controller) before Deploy.";
      callout =
        "Prerequisites: an NFS export must already exist and be reachable from cluster nodes. The console cannot verify the mount.";
      releaseId = "nfs-release";
      fields =
        field("nfs-server", "NFS server", true, "NFS server IP or hostname", vals.server) +
        field("nfs-path", "NFS path", true, "/exported/path", vals.path) +
        field("nfs-namespace", "Namespace", false, DEFAULT_NS, vals.namespace) +
        field("nfs-sc", "StorageClass name", false, DEFAULT_SC, vals.storageClassName) +
        '<label class="lg-addon-field lg-addon-check">' +
        '<input type="checkbox" id="nfs-default" name="nfs-default"' +
        (vals.defaultClass ? " checked" : "") +
        " />" +
        "<span>Default StorageClass</span>" +
        "</label>" +
        field("nfs-release", "Release name", false, DEFAULT_RELEASE, vals.release);
    }

    root.className = "lg-addons-body";
    root.innerHTML =
      '<div class="lg-addon-form-wrap">' +
      '  <button type="button" class="lg-addon-back" data-addons-action="back">' +
      '    <span class="material-icons" aria-hidden="true">arrow_back</span> Back to Addons' +
      "  </button>" +
      '  <h2 class="lg-addon-form-title">' +
      esc(title) +
      "</h2>" +
      statusHTML(st, status) +
      (!opReady ? '<div class="lg-addon-callout is-error">' + esc(missingCtrl) + "</div>" : "") +
      '  <div class="lg-addon-callout">' +
      esc(callout) +
      "</div>" +
      '  <form class="lg-addon-form" data-addons-form novalidate>' +
      fields +
      (isEdit
        ? '<p class="lg-addon-hint">Release name cannot change after install; Redeploy updates the existing CR.</p>'
        : "") +
      '    <div class="lg-addon-form-actions">' +
      formActionsHTML(isEdit, opReady) +
      "    </div>" +
      '    <div class="lg-addon-form-msg" data-form-msg hidden></div>' +
      "  </form>" +
      "</div>";

    var releaseInput = root.querySelector("#" + releaseId);
    if (releaseInput && isEdit) {
      releaseInput.readOnly = true;
      releaseInput.setAttribute("aria-readonly", "true");
    }
    draft = readForm();
  }

  /** Update status strip only — keep typed field values intact. */
  function patchFormStatus(root, cr, opReady) {
    var st = addonStatus(cr, opReady, formKind);
    var status = (cr && cr.status) || {};
    var key =
      formKind +
      "|" +
      st.label +
      "|" +
      st.msg +
      "|" +
      (status.storageClass || "") +
      "|" +
      (status.kubevirtPhase || "") +
      "|" +
      (status.cdiPhase || "");
    if (key === lastStatusKey) return;
    lastStatusKey = key;
    var el = root.querySelector("[data-addons-status]");
    if (!el) return;
    el.outerHTML = statusHTML(st, status);

    var msgEl = root.querySelector("[data-form-msg]");
    if (msgEl && !msgEl.hidden && /waiting for operator|Deploying|Redeploying/i.test(msgEl.textContent || "")) {
      if (st.label === "Ready" || st.label === "Degraded" || st.label === "Progressing") {
        msgEl.hidden = true;
        msgEl.textContent = "";
      }
    }

    var actions = root.querySelector(".lg-addon-form-actions");
    if (!actions) return;
    var isEdit = !!cr;
    var hasRedeploy = !!root.querySelector('[data-addons-action="redeploy"]');
    if (isEdit === hasRedeploy) return;
    captureDraft();
    actions.innerHTML = formActionsHTML(isEdit, opReady);
    var releaseInput = root.querySelector(
      formKind === "virt" ? "#virt-release" : formKind === "mon" ? "#mon-release" : "#nfs-release"
    );
    if (releaseInput) {
      releaseInput.readOnly = isEdit;
      if (isEdit) releaseInput.setAttribute("aria-readonly", "true");
      else releaseInput.removeAttribute("aria-readonly");
    }
    var hint = root.querySelector(".lg-addon-hint");
    if (isEdit && !hint) {
      var form = root.querySelector("[data-addons-form]");
      if (form) {
        var p = document.createElement("p");
        p.className = "lg-addon-hint";
        p.textContent = "Release name cannot change after install; Redeploy updates the existing CR.";
        form.insertBefore(p, actions);
      }
    } else if (!isEdit && hint) {
      hint.remove();
    }
  }

  function showFieldError(id, msg) {
    var el = document.getElementById(id + "-err");
    var input = document.getElementById(id);
    if (el) {
      el.textContent = msg || "";
      el.hidden = !msg;
    }
    if (input) input.classList.toggle("is-invalid", !!msg);
  }

  function readForm() {
    function val(id) {
      var el = document.getElementById(id);
      return el && el.value != null ? String(el.value).trim() : "";
    }
    if (formKind === "virt") {
      var emu = document.getElementById("virt-emulation");
      return {
        kubevirtVersion: val("virt-kv") || DEFAULT_KV_VERSION,
        cdiVersion: val("virt-cdi") || DEFAULT_CDI_VERSION,
        useEmulation: !!(emu && emu.checked),
        release: val("virt-release") || DEFAULT_VIRT_RELEASE,
      };
    }
    if (formKind === "mon") {
      var am = document.getElementById("mon-alertmanager");
      return {
        namespace: val("mon-namespace") || DEFAULT_MON_NS,
        storageClassName: val("mon-sc"),
        retention: val("mon-retention") || "15d",
        grafanaAdminUser: val("mon-admin") || "admin",
        enableAlertmanager: !!(am && am.checked),
        release: val("mon-release") || DEFAULT_MON_RELEASE,
      };
    }
    var defEl = document.getElementById("nfs-default");
    return {
      server: val("nfs-server"),
      path: val("nfs-path"),
      namespace: val("nfs-namespace") || DEFAULT_NS,
      storageClassName: val("nfs-sc") || DEFAULT_SC,
      release: val("nfs-release") || DEFAULT_RELEASE,
      defaultClass: !!(defEl && defEl.checked),
    };
  }

  function validate(vals) {
    var ok = true;
    function req(id, v, label) {
      if (!v) {
        showFieldError(id, label + " is required");
        ok = false;
      } else {
        showFieldError(id, "");
      }
    }
    function dns1123(id, v) {
      if (v && !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(v)) {
        showFieldError(id, "Use a DNS-1123 name (lowercase, digits, dashes)");
        ok = false;
      }
    }
    if (formKind === "virt") {
      req("virt-kv", vals.kubevirtVersion, "KubeVirt version");
      req("virt-cdi", vals.cdiVersion, "CDI version");
      req("virt-release", vals.release, "Release name");
      dns1123("virt-release", vals.release);
      if (vals.kubevirtVersion && !/^v?\d+\.\d+\.\d+$/.test(vals.kubevirtVersion)) {
        showFieldError("virt-kv", "Use a release tag like v1.10.0 (must be vendored)");
        ok = false;
      }
      if (vals.cdiVersion && !/^v?\d+\.\d+\.\d+$/.test(vals.cdiVersion)) {
        showFieldError("virt-cdi", "Use a release tag like v1.66.1 (must be vendored)");
        ok = false;
      }
      return ok;
    }
    if (formKind === "mon") {
      req("mon-namespace", vals.namespace, "Namespace");
      req("mon-release", vals.release, "Release name");
      dns1123("mon-release", vals.release);
      dns1123("mon-namespace", vals.namespace);
      return ok;
    }
    req("nfs-server", vals.server, "NFS server");
    req("nfs-path", vals.path, "NFS path");
    req("nfs-namespace", vals.namespace, "Namespace");
    req("nfs-sc", vals.storageClassName, "StorageClass name");
    req("nfs-release", vals.release, "Release name");
    dns1123("nfs-release", vals.release);
    if (vals.storageClassName && !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/.test(vals.storageClassName)) {
      showFieldError("nfs-sc", "Invalid StorageClass name");
      ok = false;
    }
    return ok;
  }

  function setFormMsg(view, text, isError) {
    var el = view.querySelector("[data-form-msg]");
    if (!el) return;
    el.hidden = !text;
    el.textContent = text || "";
    el.classList.toggle("is-error", !!isError);
  }

  function setSubmitting(view, on) {
    submitting = !!on;
    var btns = view.querySelectorAll(
      '[data-addons-action="deploy"], [data-addons-action="redeploy"], [data-addons-action="uninstall"]'
    );
    for (var i = 0; i < btns.length; i++) btns[i].disabled = !!on;
  }

  function submitForm(view, isPatch) {
    if (submitting) return;
    var vals = readForm();
    draft = vals;
    if (!validate(vals)) {
      setFormMsg(view, "Fix the highlighted fields.", true);
      return;
    }
    setSubmitting(view, true);
    setFormMsg(view, isPatch ? "Redeploying…" : "Deploying…", false);

    var api = formKind === "virt" ? VIRT_API : formKind === "mon" ? MON_API : NFS_API;
    var kind =
      formKind === "virt" ? "Virtualization" : formKind === "mon" ? "Monitoring" : "NFSProvisioner";
    var spec;
    if (formKind === "virt") {
      spec = {
        kubevirtVersion: vals.kubevirtVersion,
        cdiVersion: vals.cdiVersion,
        useEmulation: !!vals.useEmulation,
      };
    } else if (formKind === "mon") {
      spec = {
        namespace: vals.namespace,
        storageClassName: vals.storageClassName || "",
        retention: vals.retention || "15d",
        grafanaAdminUser: vals.grafanaAdminUser || "admin",
        enableAlertmanager: !!vals.enableAlertmanager,
      };
    } else {
      spec = {
        server: vals.server,
        path: vals.path,
        namespace: vals.namespace,
        storageClassName: vals.storageClassName,
        defaultClass: !!vals.defaultClass,
      };
    }

    var req;
    if (isPatch) {
      req = fetchJSON(api + "/" + encodeURIComponent(vals.release), {
        method: "PATCH",
        headers: { "Content-Type": "application/merge-patch+json" },
        body: JSON.stringify({ spec: spec }),
      });
    } else {
      req = fetchJSON(api, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          apiVersion: "dashboard.ocp/v1alpha1",
          kind: kind,
          metadata: { name: vals.release },
          spec: spec,
        }),
      }).catch(function (err) {
        if (err && /already exists|Conflict|409/i.test(String(err.message || err))) {
          return fetchJSON(api + "/" + encodeURIComponent(vals.release), {
            method: "PATCH",
            headers: { "Content-Type": "application/merge-patch+json" },
            body: JSON.stringify({ spec: spec }),
          });
        }
        throw err;
      });
    }

    req
      .then(function () {
        editingName = vals.release;
        panel = "form";
        setFormMsg(view, "Submitted — waiting for operator…", false);
        return load(view, false);
      })
      .catch(function (err) {
        setFormMsg(view, err && err.message ? err.message : "Deploy failed", true);
      })
      .then(function () {
        setSubmitting(view, false);
      });
  }

  function uninstall(view) {
    if (submitting) return;
    var vals = readForm();
    var name = vals.release || editingName;
    if (!name) {
      setFormMsg(view, "No release to uninstall.", true);
      return;
    }
    var label =
      formKind === "virt"
        ? 'Uninstall kubeops-virtualization "' + name + '"? KubeVirt and CDI will be removed (may take several minutes).'
        : formKind === "mon"
          ? 'Uninstall kubeops-monitoring "' + name + '"? Prometheus/Grafana workloads will be removed.'
          : 'Uninstall kubeops-nfs provisioner "' + name + '"? Owned workloads and StorageClass will be removed.';
    if (!window.confirm(label)) return;
    setSubmitting(view, true);
    setFormMsg(view, "Uninstalling…", false);
    var api = formKind === "virt" ? VIRT_API : formKind === "mon" ? MON_API : NFS_API;
    fetchJSON(api + "/" + encodeURIComponent(name), { method: "DELETE" })
      .then(function () {
        panel = "catalog";
        formKind = "nfs";
        editingName = "";
        draft = null;
        lastStatusKey = "";
        return load(view, true);
      })
      .catch(function (err) {
        setFormMsg(view, err && err.message ? err.message : "Uninstall failed", true);
      })
      .then(function () {
        setSubmitting(view, false);
      });
  }

  function pickCR(items, preferName) {
    var cr = null;
    if (preferName) {
      for (var i = 0; i < items.length; i++) {
        if (items[i].metadata && items[i].metadata.name === preferName) {
          cr = items[i];
          break;
        }
      }
    }
    if (!cr && items.length) cr = items[0];
    return cr;
  }

  function load(view, forceFull) {
    if (!view) return Promise.resolve();
    if (loading && !forceFull) return Promise.resolve();
    loading = true;
    var root = view.querySelector("[data-addons-root]");
    if (!root) {
      loading = false;
      return Promise.resolve();
    }

    if (panel === "form") captureDraft();

    if (panel === "catalog" && forceFull) {
      root.className = "lg-addons-body lg-addons-loading";
      root.textContent = "Loading addons…";
    }

    return Promise.all([
      fetchJSON(NFS_API),
      fetchJSON(OPS_API + "/kubeops-nfs"),
      fetchJSON(VIRT_API),
      fetchJSON(OPS_API + "/kubeops-virtualization"),
      fetchJSON(MON_API),
      fetchJSON(OPS_API + "/kubeops-monitoring"),
    ])
      .then(function (pair) {
        if (!onAddonsRoute()) return;
        var nfsItems = (pair[0] && pair[0].items) || [];
        var nfsOp = pair[1];
        var virtItems = (pair[2] && pair[2].items) || [];
        var virtOp = pair[3];
        var monItems = (pair[4] && pair[4].items) || [];
        var monOp = pair[5];
        var nfsCr = pickCR(nfsItems, formKind === "nfs" ? editingName : null);
        var virtCr = pickCR(virtItems, formKind === "virt" ? editingName : null);
        var monCr = pickCR(monItems, formKind === "mon" ? editingName : null);
        if (!nfsCr && nfsItems.length) nfsCr = nfsItems[0];
        if (!virtCr && virtItems.length) virtCr = virtItems[0];
        if (!monCr && monItems.length) monCr = monItems[0];

        var nfsOpReady = !!(nfsOp && nfsOp.metadata && nfsOp.metadata.name === "kubeops-nfs");
        var virtOpReady = !!(
          virtOp &&
          virtOp.metadata &&
          virtOp.metadata.name === "kubeops-virtualization"
        );
        var monOpReady = !!(
          monOp &&
          monOp.metadata &&
          monOp.metadata.name === "kubeops-monitoring"
        );
        catalogCache = {
          nfs: nfsCr,
          virt: virtCr,
          mon: monCr,
          nfsOp: nfsOpReady,
          virtOp: virtOpReady,
          monOp: monOpReady,
        };

        if (panel === "form") {
          var cr = formKind === "virt" ? virtCr : formKind === "mon" ? monCr : nfsCr;
          var opReady = formKind === "virt" ? virtOpReady : formKind === "mon" ? monOpReady : nfsOpReady;
          if (cr && cr.metadata && cr.metadata.name) editingName = cr.metadata.name;
          var formAlive = !!root.querySelector("[data-addons-form]");
          if (formAlive && !forceFull) {
            patchFormStatus(root, cr, opReady);
          } else {
            renderForm(root, cr, opReady);
          }
          schedulePoll(view, cr);
        } else {
          renderCatalog(root, nfsCr, nfsOpReady, virtCr, virtOpReady, monCr, monOpReady);
          var pollCr =
            (monCr && monCr.status && monCr.status.phase === "Progressing" && monCr) ||
            (virtCr && virtCr.status && virtCr.status.phase === "Progressing" && virtCr) ||
            (nfsCr && nfsCr.status && nfsCr.status.phase === "Progressing" && nfsCr) ||
            monCr ||
            virtCr ||
            nfsCr;
          schedulePoll(view, pollCr);
        }
      })
      .catch(function (err) {
        if (!onAddonsRoute()) return;
        if (panel === "form" && root.querySelector("[data-addons-form]")) {
          setFormMsg(view, "Refresh failed: " + (err && err.message ? err.message : "error"), true);
        } else {
          root.className = "lg-addons-body lg-addons-error";
          root.textContent =
            "Could not load addons (" + (err && err.message ? err.message : "error") + ").";
        }
      })
      .then(function () {
        loading = false;
      });
  }

  function schedulePoll(view, cr) {
    if (timer) clearInterval(timer);
    var phase = cr && cr.status && cr.status.phase;
    var ms = phase === "Progressing" ? 3000 : 10000;
    timer = setInterval(function () {
      if (!onAddonsRoute()) {
        hide();
        return;
      }
      // Soft refresh — never forceFull on poll (preserves typed fields).
      load(view, false);
    }, ms);
  }

  function syncNavActive() {
    var active = onAddonsRoute();
    document.documentElement.classList.toggle("lg-addons-route", active);
    document.body && document.body.classList.toggle("lg-addons-route", active);
    var link = document.querySelector("#nav-addons a.kd-nav-item-button");
    if (link) link.classList.toggle("kd-nav-item-button-active", active);
    var settingsLink = document.querySelector("#nav-settings > a.kd-nav-item-button");
    if (settingsLink) {
      if (active) {
        if (settingsLink.classList.contains("kd-nav-item-button-active")) {
          settingsLink.classList.remove("kd-nav-item-button-active");
          settingsLink.setAttribute("data-lg-addons-suppressed-active", "1");
        }
      } else if (settingsLink.getAttribute("data-lg-addons-suppressed-active") === "1") {
        settingsLink.removeAttribute("data-lg-addons-suppressed-active");
        if (isExactSettingsPath(location.hash) && !hasAddonsView(location.hash)) {
          settingsLink.classList.add("kd-nav-item-button-active");
        }
      }
    }
  }

  function setBreadcrumbs(active) {
    var host = document.querySelector(
      "kd-breadcrumbs .kd-breadcrumbs-container, .kd-breadcrumbs-container"
    );
    if (!host) return;
    if (!active) {
      var custom = host.querySelector(".lg-addons-breadcrumbs");
      if (custom) custom.remove();
      var kids = host.children;
      for (var i = 0; i < kids.length; i++) {
        if (kids[i].classList && kids[i].classList.contains("lg-addons-breadcrumbs")) continue;
        kids[i].style.removeProperty("display");
      }
      return;
    }
    // Hide Angular's native "Settings" crumb (CSS also enforces via .lg-addons-route).
    var kids2 = host.children;
    for (var j = 0; j < kids2.length; j++) {
      if (kids2[j].classList && kids2[j].classList.contains("lg-addons-breadcrumbs")) continue;
      kids2[j].style.setProperty("display", "none", "important");
    }
    var bar = host.querySelector(".lg-addons-breadcrumbs");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "lg-addons-breadcrumbs kd-inline-breadcrumb";
      // Top-level nav tile — same style as Settings/About (no parent crumb).
      bar.innerHTML = '<span class="kd-current-breadcrumb">Addons</span>';
      host.appendChild(bar);
    } else {
      bar.innerHTML = '<span class="kd-current-breadcrumb">Addons</span>';
    }
  }

  function show() {
    var host = contentHost();
    if (!host) return false;
    var view = ensureView(host);
    host.classList.add("lg-addons-active");
    view.hidden = false;
    view.style.display = "";
    load(view, true);
    setBreadcrumbs(true);
    syncNavActive();
    return true;
  }

  function hide() {
    captureDraft();
    var hosts = document.querySelectorAll(".kd-scroll-container.lg-addons-active, .lg-addons-active");
    for (var i = 0; i < hosts.length; i++) {
      hosts[i].classList.remove("lg-addons-active");
    }
    var views = document.querySelectorAll(".lg-addons-view, #" + VIEW_ID);
    for (var j = 0; j < views.length; j++) {
      views[j].hidden = true;
      views[j].style.display = "none";
      if (views[j].parentNode) views[j].parentNode.removeChild(views[j]);
    }
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
    panel = "catalog";
    editingName = "";
    draft = null;
    lastStatusKey = "";
    submitting = false;
    loading = false;
    setBreadcrumbs(false);
    syncNavActive();
  }

  function stripAddonsParam(hash) {
    if (!hash) return hash;
    return hash
      .replace(/([?&])view=addons(&|$)/g, function (_, sep, end) {
        if (sep === "?" && end === "&") return "?";
        if (sep === "?" && end === "") return "";
        if (sep === "&" && end === "") return "";
        return end === "&" ? sep : "";
      })
      .replace(/\?&/, "?")
      .replace(/[?&]$/, "");
  }

  function healStickyAddonsParam() {
    var h = location.hash || "";
    if (!hasAddonsView(h)) return false;
    if (isExactSettingsPath(h)) return false;
    if (healing) return true;
    healing = true;
    var cleaned = stripAddonsParam(h) || "#/workloads";
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
    if (healStickyAddonsParam()) return;
    // Leave-guard: Angular reattached view=addons on Settings — strip before show().
    if (
      leaveGuardUntil > Date.now() &&
      onAddonsRoute() &&
      isExactSettingsPath(location.hash)
    ) {
      var cleaned = stripAddonsParam(location.hash || "");
      if (cleaned && cleaned !== location.hash) {
        hide();
        location.replace(location.pathname + location.search + cleaned);
        return;
      }
    }
    if (onAddonsRoute()) {
      clearAddonsLeaveGuard();
      show();
    } else hide();
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
        if (a.closest && a.closest("#nav-addons")) {
          clearAddonsLeaveGuard();
          return;
        }

        // Status links inside the form: leave Addons cleanly to the target page.
        var inAddons = a.closest && a.closest("#" + VIEW_ID);
        var viaSettingsNav = !!(a.closest && a.closest("#nav-settings"));
        var viaSettingsCrumb = !!(a.classList && a.classList.contains("kd-faded-breadcrumb"));
        var href = a.getAttribute("href") || "";
        if (href.charAt(0) !== "#") return;

        var hrefIsAddons = isExactSettingsPath(href) && hasAddonsView(href);
        // Settings nav href is rewritten to include view=addons (queryParamsHandling=preserve).
        // Still treat it as a leave-to-Settings intent.
        if (hrefIsAddons && !viaSettingsNav && !viaSettingsCrumb) return;

        var leaving = onAddonsRoute() || !!document.querySelector(".lg-addons-view");
        if (!(leaving || hasAddonsView(href) || hasAddonsView(location.hash))) return;

        // Allow in-form deep links (StorageClass / Pods) to navigate after hide.
        if (inAddons && a.classList.contains("lg-addon-status-link")) {
          evt.preventDefault();
          evt.stopPropagation();
          if (typeof evt.stopImmediatePropagation === "function") evt.stopImmediatePropagation();
          hide();
          location.hash = href;
          armAddonsLeaveGuard();
          return;
        }

        // Don't intercept clicks on form controls / buttons.
        if (inAddons && !a.classList.contains("lg-addon-status-link")) {
          if (viaSettingsCrumb) {
            /* fall through — leave to Settings */
          } else {
            return;
          }
        }

        evt.preventDefault();
        evt.stopPropagation();
        if (typeof evt.stopImmediatePropagation === "function") {
          evt.stopImmediatePropagation();
        }
        hide();

        var next;
        if (viaSettingsNav || viaSettingsCrumb) {
          next = settingsHrefPlain();
        } else {
          next = stripAddonsParam(href) || href;
        }
        if (!next || next === "#") next = "#/workloads";
        if (next.charAt(0) !== "#") next = "#" + next;
        location.hash = next;
        // Strip view=addons if Angular preserve reattaches it on #/settings.
        if (viaSettingsNav || viaSettingsCrumb || isExactSettingsPath(next)) {
          armAddonsLeaveGuard();
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
