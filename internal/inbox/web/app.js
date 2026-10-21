"use strict";
const $ = (id) => document.getElementById(id);
let accessKey = "",
  settings = {},
  actions = [],
  current = null,
  busy = false,
  pendingProposal = null,
  noticeTimer;
const labels = {
  drafting: "Draft interrupted / in progress",
  draft_failed: "Draft failed",
  proposed: "Needs review",
  approved: "Approved",
  executing: "Check execution outcome",
  uncertain: "Needs reconciliation",
  succeeded: "Completed",
  rejected: "Rejected",
};
function element(tag, text, cls) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (cls) node.className = cls;
  return node;
}
function notify(message, error = false) {
  $("notice").textContent = message;
  $("notice").className = error ? "error" : "";
  $("notice").hidden = false;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => ($("notice").hidden = true), 12000);
}
async function api(path, body) {
  const response = await fetch("/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Authorization: "Bearer " + accessKey,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || "Request failed.");
  return result;
}
async function run(work) {
  if (busy) return;
  busy = true;
  document.querySelectorAll("button").forEach((b) => (b.disabled = true));
  try {
    await work();
  } catch (error) {
    notify(error.message, true);
  } finally {
    busy = false;
    document.querySelectorAll("button").forEach((b) => (b.disabled = false));
  }
}
function badge(status) {
  return element("span", labels[status] || status, "status " + status);
}
function date(ms) {
  return new Date(ms).toLocaleString();
}
function renderList() {
  const list = $("action-list");
  list.replaceChildren();
  const filter = $("filter").value;
  const visible = actions.filter(
    (a) =>
      filter === "all" ||
      a.status === filter ||
      (filter === "uncertain" && a.status === "executing"),
  );
  $("count").textContent = String(actions.length);
  if (!visible.length)
    list.append(
      element(
        "p",
        "No actions in this view. Create a proposal to begin.",
        "muted",
      ),
    );
  for (const a of visible) {
    const button = element(
      "button",
      undefined,
      "action" + (current?.id === a.id ? " selected" : ""),
    );
    button.append(
      badge(a.status),
      element("strong", a.title || "Bug report draft"),
      element("small", date(a.created_ms)),
    );
    button.addEventListener("click", () =>
      run(async () => {
        current = await api("actions/" + a.id);
        renderList();
        renderReview();
      }),
    );
    list.append(button);
  }
}
function field(label, id, value, multiline = false) {
  const fragment = document.createDocumentFragment();
  const caption = element("label", label);
  caption.htmlFor = id;
  const input = element(multiline ? "textarea" : "input");
  input.id = id;
  input.value = value;
  if (multiline) input.rows = 10;
  input.maxLength = multiline ? 12000 : 180;
  fragment.append(caption, input);
  return fragment;
}
function addButton(parent, title, operation, cls = "secondary") {
  const button = element("button", title, cls);
  button.addEventListener("click", () =>
    run(async () => {
      if (
        operation === "execute" &&
        ($("issue-title").value !== current.title ||
          $("issue-body").value !== current.body)
      )
        throw new Error(
          "Save your edits and approve the revised draft before creating the issue.",
        );
      const decision = {
        version: current.version,
        note: $("review-note")?.value || "",
      };
      if (operation === "edit" || operation === "approve") {
        decision.title = $("issue-title").value;
        decision.body = $("issue-body").value;
      }
      if (operation === "reconcile") {
        decision.issue_number = Number($("existing-issue").value);
        if (
          !Number.isSafeInteger(decision.issue_number) ||
          decision.issue_number < 1
        )
          throw new Error("Enter the GitHub issue number.");
      }
      current = await api("actions/" + current.id + "/" + operation, decision);
      await refresh();
      notify(
        operation === "execute" && current.status === "succeeded"
          ? "GitHub confirmed the new issue."
          : "Action saved.",
      );
    }),
  );
  parent.append(button);
}
function renderReview() {
  const root = $("review");
  root.replaceChildren();
  if (!current) {
    const empty = element("div", undefined, "empty");
    empty.append(
      element("h2", "Your review starts here"),
      element(
        "p",
        "Select an action to inspect its report, proposed issue, and history.",
      ),
    );
    root.append(empty);
    return;
  }
  const a = current;
  const heading = element("div", undefined, "review-heading");
  const meta = element("div");
  meta.append(
    element("h2", "Review issue"),
    element("p", a.repository + " · version " + a.version),
    element(
      "p",
      "Draft source: " + a.provider + (a.model ? " / " + a.model : ""),
    ),
  );
  heading.append(meta, badge(a.status));
  root.append(heading);
  const report = element("details");
  report.append(
    element("summary", "Original bug report"),
    element("p", a.report, "report"),
  );
  root.append(report);
  const editable = ["proposed", "approved"].includes(a.status);
  if (a.title) {
    root.append(
      field("Issue title", "issue-title", a.title),
      field("Issue body", "issue-body", a.body, true),
    );
    $("issue-title").readOnly = !editable;
    $("issue-body").readOnly = !editable;
    if (a.rationale)
      root.append(
        element("p", "Review guidance: " + a.rationale, "muted small"),
      );
  }
  if (
    editable ||
    ["drafting", "draft_failed", "uncertain", "executing"].includes(a.status)
  ) {
    root.append(field("Review note (optional)", "review-note", ""));
    $("review-note").maxLength = 2000;
  }
  const buttons = element("div", undefined, "buttons");
  if (editable) {
    addButton(buttons, "Save edits", "edit");
    if (a.status === "proposed")
      addButton(buttons, "Approve draft", "approve", "");
    addButton(buttons, "Reject", "reject", "danger");
  }
  if (["drafting", "draft_failed"].includes(a.status))
    addButton(buttons, "Reject this draft", "reject", "danger");
  root.append(buttons);
  if (a.status === "approved") {
    const box = element("div", undefined, "notice-box");
    box.append(
      element(
        "p",
        "Approved content will be published to " +
          a.repository +
          ". Save any further edits and approve them again before execution.",
      ),
    );
    if (settings.execution_enabled) {
      addButton(box, "Create GitHub issue", "execute", "");
    } else {
      box.append(
        element("p", "Execution is disabled in server settings.", "muted"),
      );
    }
    root.append(box);
  }
  if (["uncertain", "executing"].includes(a.status)) {
    const box = element("div", undefined, "notice-box");
    box.append(
      element(
        "p",
        "This attempt cannot be retried automatically. Check GitHub for the issue and reconcile it here. Creating a replacement could produce a duplicate.",
      ),
    );
    const link = element("a", "Open target repository");
    link.href = "https://github.com/" + a.repository + "/issues";
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    box.append(link, field("Existing issue number", "existing-issue", ""));
    $("review")?.append(box);
    const number = box.querySelector("input");
    number.type = "number";
    number.min = "1";
    addButton(box, "Verify existing issue", "reconcile");
  }
  if (a.issue_url) {
    const link = element("a", "Open GitHub issue #" + a.issue_number);
    link.href =
      "https://github.com/" + a.repository + "/issues/" + a.issue_number;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    root.append(link);
  }
  const history = element("details");
  history.open = true;
  history.append(element("summary", "Decision history"));
  const timeline = element("ol", undefined, "timeline");
  for (const e of a.events) {
    const li = element("li");
    li.append(
      element("strong", e.kind.replaceAll("_", " ")),
      element("small", e.actor + " · " + new Date(e.at).toLocaleString()),
      element("p", e.note || "No note added."),
    );
    if (e.draft) {
      const revision = element("details");
      revision.append(
        element("summary", "Saved revision"),
        element("strong", e.draft.title),
        element("p", e.draft.body, "report"),
      );
      li.append(revision);
    }
    timeline.append(li);
  }
  history.append(timeline);
  root.append(history);
}
function renderSettings() {
  const list = $("connection-list");
  list.replaceChildren();
  const rows = [
    ["Database", settings.database + " · " + settings.database_status],
    ["Namespace", settings.namespace],
    ["AI provider", settings.provider],
    ["Model", settings.model || "Not configured"],
    ["AI request ceiling", String(settings.ai_request_limit) + " attempts (including failures)"],
    [
      "AI credentials",
      settings.ai_configured
        ? "Configured (test by drafting)"
        : "Missing key or model",
    ],
    ["GitHub repository", settings.repository],
    [
      "GitHub credentials",
      settings.github_configured ? "Configured" : "Not configured",
    ],
    [
      "Execution",
      settings.execution_enabled ? "Enabled after human approval" : "Disabled",
    ],
  ];
  for (const [key, value] of rows)
    list.append(element("dt", key), element("dd", value));
  $("submit-proposal").textContent = $("manual").checked
    ? "Submit for review"
    : "Draft with " + (settings.provider === "claude" ? "Claude" : "OpenAI");
  $("draft-destination").textContent = "Destination: " + settings.repository;
}
async function refresh() {
  [settings, actions] = await Promise.all([api("settings"), api("actions")]);
  if (current) current = await api("actions/" + current.id);
  renderSettings();
  renderList();
  renderReview();
}
$("login-form").addEventListener("submit", (event) => {
  event.preventDefault();
  run(async () => {
    accessKey = $("access-key").value;
    await refresh();
    $("access-key").value = "";
    $("login").hidden = true;
    $("workspace").hidden = false;
  });
});
$("logout").addEventListener("click", () => {
  accessKey = "";
  actions = [];
  current = null;
  pendingProposal = null;
  settings = {};
  $("proposal-form").reset();
  $("new-proposal").open = false;
  $("filter").value = "all";
  $("connection-list").replaceChildren();
  $("notice").hidden = true;
  clearTimeout(noticeTimer);
  tab(false);
  $("workspace").hidden = true;
  $("login").hidden = false;
  $("action-list").replaceChildren();
  $("review").replaceChildren();
  $("access-key").focus();
});
$("refresh").addEventListener("click", () => run(refresh));
$("filter").addEventListener("change", renderList);
function tab(settingsVisible) {
  $("settings").hidden = !settingsVisible;
  $("inbox-view").hidden = settingsVisible;
  $("settings-tab").className = settingsVisible ? "" : "secondary";
  $("inbox-tab").className = settingsVisible ? "secondary" : "";
  $("settings-tab").setAttribute(
    "aria-current",
    settingsVisible ? "page" : "false",
  );
  $("inbox-tab").setAttribute(
    "aria-current",
    settingsVisible ? "false" : "page",
  );
}
$("settings-tab").addEventListener("click", () => tab(true));
$("inbox-tab").addEventListener("click", () => tab(false));
$("manual").addEventListener("change", renderSettings);
$("example").addEventListener("click", () => {
  $("report").value =
    "Synthetic demo report: In our staging app, clicking Export CSV while a date filter is active downloads all records. I expected only the filtered records. Reproduced twice in Chrome on a sample dataset. No production or customer data is included. The affected build and exact steps still need confirmation.";
  pendingProposal = null;
});
$("proposal-form").addEventListener("input", () => {
  pendingProposal = null;
});
$("proposal-form").addEventListener("submit", (event) => {
  event.preventDefault();
  run(async () => {
    if (!pendingProposal) {
      pendingProposal = {
        request_id: crypto.randomUUID(),
        report: $("report").value,
      };
      if ($("manual").checked)
        pendingProposal.draft = {
          title: $("manual-title").value,
          body: $("manual-body").value,
          rationale: "Submitted draft; verify every claim before approval.",
        };
    }
    notify(
      $("manual").checked
        ? "Saving proposal…"
        : "Drafting with " + settings.provider + "…",
    );
    current = await api("actions", pendingProposal);
    pendingProposal = null;
    await refresh();
    $("new-proposal").open = false;
    notify(
      current.status === "draft_failed"
        ? "Draft failed. See the decision history for details."
        : "Proposal saved for review.",
      current.status === "draft_failed",
    );
  });
});
