const state = {
  config: null,
  currentView: "dashboard",
  autoRefresh: false,
  autoTimer: null,
};

const AUTO_REFRESH_MS = 5000;
// Views that show live cluster state and are safe to poll without disturbing input.
const LIVE_VIEWS = new Set(["dashboard", "workers", "metrics"]);

const titles = {
  dashboard: ["Dashboard", "Cluster readiness, workers, and execution activity."],
  workers: ["Workers", "Inspect registered workers and drain nodes from scheduling."],
  execute: ["Execute", "Run a WASM module now or create a durable job."],
  jobs: ["Jobs", "Look up durable job state by request ID."],
  metrics: ["Metrics", "Inspect master counters and request activity."],
  settings: ["Settings", "Configure the UI backend mTLS client."],
};

document.addEventListener("DOMContentLoaded", () => {
  bindNavigation();
  bindActions();
  loadConfig();
  refreshCurrentView();
});

function bindNavigation() {
  document.querySelectorAll(".nav-item").forEach((button) => {
    button.addEventListener("click", () => {
      setView(button.dataset.view);
    });
  });
}

function bindActions() {
  document.getElementById("refresh-btn").addEventListener("click", refreshCurrentView);
  document.getElementById("autorefresh-btn").addEventListener("click", toggleAutoRefresh);
  document.getElementById("workers-refresh").addEventListener("click", loadWorkers);
  document.getElementById("metrics-refresh").addEventListener("click", loadMetrics);
  document.getElementById("execute-form").addEventListener("submit", executeNow);
  document.getElementById("create-job-from-execute").addEventListener("click", createJobFromExecute);
  document.getElementById("job-get-form").addEventListener("submit", getJob);
  document.getElementById("settings-form").addEventListener("submit", saveConfig);
}

function setView(view) {
  state.currentView = view;
  document.querySelectorAll(".nav-item").forEach((button) => {
    button.classList.toggle("active", button.dataset.view === view);
  });
  document.querySelectorAll(".view").forEach((section) => {
    section.classList.toggle("active", section.id === `${view}-view`);
  });
  document.getElementById("view-title").textContent = titles[view][0];
  document.getElementById("view-subtitle").textContent = titles[view][1];
  refreshCurrentView();
}

function toggleAutoRefresh() {
  state.autoRefresh = !state.autoRefresh;
  const button = document.getElementById("autorefresh-btn");
  button.textContent = `Auto-refresh: ${state.autoRefresh ? "On" : "Off"}`;
  button.setAttribute("aria-pressed", String(state.autoRefresh));
  button.classList.toggle("primary", state.autoRefresh);
  button.classList.toggle("secondary", !state.autoRefresh);

  if (state.autoTimer) {
    window.clearInterval(state.autoTimer);
    state.autoTimer = null;
  }
  if (state.autoRefresh) {
    state.autoTimer = window.setInterval(() => {
      if (LIVE_VIEWS.has(state.currentView)) {
        refreshCurrentView();
      }
    }, AUTO_REFRESH_MS);
  }
}

async function refreshCurrentView() {
  if (state.currentView === "dashboard") {
    await loadDashboard();
  } else if (state.currentView === "workers") {
    await loadWorkers();
  } else if (state.currentView === "metrics") {
    await loadMetrics();
  } else if (state.currentView === "settings") {
    await loadConfig();
  }
}

async function loadDashboard() {
  const healthValue = document.getElementById("health-value");
  const readyValue = document.getElementById("ready-value");
  const workersValue = document.getElementById("active-workers-value");
  const dispatchValue = document.getElementById("dispatch-success-value");
  const message = document.getElementById("dashboard-message");

  // Use allSettled so a single gated endpoint (for example /api/v1/workers when
  // an execute-client allowlist is configured) does not blank the whole view.
  const [health, ready, metrics, workers] = await Promise.allSettled([
    apiGet("/ui/api/health"),
    apiGet("/ui/api/ready"),
    apiGet("/ui/api/metrics"),
    apiGet("/ui/api/workers"),
  ]);

  healthValue.textContent = health.status === "fulfilled" ? health.value.status || "-" : "error";
  readyValue.textContent = ready.status === "fulfilled" ? ready.value.status || "-" : "error";
  workersValue.textContent = workers.status === "fulfilled" ? String(workers.value.length) : "error";
  dispatchValue.textContent =
    metrics.status === "fulfilled" ? String(metrics.value.master?.dispatch_success ?? 0) : "error";

  const errors = [];
  if (health.status === "rejected") errors.push(`health: ${health.reason.message}`);
  if (ready.status === "rejected") errors.push(`ready: ${ready.reason.message}`);
  if (metrics.status === "rejected") errors.push(`metrics: ${metrics.reason.message}`);
  if (workers.status === "rejected") errors.push(`workers: ${workers.reason.message}`);

  if (errors.length === 0) {
    const role = health.value.role || "unknown";
    const requests = metrics.value.requests_total ?? 0;
    message.textContent = `Master role ${role} has ${requests} observed HTTP requests.`;
    setConnection("ok", "Connected");
  } else if (errors.length === 4) {
    message.textContent = errors[0];
    setConnection("bad", "Disconnected");
  } else {
    message.textContent = `Partial cluster state. ${errors.join(" | ")}`;
    setConnection("warn", "Degraded");
  }
  document.getElementById("last-refresh").textContent = `Refreshed ${new Date().toLocaleTimeString()}`;
}

