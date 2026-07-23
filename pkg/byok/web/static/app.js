// BYOK dashboard: thin fetch() layer over /api/*.
async function api(path, opts = {}) {
  const resp = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    ...opts,
  });
  if (resp.status === 401) {
    window.location = "/login?return_to=/app";
    throw new Error("not logged in");
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}));
    throw new Error(body.error || resp.statusText);
  }
  return resp.status === 204 ? null : resp.json();
}

function el(tag, attrs = {}, text = "") {
  const node = document.createElement(tag);
  Object.entries(attrs).forEach(([k, v]) => node.setAttribute(k, v));
  if (text) node.textContent = text;
  return node;
}

async function refreshMe() {
  const me = await api("/api/me");
  document.getElementById("whoami").textContent = me.username;
}

function updateGrantFormAvailability() {
  const credentialSelect = document.querySelector("#grant-form select[name=credential_ids]");
  const modelSelect = document.querySelector("#grant-form select[name=allowed_models]");
  const submit = document.querySelector("#grant-form button[type=submit]");
  const hasCredentials = Array.from(credentialSelect.options).some((option) => !option.disabled && option.value);
  const hasModels = Array.from(modelSelect.options).some((option) => !option.disabled && option.value);
  const unavailable = !hasCredentials || !hasModels;
  submit.disabled = unavailable;
  const tokenSubmit = document.querySelector("#token-form button[type=submit]");
  if (tokenSubmit) tokenSubmit.disabled = !hasCredentials;
}

async function refreshCredentials() {
  const creds = await api("/api/credentials");
  const tbody = document.querySelector("#credentials-table tbody");
  const selects = document.querySelectorAll("select[name=credential_ids]");
  tbody.innerHTML = "";
  selects.forEach((select) => { select.innerHTML = ""; });
  if (creds.length === 0) {
    selects.forEach((select) => {
      select.append(el("option", { disabled: "", selected: "" }, "No provider credentials available"));
      select.disabled = true;
    });
  } else {
    selects.forEach((select) => { select.disabled = false; });
  }
  for (const c of creds) {
    const tr = el("tr");
    tr.append(el("td", {}, c.label), el("td", {}, c.provider), el("td", {}, c.api_type), el("td", {}, c.secret_last4));
    const btn = el("button", { class: "btn btn-outline-danger btn-sm" }, "delete");
    btn.onclick = async () => {
      if (!confirm(`Delete credential "${c.label}"? Tokens bound only to it will be revoked.`)) return;
      await api(`/api/credentials/${c.id}`, { method: "DELETE" });
      await refreshAll();
    };
    const td = el("td");
    td.append(btn);
    tr.append(td);
    tbody.append(tr);
    selects.forEach((select) => select.append(el("option", { value: c.id }, `${c.label} (${c.api_type})`)));
  }
  updateGrantFormAvailability();
}

function usageCell(t) {
  const td = el("td", { class: "usage-bar" });
  if (t.max_total_tokens) {
    const pct = Math.min(100, Math.round((t.used_tokens / t.max_total_tokens) * 100));
    const bar = el("div", { class: "progress", role: "progressbar" });
    const inner = el("div", { class: `progress-bar ${pct >= 100 ? "bg-danger" : ""}`, style: `width: ${pct}%` }, `${pct}%`);
    bar.append(inner);
    td.append(bar, el("small", { class: "text-muted" }, `${t.used_tokens} / ${t.max_total_tokens} tok`));
  } else {
    td.append(el("small", {}, `${t.used_tokens} tok · ${t.used_requests} req`));
  }
  return td;
}

async function refreshGrantModels() {
  const models = await api("/api/grant-models");
  const select = document.querySelector("#grant-form select[name=allowed_models]");
  select.innerHTML = "";
  if (models.length === 0) {
    select.append(el("option", { disabled: "", selected: "" }, "No configured profiles available"));
    select.disabled = true;
  } else {
    select.disabled = false;
    models.forEach((model) => select.append(el("option", { value: model }, model)));
  }
  updateGrantFormAvailability();
}

async function refreshGrants() {
  const grants = await api("/api/agent-grants");
  const tbody = document.querySelector("#grants-table tbody");
  tbody.innerHTML = "";
  for (const grant of grants) {
    const tr = el("tr");
    const status = grant.revoked_at || !grant.enabled ? "revoked" : "active";
    const usage = grant.grant_max_total_tokens
      ? `${grant.used_tokens} / ${grant.grant_max_total_tokens} tok · ${grant.used_requests} req`
      : `${grant.used_tokens} tok · ${grant.used_requests} req`;
    tr.append(
      el("td", {}, grant.name),
      el("td", {}, (grant.allowed_models || []).join(", ")),
      el("td", {}, usage),
      el("td", {}, `${grant.token_ttl_seconds}s`),
      el("td", {}, status),
    );
    const actions = el("td");
    if (status === "active") {
      const revoke = el("button", { class: "btn btn-outline-danger btn-sm" }, "revoke");
      revoke.onclick = async () => {
        if (!confirm(`Revoke agent grant "${grant.name}" and every child token?`)) return;
        await api(`/api/agent-grants/${grant.id}/revoke`, { method: "POST" });
        await Promise.all([refreshGrants(), refreshTokens()]);
      };
      actions.append(revoke);
    }
    tr.append(actions);
    tbody.append(tr);
  }
}

