/**
 * kube-dashboard — sidebar accordion for Kubernetes Dashboard nav.
 *
 * - Tap a main tile: open that page AND expand its submenu.
 * - Tap the chevron arrow only: expand / collapse the submenu (no navigation).
 */
(function () {
  "use strict";

  var STORAGE_KEY = "lg-nav-open-ids";
  var READY_KEY = "lg-nav-accordion-ready";
  var bound = false;
  var started = false;

  function loadOpenIds() {
    try {
      var raw = sessionStorage.getItem(STORAGE_KEY);
      if (raw === null) return null;
      var parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch (e) {
      return [];
    }
  }

  function saveOpenIds(ids) {
    try {
      sessionStorage.setItem(STORAGE_KEY, JSON.stringify(ids));
    } catch (e) {
      /* ignore */
    }
  }

  function groupId(group) {
    return group.getAttribute("id") || (group.textContent || "").trim();
  }

  function navRoot(from) {
    return (from && from.closest && from.closest(".kd-nav-items")) || document.querySelector(".kd-nav-items");
  }

  function isGroup(el) {
    return !!(el && el.classList && el.classList.contains("kd-nav-group-item"));
  }

  function isChild(el) {
    if (!el) return false;
    if (el.classList && el.classList.contains("lg-nav-injected")) return true;
    if (el.tagName === "KD-PINNER-NAV") return true;
    if (el.tagName === "KD-NAV-ITEM") return !isGroup(el);
    return false;
  }

  var OPERATORS_ID = "nav-operators";
  // Use a real Cluster route + query flag so Angular does not bounce the hash.
  var OPERATORS_HREF = "#/cluster?view=operators";

  function onOperatorsHash() {
    var h = location.hash || "";
    return h.indexOf("#/cluster") === 0 && /(?:[?&])view=operators(?:&|$)/.test(h);
  }

  function operatorsHref() {
    var ns = "default";
    try {
      var m = (location.hash || "").match(/[?&]namespace=([^&]*)/);
      if (m && m[1]) ns = decodeURIComponent(m[1]);
    } catch (e) {
      /* ignore */
    }
    if (!ns) ns = "default";
    return "#/cluster?namespace=" + encodeURIComponent(ns) + "&view=operators";
  }

  function ensureOperatorsNav(root) {
    if (!root) return;
    var existing = document.getElementById(OPERATORS_ID);
    if (existing && existing.parentElement === root) {
      // Keep active state in sync with location.
      var link = existing.querySelector("a.kd-nav-item-button");
      if (link) {
        link.href = operatorsHref();
        link.classList.toggle("kd-nav-item-button-active", onOperatorsHash());
      }
      return;
    }
    if (existing) existing.remove();

    var cluster = root.querySelector("#nav-cluster");
    if (!cluster) return;
    // Prefer after Nodes; fall back to last Cluster child before next group.
    var after = root.querySelector("#nav-node") || cluster;
    var probe = after.nextElementSibling;
    while (probe && !isGroup(probe) && probe.tagName !== "MAT-DIVIDER") {
      if (isChild(probe) && probe.id !== OPERATORS_ID) after = probe;
      else break;
      probe = after.nextElementSibling;
    }

    var item = document.createElement("kd-nav-item");
    item.id = OPERATORS_ID;
    item.className = "kd-nav-item lg-nav-injected";
    item.setAttribute("data-lg-operators", "1");

    var a = document.createElement("a");
    a.className = "kd-nav-item-button mat-button";
    a.href = operatorsHref();
    a.setAttribute("aria-label", "Operators");
    if (onOperatorsHash()) {
      a.classList.add("kd-nav-item-button-active");
    }
    a.addEventListener("click", function (evt) {
      evt.preventDefault();
      var next = operatorsHref();
      if (location.hash !== next) {
        location.hash = next;
      } else {
        try {
          window.dispatchEvent(new Event("hashchange"));
        } catch (e) {
          /* ignore */
        }
      }
      // Ensure Cluster group stays expanded.
      if (!cluster.classList.contains("lg-nav-open")) {
        setOpen(cluster, true);
        syncStorageFromDom(root);
      }
    });

    var wrap = document.createElement("span");
    wrap.className = "mat-button-wrapper";
    wrap.textContent = "Operators";
    a.appendChild(wrap);
    item.appendChild(a);

    if (after.nextSibling) {
      root.insertBefore(item, after.nextSibling);
    } else {
      root.appendChild(item);
    }
  }

  function childrenOf(group) {
    var kids = [];
    var el = group.nextElementSibling;
    while (el) {
      if (isGroup(el) || el.tagName === "MAT-DIVIDER") break;
      if (isChild(el)) kids.push(el);
      el = el.nextElementSibling;
    }
    return kids;
  }

  function placeChevronInLink(link, btn) {
    var wrapper = link.querySelector(".mat-button-wrapper");
    var ripple = link.querySelector(".mat-button-ripple, .mat-ripple");
    if (wrapper && ripple) {
      link.insertBefore(btn, ripple);
    } else if (wrapper) {
      wrapper.insertAdjacentElement("afterend", btn);
    } else {
      link.appendChild(btn);
    }
  }

  function ensureChevron(group) {
    // Flex-row trailing control inside the mat-button link:
    // [ .mat-button-wrapper (label + badge) ] [ .lg-nav-chevron ]
    var link = group.querySelector("a.kd-nav-item-button");
    if (!link) return;

    var existing = group.querySelector(".lg-nav-chevron");
    if (existing) {
      if (existing.parentElement !== link) {
        placeChevronInLink(link, existing);
      }
      return;
    }

    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "lg-nav-chevron";
    btn.setAttribute("aria-label", "Expand or collapse section");
    btn.tabIndex = 0;
    placeChevronInLink(link, btn);
  }

  function setOpen(group, open) {
    var kids = childrenOf(group);
    if (!kids.length) return false;

    group.classList.add("lg-nav-toggleable");
    ensureChevron(group);
    group.classList.toggle("lg-nav-open", !!open);
    group.classList.toggle("lg-nav-collapsed", !open);
    group.setAttribute("aria-expanded", open ? "true" : "false");
    group.setAttribute("data-lg-accordion", "true");

    var chevron = group.querySelector(".lg-nav-chevron");
    if (chevron) {
      chevron.setAttribute("aria-expanded", open ? "true" : "false");
      chevron.title = open ? "Collapse section" : "Expand section";
    }

    for (var i = 0; i < kids.length; i++) {
      kids[i].classList.toggle("lg-nav-hidden", !open);
      kids[i].setAttribute("aria-hidden", open ? "false" : "true");
    }
    return true;
  }

  function syncStorageFromDom(root) {
    if (!root) return;
    var groups = root.querySelectorAll(".kd-nav-group-item");
    var open = [];
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].classList.contains("lg-nav-open")) open.push(groupId(groups[i]));
    }
    saveOpenIds(open);
  }

  function groupHasActiveChild(group) {
    var kids = childrenOf(group);
    for (var i = 0; i < kids.length; i++) {
      if (kids[i].querySelector(".kd-nav-item-button-active")) return true;
    }
    return false;
  }

  function groupIsActive(group) {
    return !!group.querySelector("a.kd-nav-item-button-active");
  }

  function apply(root) {
    if (!root) return false;

    var groups = root.querySelectorAll(".kd-nav-group-item");
    if (!groups.length) return false;

    var stored = loadOpenIds();
    var initialized = stored !== null;
    var anyKids = false;
    var activeFound = false;
    var firstToggleable = null;

    for (var i = 0; i < groups.length; i++) {
      var group = groups[i];
      var kids = childrenOf(group);
      if (!kids.length) {
        group.classList.remove("lg-nav-open", "lg-nav-collapsed", "lg-nav-toggleable");
        group.removeAttribute("data-lg-accordion");
        var orphan = group.querySelector(".lg-nav-chevron");
        if (orphan) orphan.remove();
        continue;
      }

      anyKids = true;
      if (!firstToggleable) firstToggleable = group;
      if (groupHasActiveChild(group) || groupIsActive(group)) activeFound = true;
    }

    for (var j = 0; j < groups.length; j++) {
      var g = groups[j];
      if (!childrenOf(g).length) continue;

      var id = groupId(g);
      var shouldOpen;
      if (!initialized) {
        shouldOpen =
          groupHasActiveChild(g) ||
          groupIsActive(g) ||
          (!activeFound && g === firstToggleable);
      } else {
        // Trust chevron/tile storage; auto-open only when a child route is active.
        shouldOpen = stored.indexOf(id) !== -1 || groupHasActiveChild(g);
      }
      setOpen(g, shouldOpen);
    }

    if (!anyKids) return false;

    if (!initialized) syncStorageFromDom(root);
    try {
      sessionStorage.setItem(READY_KEY, "1");
    } catch (e) {
      /* ignore */
    }
    return true;
  }

  function toggleGroup(group) {
    if (!childrenOf(group).length) return;
    setOpen(group, !group.classList.contains("lg-nav-open"));
    syncStorageFromDom(navRoot(group));
  }

  function openGroup(group) {
    if (!childrenOf(group).length) return;
    if (!group.classList.contains("lg-nav-open")) {
      setOpen(group, true);
      syncStorageFromDom(navRoot(group));
    }
  }

  function bind() {
    if (bound) return;
    bound = true;

    // Chevron only: collapse / expand, do not navigate.
    document.addEventListener(
      "click",
      function (evt) {
        var chevron = evt.target.closest && evt.target.closest(".lg-nav-chevron");
        if (!chevron) return;

        var group = chevron.closest(".kd-nav-group-item");
        if (!group || !childrenOf(group).length) return;

        evt.preventDefault();
        evt.stopPropagation();
        if (typeof evt.stopImmediatePropagation === "function") {
          evt.stopImmediatePropagation();
        }
        toggleGroup(group);
      },
      true
    );

    // Main tile / link: allow Angular navigation, and expand submenu.
    document.addEventListener(
      "click",
      function (evt) {
        if (evt.target.closest && evt.target.closest(".lg-nav-chevron")) return;

        var group = evt.target.closest && evt.target.closest(".kd-nav-group-item");
        if (!group || !childrenOf(group).length) return;

        // Do not preventDefault — tile must open its overview page.
        group.classList.add("lg-nav-toggleable");
        openGroup(group);
      },
      true
    );

    document.addEventListener(
      "keydown",
      function (evt) {
        if (evt.key !== "Enter" && evt.key !== " ") return;
        var chevron = evt.target.closest && evt.target.closest(".lg-nav-chevron");
        if (!chevron) return;
        var group = chevron.closest(".kd-nav-group-item");
        if (!group || !childrenOf(group).length) return;
        evt.preventDefault();
        evt.stopPropagation();
        toggleGroup(group);
      },
      true
    );
  }

  function refresh() {
    var roots = document.querySelectorAll(".kd-nav-items");
    var ok = false;
    for (var i = 0; i < roots.length; i++) {
      ensureOperatorsNav(roots[i]);
      if (apply(roots[i])) ok = true;
    }
    return ok;
  }

  function boot() {
    if (started) return;
    started = true;
    bind();

    var tries = 0;
    var timer = setInterval(function () {
      tries += 1;
      if (refresh() || tries > 80) clearInterval(timer);
    }, 250);

    var scheduled = false;
    var obs = new MutationObserver(function () {
      if (scheduled) return;
      scheduled = true;
      setTimeout(function () {
        scheduled = false;
        refresh();
      }, 30);
    });

    if (document.body) {
      obs.observe(document.body, { childList: true, subtree: true });
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