async function loadWorkers() {
  const tbody = document.getElementById("workers-table");
  tbody.innerHTML = `<tr><td colspan="8">Loading workers...</td></tr>`;
  try {
    const workers = await apiGet("/ui/api/workers");
    if (!workers.length) {
      tbody.innerHTML = `<tr><td colspan="8">No workers registered.</td></tr>`;
      return;
    }

    tbody.innerHTML = "";
    for (const worker of workers) {
      const state = worker.state || "ready";
      const draining = state === "draining";
      const stateBadge = `<span class="pill ${draining ? "pill-warn" : "pill-ok"}">${escapeHTML(state)}</span>`;
      const action = draining
        ? `<button class="button secondary" disabled>Draining</button>`
        : `<button class="button danger" data-worker="${escapeAttr(worker.id)}">Drain</button>`;
      const row = document.createElement("tr");
      row.innerHTML = `
        <td>${escapeHTML(worker.id)}</td>
        <td>${escapeHTML(worker.ip_address)}</td>
        <td>${stateBadge}</td>
        <td>${formatNumber(worker.cpu_free, 1)}%</td>
        <td>${formatNumber(worker.ram_free_mb, 0)} MB</td>
        <td>${formatNumber(worker.latitude, 4)}, ${formatNumber(worker.longitude, 4)}</td>
        <td>${formatAge(worker.last_seen)}</td>
        <td>${action}</td>
      `;
      if (!draining) {
        row.querySelector("button").addEventListener("click", () => drainWorker(worker.id));
      }
      tbody.appendChild(row);
    }
  } catch (error) {
    tbody.innerHTML = `<tr><td colspan="8">${escapeHTML(error.message)}</td></tr>`;
  }
}

async function drainWorker(workerID) {
  if (!window.confirm(`Drain worker ${workerID}?`)) {
    return;
  }
  try {
    const response = await apiPost(`/ui/api/workers/${encodeURIComponent(workerID)}/drain`, {});
    showToast(response.message || `Worker ${workerID} marked draining`);
    await loadWorkers();
  } catch (error) {
    showToast(error.message);
  }
}

async function executeNow(event) {
  event.preventDefault();
  const output = document.getElementById("execute-result");
  output.textContent = "Executing...";
  try {
    const response = await apiPost("/ui/api/execute", executionRequestFromForm());
    output.textContent = JSON.stringify(response, null, 2);
    showToast("Execution completed");
  } catch (error) {
    output.textContent = error.message;
    showToast(error.message);
  }
}

async function createJobFromExecute() {
  const output = document.getElementById("execute-result");
  output.textContent = "Creating durable job...";
  try {
    const response = await apiPost("/ui/api/jobs", executionRequestFromForm());
    output.textContent = JSON.stringify(response, null, 2);
    showToast("Durable job created");
  } catch (error) {
    output.textContent = error.message;
    showToast(error.message);
  }
}

async function getJob(event) {
  event.preventDefault();
  const requestID = new FormData(event.target).get("request_id").trim();
  const output = document.getElementById("job-result");
  output.textContent = "Loading job...";
  try {
    const response = await apiGet(`/ui/api/jobs/${encodeURIComponent(requestID)}`);
    output.textContent = JSON.stringify(response, null, 2);
  } catch (error) {
    output.textContent = error.message;
  }
}