async function refreshTokens() {
  const toks = await api("/api/tokens");
  const tbody = document.querySelector("#tokens-table tbody");
  tbody.innerHTML = "";
  for (const t of toks) {
    const tr = el("tr");
    const status = t.revoked_at ? "revoked" : (t.expires_at && new Date(t.expires_at) < new Date() ? "expired" : "active");
    tr.append(el("td", {}, t.name), el("td", {}, (t.allowed_models || []).join(", ")), usageCell(t),
      el("td", {}, status));
    const td = el("td");
    const usageBtn = el("button", { class: "btn btn-outline-secondary btn-sm me-1" }, "usage");
    usageBtn.onclick = () => showUsage(t);
    td.append(usageBtn);
    if (status === "active") {
      const btn = el("button", { class: "btn btn-outline-danger btn-sm" }, "revoke");
      btn.onclick = async () => {
        if (!confirm(`Revoke token "${t.name}"?`)) return;
        await api(`/api/tokens/${t.id}/revoke`, { method: "POST" });
        await refreshTokens();
      };
      td.append(btn);
    }
    tr.append(td);
    tbody.append(tr);
  }
}

async function showUsage(t) {
  document.getElementById("usage-token-name").textContent = `— ${t.name}`;
  const entries = await api(`/api/usage?token_id=${encodeURIComponent(t.id)}`);
  const tbody = document.querySelector("#usage-table tbody");
  tbody.innerHTML = "";
  for (const e of entries) {
    const tr = el("tr");
    tr.append(
      el("td", {}, new Date(e.created_at).toLocaleString()),
      el("td", {}, e.model),
      el("td", {}, String(e.prompt_tokens)),
      el("td", {}, String(e.completion_tokens)),
      el("td", {}, e.streamed ? "yes" : "no"),
      el("td", {}, e.status),
    );
    tbody.append(tr);
  }
}

document.getElementById("credential-form").onsubmit = async (ev) => {
  ev.preventDefault();
  const data = Object.fromEntries(new FormData(ev.target));
  await api("/api/credentials", { method: "POST", body: JSON.stringify(data) });
  ev.target.reset();
  await refreshCredentials();
};

document.getElementById("grant-form").onsubmit = async (ev) => {
  ev.preventDefault();
  const form = ev.target;
  const optionalNumber = (name) => form[name].value ? Number(form[name].value) : null;
  const body = {
    name: form.name.value,
    credential_ids: Array.from(form.credential_ids.selectedOptions).map((o) => o.value),
    allowed_models: Array.from(form.allowed_models.selectedOptions).map((o) => o.value),
    token_ttl_seconds: Number(form.token_ttl_seconds.value),
    max_active_per_instance: Number(form.max_active_per_instance.value),
    grant_max_total_tokens: optionalNumber("grant_max_total_tokens"),
    grant_max_requests: optionalNumber("grant_max_requests"),
    per_token_max_total_tokens: optionalNumber("per_token_max_total_tokens"),
    per_token_max_requests: optionalNumber("per_token_max_requests"),
    rate_limit_rpm: optionalNumber("rate_limit_rpm"),
  };
  await api("/api/agent-grants", { method: "POST", body: JSON.stringify(body) });
  form.reset();
  form.token_ttl_seconds.value = "28800";
  form.max_active_per_instance.value = "1";
  await refreshGrants();
};

document.getElementById("token-form").onsubmit = async (ev) => {
  ev.preventDefault();
  const form = ev.target;
  const body = {
    name: form.name.value,
    allowed_models: form.allowed_models.value.split(",").map((s) => s.trim()).filter(Boolean),
    credential_ids: Array.from(form.credential_ids.selectedOptions).map((o) => o.value),
    max_total_tokens: Number(form.max_total_tokens.value || 0),
    rate_limit_rpm: Number(form.rate_limit_rpm.value || 0),
    expires_in_days: Number(form.expires_in_days.value || 0),
  };
  const minted = await api("/api/tokens", { method: "POST", body: JSON.stringify(body) });
  const box = document.getElementById("mint-result");
  box.classList.remove("d-none");
  box.innerHTML = "";
  box.append(
    el("div", {}, "Token minted — copy it now, it will not be shown again:"),
    el("code", {}, minted.token),
  );
  form.reset();
  await refreshTokens();
};

async function refreshAll() {
  await Promise.all([refreshMe(), refreshCredentials(), refreshGrantModels(), refreshGrants(), refreshTokens()]);
}
refreshAll().catch((err) => console.error(err));