async function loadMetrics() {
  const grid = document.getElementById("metrics-grid");
  const json = document.getElementById("metrics-json");
  grid.innerHTML = "";
  json.textContent = "Loading metrics...";
  try {
    const metrics = await apiGet("/ui/api/metrics");
    const cards = [
      ["Role", metrics.role],
      ["Uptime", `${metrics.uptime_seconds}s`],
      ["Requests", metrics.requests_total],
      ["Active workers", metrics.master?.active_workers ?? "-"],
      ["Dispatch failures", metrics.master?.dispatch_failure ?? "-"],
      ["Reschedules", metrics.master?.dispatch_reschedules ?? "-"],
    ];
    for (const [label, value] of cards) {
      const tile = document.createElement("div");
      tile.className = "metric-tile";
      tile.innerHTML = `<span>${escapeHTML(label)}</span><strong>${escapeHTML(String(value ?? "-"))}</strong>`;
      grid.appendChild(tile);
    }
    json.textContent = JSON.stringify(metrics, null, 2);
  } catch (error) {
    json.textContent = error.message;
  }
}

async function loadConfig() {
  try {
    const response = await apiGet("/ui/api/config");
    state.config = response.config;
    document.getElementById("config-path").value = response.path || "";
    fillForm(document.getElementById("settings-form"), response.config);
    fillExecutionDefaults(response.config);
    document.getElementById("settings-message").textContent = response.exists
      ? "Config loaded."
      : `Default config shown. ${response.error || "Save it before calling the master."}`;
  } catch (error) {
    document.getElementById("settings-message").textContent = error.message;
  }
}

async function saveConfig(event) {
  event.preventDefault();
  const form = event.target;
  const data = new FormData(form);
  const config = {
    master_url: data.get("master_url").trim(),
    ca_cert: data.get("ca_cert").trim(),
    client_cert: data.get("client_cert").trim(),
    client_key: data.get("client_key").trim(),
    default_user_lat: numberOrZero(data.get("default_user_lat")),
    default_user_lon: numberOrZero(data.get("default_user_lon")),
    output: data.get("output") || "table",
  };
  try {
    const response = await apiPut("/ui/api/config", config);
    state.config = response.config;
    fillExecutionDefaults(response.config);
    document.getElementById("settings-message").textContent = "Settings saved.";
    showToast("Settings saved");
  } catch (error) {
    document.getElementById("settings-message").textContent = error.message;
    showToast(error.message);
  }
}

function executionRequestFromForm() {
  const form = document.getElementById("execute-form");
  const data = new FormData(form);
  return {
    request_id: data.get("request_id").trim(),
    module_name: data.get("module_name").trim(),
    module_url: data.get("module_url").trim(),
    module_registry_url: data.get("module_registry_url").trim(),
    module_digest: data.get("module_digest").trim(),
    abi: data.get("abi") || "",
    user_lat: numberOrZero(data.get("user_lat")),
    user_lon: numberOrZero(data.get("user_lon")),
    payload: data.get("payload") || "",
  };
}

function fillForm(form, values) {
  for (const [key, value] of Object.entries(values || {})) {
    const field = form.elements.namedItem(key);
    if (field) {
      field.value = value ?? "";
    }
  }
}

function fillExecutionDefaults(config) {
  const form = document.getElementById("execute-form");
  if (!form.elements.user_lat.value) {
    form.elements.user_lat.value = config.default_user_lat ?? 0;
  }
  if (!form.elements.user_lon.value) {
    form.elements.user_lon.value = config.default_user_lon ?? 0;
  }
}

async function apiGet(path) {
  return api(path, { method: "GET" });
}

async function apiPost(path, body) {
  return api(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function apiPut(path, body) {
  return api(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function api(path, options) {
  const response = await fetch(path, options);
  const text = await response.text();
  let payload = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { error: text };
    }
  }
  if (!response.ok) {
    throw new Error(payload.error || `Request failed with ${response.status}`);
  }
  return payload;
}

function setConnection(level, text) {
  const badge = document.getElementById("connection-state");
  badge.textContent = text;
  badge.className = `status ${level}`;
}

function showToast(message) {
  const toast = document.getElementById("toast");
  toast.textContent = message;
  toast.classList.add("show");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => toast.classList.remove("show"), 2600);
}

function formatAge(value) {
  if (!value) {
    return "unknown";
  }
  const lastSeen = new Date(value);
  if (Number.isNaN(lastSeen.getTime())) {
    return "unknown";
  }
  const seconds = Math.max(0, Math.round((Date.now() - lastSeen.getTime()) / 1000));
  if (seconds < 60) {
    return `${seconds}s ago`;
  }
  const minutes = Math.round(seconds / 60);
  return `${minutes}m ago`;
}

function formatNumber(value, digits) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return "-";
  }
  return number.toFixed(digits);
}

function numberOrZero(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : 0;
}

function escapeHTML(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttr(value) {
  return escapeHTML(String(value));
}
