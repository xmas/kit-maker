# Kit Maker — Design

Why Kit Maker is shaped the way it is. What exists is in [CLAUDE.md](CLAUDE.md) and the code; open
work, plans and decisions still to make are in beads (`bd ready`, `bd show <id>`). Decisions already
made are `decision` beads (`bd list --type decision --all`). Section numbers are stable because beads
cite them.

Kit Maker is a web app for building **blueprints**, the designs for **Docker Sandboxes Kit v3**
kits, by stating a goal and filling in the blanks, with AI drafting, explaining and reviewing
every step. Blueprints are composed from reusable, versioned, inheritable blocks; each published
version is an OCI kit image that people download or reference from a repo's `sbxenv.yaml`. Every
version carries its **blueprint ID**, so we can trace where a sandbox came from, who made and
downloaded it, which project it served and what network access it used (§2, §5).

Stack: **Go** (one static binary) · Vue 3 + TypeScript + Inertia (via
`gonertia`) · Tailwind + shadcn-vue · Postgres + pgvector · Ent + Atlas
(models and versioned migrations) · River (Postgres-backed jobs) · Claude via
the official Go SDK (`anthropic-sdk-go`) · the kit spec's own Go module
(`github.com/docker/sandbox-kit-spec/v3`) for validation, merge, permission
surface and assembly. Shipped as a distroless image with a Helm chart (AWS,
Azure, OpenShift, any Kubernetes) and a Docker Compose stack (a single
computer). See §11.

Spec source of truth: [`docker/sandbox-kit-spec`](https://github.com/docker/sandbox-kit-spec)
(`docs/spec/SPEC-v3.md`, `schema/kit.schema.json`, `schema/capabilities/**`,
and the repo's `sbxenv.yaml` for the environment format). The spec is
experimental, with the final version targeted for Q4 2026, so the schema is
vendored and pinned.

---

## 1. What the app produces: a blueprint, referenced from code

The blueprint lives **in the cloud**. Repos never hold the blueprint itself. They hold a
small **code-level reference** that points at the blueprint, either at a
moving channel (current) or pinned to one revision.

| Output | Where | Changes when |
|---|---|---|
| **Blueprint version**: each publish is an OCI kit image (v3 kits are ordinary OCI images) | `docker.io/acme/sbx-kit-acme-shop` with tags `vN` (immutable), `latest` (newest version) and `stable` (promoted version), all with digests | every time you publish |
| **Code-level reference**: a small `sbxenv.yaml` in the repo | `acme/shop/sbxenv.yaml`, added once by PR | rarely: switching follow/pin |
| **SANDBOX.md** (optional) | in the repo, written by AI | with the reference |
| **Offline bundle** (fallback) | zip of the descriptor + files + sbxenv | on demand |

The code-level reference:

```yaml
# sbxenv.yaml: managed by Kit Maker (bp_01JAC2W8F3). Edit the blueprint at kitmaker.app/k/acme-shop
schemaVersion: "1"
name: acme-shop
agent: sbx-kit-acme-shop
kits:
  - source: docker.io/acme/sbx-kit-acme-shop:stable   # follow: stable channel
  # pinned instead:   docker.io/acme/sbx-kit-acme-shop:v2
  # exact:            docker.io/acme/sbx-kit-acme-shop@sha256:7d02…c3e1
workspace: .
sandboxOptions:
  pullPolicy: always        # re-resolve the tag on every create
bindings:                   # optional convenience; merged into the user's global credentials.yaml (§1.1)
  github:    { apiKey: { domains: [api.github.com, github.com] } }
  packagist: { apiKey: { domains: [repo.packagist.com] } }
lifecycle:
  postCreate:
    - name: Register with Kit Maker   # §5, opt-in
      command: …
```

- **Following vs pinning.** `:stable` or `:latest` means each new sandbox gets
  the current blueprint. `pullPolicy: always` re-resolves the tag, and the
  sbx runtime's update gate (spec §7.4) asks for approval whenever a new version
  widens permissions. `:vN` or `@sha256:` pins the version until someone
  changes the reference.
- **Channels.** `latest` moves on every generate. `stable` moves only when
  someone **promotes** a rev, after its checks pass. Teams point repos at
  `stable`.
- **Content split.** Everything about *the blueprint* (agent, toolchain, network,
  credentials, setup, context) lives in the blueprint. Things about *the repo
  or host* stay in the reference: workspace, env overrides, the registration
  hook and, optionally, bindings (§1.1). The
  blueprint is compiled as a `kind: set`, so one reference carries the agent
  workload and every mixin (publishing a set merges them into one kit).
- **Reference tracking.** The app knows which repos point at which blueprint, and at
  what channel or pin. It learns this from a GitHub App scan of
  `sbxenv.yaml`, and from the registration hook, which reports the reference
  and the digest it resolved to. That powers "acme/admin is pinned to rev 1,
  2 revs behind" and one-click "Open PR to update the pin".
- **New credentials don't need a repo PR** when someone is at the keyboard
  (§1.1). The app warns about the non-interactive gap before a rev that adds
  a credential is promoted to `stable`.
- **Publishing.** A blueprint with no Dockerfile content is a staged
  descriptor plus file layers (a content-free kit is one layer), so a queue
  worker assembles and pushes it through the OCI distribution API. Blueprints with
  Dockerfile content go to a build runner. The registry and the build runner
  are both **set per deployment** (§1.2).

### 1.1 Credential bindings: what sbx supports today, and the gap

Confirmed by experiment against **sbx v0.48.0, locally**, on 2026-10-16. We
used a throwaway v3 mixin that requires `credential@1` for a dummy service
(`kmtest`, proxy-managed, injected as a header on echo hosts), on top of the
published `docker/sbx-kit-shell` workload, from an `sbxenv.yaml` with no
`bindings:` block. Every outcome below was checked inside the sandbox by
reading the echoed request headers. Background docs:
[credentials](https://docs.docker.com/ai/sandboxes/configuration/credentials/),
[environment files](https://docs.docker.com/ai/sandboxes/configuration/environment-files/),
[`sbx env rm`](https://docs.docker.com/reference/cli/sbx/env/rm/).

- **Bindings are per user, not per repo.** They live in
  `~/.config/sbx/credentials.yaml` as `bindings.<service>.<apiKey|oauth>.domains`.
  The secret goes into the OS store under the service name (`sbx secret ls`
  shows it as a global `service` secret). Any service name works, and
  `printf … | sbx secret set kmtest` stores one without a prompt. An env
  file's `bindings:` block merges into the global file. `sbx env rm` keeps
  the bindings unless you pass `--prune-bindings`.
- **An interactive `sbx env run` prompts. Confirmed.** `sbx env plan` and
  the plan approval do **not** list a blueprint's credentials. The prompt comes
  after the plan is approved and the blueprint is built:
  *"This kit wants to use these credentials: kmtest: API key sent to
  httpbin.org (not set)"*, with the options **Approve all / Review each /
  No, start without them**. No is the default. "Review each" offers
  *"Enter it now (saved to your secret store as "kmtest")"* or *Skip*. It
  then asks *"Allow this API key's secret value to be sent to these
  domains? (y/N)"*. Approving writes `kmtest: apiKey: domains:
  [httpbin.org]` to `credentials.yaml` and stores the secret globally.
  Inside the sandbox, `KMTEST_TOKEN=proxy-managed` and the proxy adds the
  real header. "Approve all" with no stored secret does **not** ask for
  the value. It creates the sandbox without one and only warns.
- **Widening re-prompts, but only interactively.** When a new blueprint version adds
  an inject domain that the binding lacks, an interactive run prompts again.
  The prompt lists all of the domains, not just the new one. Approve all
  adds the new domain to `credentials.yaml`. When the binding already covers
  every domain, there is no prompt. So a blueprint that adds Packagist
  prompts each developer once on their next create, with **no repo
  change**.
- **The user-level file works.** A `bindings:` block in `~/.sbxenv.yaml` is
  honoured. The plan lists both files and a `BINDINGS + kmtest` row.
  Applying it prints *"Binding saved to ~/.config/sbx/credentials.yaml"*,
  even under `--force --detach` with no TTY. **The catch:** the user file
  is merged only when `sbx env` runs **without a PATH**. `sbx env run .`
  or `sbx env run ./project` skips it.

The gaps:

1. **Non-interactive runs never prompt, never fail, and exit 0.** Both
   `--detach` (even with a TTY) and `--force` with no TTY skip the
   credential prompt. The plan prompt still shows with `--detach` alone.
   We tested the combinations:

   | binding | secret | result |
   |---|---|---|
   | none | stored | created, header **not** sent. *"WARN: required credential has no binding; sandbox will start without it"*, then *"Note: no binding authorizes kmtest … Create a binding (re-run interactively, or edit ~/.config/sbx/credentials.yaml)"* |
   | none | none | created, not sent. The same warnings, plus *"required credential not found for service"* |
   | present | none | created, not sent. *"WARN: no credential resolved for required service; sandbox will start without it"* |
   | present | stored | created, header sent, no warnings. **This is the CI recipe.** |

   The env var shows the sentinel `proxy-managed` in **every** case, so kit
   content can't tell whether the credential is wired. A setup hook that
   needs it fails later, in an unclear way. The CI workaround is confirmed:
   pre-write the binding (in `credentials.yaml`, or in a runner-level
   `~/.sbxenv.yaml` with `sbx env run` and no PATH), and store the secret
   with `sbx secret set <service>` from stdin. Kit Maker generates both
   snippets per blueprint and per CI provider.
2. **Local sbx doesn't enforce the binding's domains.** This is new, and
   it's the serious one. A binding gates the *service*, not *where it is
   sent*. The binding listed only `httpbin.org`, and the blueprint's
   `inject` list added `postman-echo.com` and `httpbingo.org`. The secret
   reached **all three** hosts in two cases:
   - a non-interactive create, with **no warning at all**, and with a host
     that had never been approved anywhere;
   - an interactive create where the user answered **"No, start without
     them"**. `credentials.yaml` stayed as it was, and the secret was
     still sent to all three.

   So a new blueprint version can widen where a developer's secret goes, and the sbx
   prompt doesn't protect them in CI or when they decline. Kit Maker's own
   gate has to treat **a new inject domain on an existing credential** as a
   credential change. The rev review shows it, and the AI reviewer
   escalates it (§14). Report it upstream as a bug, next to the ask in gap
   3.
3. **No org-level approval.** Nothing in the CLI offers a central or
   policy-level binding. There is no way to say "blueprints from
   `docker.io/acme/*` may use `github` and `packagist` on these domains"
   once for the whole org. `~/.sbxenv.yaml` gets as far as per-person and
   all-repos, but only for no-PATH `sbx env` runs. The ask for the sbx
   team: publisher- or org-scoped binding policy, ideally alongside the
   existing org governance.
4. **Spec vs CLI mismatch. Confirmed.** `credential@1` says that a required
   entry with no binding **MUST fail resolution**. sbx 0.48.0 warns and
   creates (exit 0) in every case above, interactive or not. Ask the sbx
   team for a strict mode (`--require-credentials`, or a setting) so that
   CI fails fast. Until then, a Kit Maker `postCreate` hook can check the
   proxy for real (call an inject host and check the response), since the
   env var can't be trusted.
5. **Cloud: not tested.** This account has no Docker Agentic Platform
   subscription, so `sbx --cloud env plan` returns *"Cloud sandboxes require
   a Docker Agentic Platform subscription"*. Per `sbx env --help`
   (0.48.0), cloud supports bindings only for *"supported providers"*. It
   rejects *"custom credential providers"*. Its bindings merge into the same
   `credentials.yaml`. The cloud must advertise kit-credential support.
   *"Third-party kit domains must be approved by the binding"*, which is
   stricter than local. Bindings and secrets are fixed at creation. So a
   custom service like `packagist` may not work in cloud at all. Re-test
   with an entitled account.

Plan: `bindings:` in the repo reference becomes **optional**. It's off by
default and only offered for repos that run sandboxes in CI, and there the
CI snippet (binding + `sbx secret set`) is the main tool. Credential
*additions* don't block promotion: the rev review shows "first create will
prompt for `packagist`" and "CI runners need this snippet". Credential
*domain widening* is gated by Kit Maker itself, because sbx doesn't gate
it (gap 2).

### 1.2 Deployment model: self-hosted per customer

Kit Maker is **deployed by each customer** into their own cloud (§11). One
deployment is one organisation, with teams inside it. Everything
that touches the customer's infrastructure is set per deployment (env +
admin settings), behind drivers:

| Setting | Drivers | Notes |
|---|---|---|
| **Registry** | Docker Hub org, GHCR, ECR, GAR, Harbor, any OCI distribution registry | Push credentials stored encrypted. Repository naming template, e.g. `{org}/sbx-kit-{slug}`. **sbx only pulls remote blueprints from sources in `kit.allowedSources`. Docker Hub is allowed by default**, so any other registry has to be added on developer machines or by org policy. The admin page shows the exact setting to roll out. |
| **Build runner** (blueprints with Dockerfile content) | `none` (content-free kits only), Docker Build Cloud, GitHub Actions (workflow in a repo the deployment controls), self-hosted BuildKit endpoint | The in-app assembler always handles content-free kits. Builds run on a queue, report status back by webhook, and record the digest. |
| **Git host** | GitHub App (scan references, open PRs), GitLab later | Needed for reference tracking and "Open PR". |
| **AI** | Anthropic API, Amazon Bedrock, Google Vertex AI, or `fake` (recorded fixtures, for tests). Claude on Microsoft Foundry comes once the Go SDK supports it; until then Azure deployments use the Anthropic API | Model is `claude-opus-5-5`, with effort per route. Budgets are set per deployment. The cloud drivers keep AI traffic inside the customer's tenancy. |
| **Identity** | Any mix of username/password, Microsoft Entra ID, Okta, Google, and generic OIDC, chosen per deployment | See §12. Team membership sources are in §13 and RBAC in §14. |
| **Object storage** | S3-compatible (S3, MinIO, ODF/NooBaa, GCS interop) or Azure Blob | Holds bundles, build contexts and context files. |
| **Registration endpoint** | The deployment's own URL | The hostname written into `postCreate` hooks. |

Consequences: no multi-tenant isolation is needed between customers.
Settings are validated on save ("push a test manifest", "dispatch a no-op
build"). Packaging and per-cloud mapping are in §11.

---

## 2. Core concepts

```
Team ──< Project ──> Blueprint ──< Slot ──> Block ──< BlockVersion
            │            │  ╰─ Goal ──< Check     (blank / suggested / filled)
            │            │ publish
            │            ▼
            │       BlueprintVersion (vN, digest; packed as an OCI image)
            │            │ download · sbx create
            └────────< Sandbox (sbx_…) ──< Activity (network hosts, counts)
```

**Words** (decision `kit-maker-hcw`). The app builds **blueprints**. A blueprint
is the design for a Docker Sandboxes kit: "kit" is the spec's name for the
published image, and the app says so once (docs, onboarding, the empty state).
Everywhere else (nav, titles, buttons, logs) it says only "Blueprint":
"Blueprint v3", "Download blueprint", "Sandboxes from this blueprint". "Kit"
appears only inside the YAML and commands we generate for sbx (`kits:` in
`sbxenv.yaml`, `sbx kit`). The workload is a **Project**, not a "workload",
because the spec already has `kind: workload`.

- **Team**: owns projects, blueprints and blocks (§13).
- **Project**: the workload: what a team is doing, its repos (a repo's
  `sbxenv.yaml` is the project's pointer, §1) and the **one** blueprint it
  runs, on a channel or pinned to a version. One blueprint per project (it
  may include others, §2.5) keeps attribution and right-sizing to one target.
  Sandboxes register against the project, which is what answers "what work
  was this for" (§5.1).
- **Blueprint**: what people edit: a goal, slots filled by blocks, and full
  history (§2.5a). Stable ID `bp_<ULID>`.
- **Blueprint version**: one publish, immutable, numbered `vN`, with a digest,
  permission surface and **who authored each part** (human, or AI accepted
  by a named human). Downloaded either as a reference for a repo or as a file
  (§1); both are logged.
- **Goal**: the blueprint's purpose in plain language, plus optional sources (a
  GitHub repo, an existing blueprint, a Dockerfile, an sbx-scan report).
- **Check**: one testable expectation derived from the goal. Check types:
  - `network`: a URL plus expectation (pass or blocked), evaluated against the
    merged policy, as in sbx-scan's table.
  - `credential`: a service is available at a phase.
  - `tool`: a binary or `provides` capability is present.
  - `setup`: a command runs on create (e.g. `composer install`).
  - `agent`: the agent and its context or skills are present.
  - `manual`: needs a human to confirm.

  Each check has a status (passing, failing, or unknown) and links to the
  slots that satisfy it.
- **Slot**: a place in the blueprint outline: "Agent", "Base image", "Network:
  internal services", "GitHub credential", "Setup: install deps", and so on.
  A slot is **blank** (required, empty), **suggested** (an AI proposal waiting
  for review), **filled** (bound to a block version), or **optional**. Slots
  come from the template for the blueprint's kind plus whatever the goal implies.
- **Block / BlockVersion**: reusable, immutable-versioned pieces of a
  blueprint: *"Internal network policy"*, *"GitHub API credential"*.
- **Sandbox**: a running instance created from a blueprint version, registered
  on create with its project, person and host (§5).
- **Activity**: what a sandbox did, starting with the network hosts it reached
  or was refused (§5.1).

### 2.1 Block types → spec fields

| Group | Block types | Renders into |
|---|---|---|
| Metadata | `display-name`, `author`, `description`, `source-url`, `icon-url`, `version`, `license-set` | top-level scalars |
| Dependencies | `provides`, `requires`, `integrates`, `conflicts` | §5 lists |
| Content | `dockerfile`, `build`, `kit-refs` | `dockerfile:` / `build:` / `kits:` |
| Args | `arg` | `args.<name>` |
| Network | `network-rules` | **merged** into one `network-policy@2` |
| Credentials | `credential` | `credential@1` |
| Lifecycle | `install-hook`, `startup-hook`, `file`, `interactive` | **merged** into one `lifecycle@1` |
| Storage | `volume`, `host-mount` | `volume@1`, `host-mount@1` |
| Runtime | `resources`, `port`, `ssh-agent`, `git-identity`, `privileged`, `long-running`, `usb-device` | matching capabilities |
| Agent | `agent-context`, `agent-skill`, `agent-skills`, `acp`, `agent-sessions` | matching capabilities |
| Escape hatch | `raw-capability` | verbatim capability entry |
| Environment | `env-kit-ref`, `workspace`, `env-vars`, `binding`, `sandbox-options`, `post-create-hook`, `network-preset` | `sbxenv.yaml` keys |

Every field is editable: typed forms cover the common types, `raw-capability`
covers everything else, and every block has a YAML view.

### 2.2 Block inheritance: link, edit locally, fork

A block placed in a blueprint is a **link**, not a copy:
`{block, track: latest | pinned vN, overrides}`. The blueprint uses
**base ⊕ overrides**.

- **Edit in the blueprint, keep the link.** Editing a linked block records the
  change as an override on the link. The library block is untouched, and the
  blueprint still knows where the block came from. In the UI every row or field
  shows where it comes from: *inherited*, *changed here*, *added here* or
  *removed here*.
- **Upstream changes keep flowing in.** When the library block publishes v4,
  each linked blueprint **rebases** its overrides onto v4 (a 3-way merge of old
  base, new base and overrides). It applies automatically when the merge is
  clean, the blueprint follows latest, and the surface doesn't widen. Otherwise it
  waits as "v4 available" with a preview. A **conflict** (an override touches
  something v4 also changed or removed) gets an AI-proposed resolution that
  you accept or edit.
- **Revert** drops the overrides and goes back to pure library.
- **Fork** promotes base + overrides into a *new library block* that remembers
  its parent. A fork is itself a link to the parent plus a patch, so it keeps
  inheriting the parent's updates. Chains are capped at depth 3.
- **Propose upstream** turns this blueprint's overrides into a draft vN+1 of the
  original. It goes through the where-used review: AI checks every blueprint that
  follows the block.
- **Patch format.** List items in a payload (network rules, hooks, files,
  inject entries) carry stable item keys (`_k`, stripped at compile). Override
  ops are `set`, `unset`, `add(after?)`, `remove(key)` and `replace(key)`, so
  overrides survive reordering and can be merged meaningfully rather than
  line by line.
- **Snapshots.** A blueprint records base version + overrides hash per block,
  so a rev is exactly reproducible. YAML comments read
  `# block: github-egress v3 (+2 edits here)`.

### 2.3 Visual language for blocks

- **Type** = colour + icon, used everywhere a block appears (outline, cards,
  library, diffs, lineage): Network (teal, globe), Credential (amber, key),
  Setup (slate, terminal), Toolchain (blue, box), Storage (orange, disk),
  Runtime (grey, cpu), Agent context (rose, doc), Metadata (neutral, tag),
  Kit ref (indigo, layers).
- **Link state** = a badge, separate from type: *Linked vN* (chain), *Edited
  here · N* (chain + pencil), *Update available* (arrow), *Conflict* (red),
  *Local* (no chain), *Forked from X* (branch).
- **AI** is the only violet in the app, so an AI suggestion never looks like a
  block type.
- **Origin** = a small marker on every outline item and every field (§2.6).

### 2.4 Singleton merging

`network-policy@2`, `lifecycle@1` and `resources@1` may appear only once per
descriptor. Separate `network-rules` blocks are merged (allow and deny lists
unioned per phase, each rule commented with its source block). Lifecycle
hooks are concatenated in blueprint order. Instance-shaped types emit one entry per
block, and the spec's dedup keys are checked at compile time.

### 2.5 Included blueprints

A blueprint can **include other blueprints**. For example, `acme-base` holds the team's
deny rules, GitHub access and AGENTS.md, and `acme-shop` and `acme-admin`
both include it. An include is `{child blueprint, track: stable | latest | pinned
rev}`, just like a block link one level up.

- The included blueprint's slots appear in the parent's outline as **From acme-base**.
- The parent can **override** any of them. It can bind a different block
  (replace) or keep edits on top of the included blueprint's value (patch). Either way
  the item shows **Overrides acme-base**, and the original stays visible for
  comparison.
- **Precedence:** the blueprint's own slots win over included blueprint slots. When two
  included blueprints both provide something that can only exist once (the same
  credential service+phase, a singleton like `resources@1`), the parent has
  to resolve it, and AI proposes how.
- **Updates:** when `acme-base` publishes a new version, every parent tracking it
  shows "update available", rebases its overrides, and re-runs its checks,
  the same as for blocks.
- **Compile = flatten.** The spec's `kind: set` composition unions
  declarations and treats duplicate owners as conflicts. It has **no
  override**, so Kit Maker resolves included blueprints and overrides before publishing
  and emits one ordinary kit. The blueprint records which included blueprint version each
  slot came from. (An included blueprint with no overrides could instead be referenced
  by image in a set. That's an optimisation for later.)

### 2.5a History: undo everything

Every change to a blueprint is recorded as a revision: who, when, a one-line
summary, and a full snapshot (goal, slots, the block versions they link and
the blueprint's edits) plus the YAML it compiled to. Any revision can be previewed
(its YAML against now) and restored, for the whole blueprint or one slot; a
restore is itself a revision, so it can be undone. Library blocks already
keep immutable versions; restoring an old one publishes it as the newest, so
blueprints take it like any update. Two words stay distinct in the UI: **Revert
changes** throws away unsaved edits; **Resync to block** drops the blueprint's
saved edits and takes the block's latest version. In the outline a pencil
means unsaved edits only; a link with an amber dot means linked with saved
edits.

### 2.6 The outline is the blueprint's navigation

Builder layout: outline on the left, the selected slot's editor in the
middle, and the blueprint's YAML on the **right**, always visible, with the lines
the selected slot produced highlighted. The YAML stays on the right so the
selection bridge runs straight from the outline into the editor. The outline
is always open; only the YAML can be hidden, and it folds first when space
runs out.

The outline from the builder is the **persistent left rail on every blueprint
screen**: builder, block editor, inheritance view, checks, generate, in code.
It does two jobs:

1. **Navigation.** Sections (Agent & base, Network, Credentials, Setup,
   Agent, Runtime, Included blueprints) and their slots are links. The selected slot
   opens in the main pane, and URLs are stable (`/blueprints/{blueprint}/slots/{slot}`).
   Filters: *All · Needs you · Changed here · From included blueprints*.
2. **State at a glance.** Each item shows its type colour, its name, and two
   markers:
   - **Origin** (where the value comes from):
     - *Inherited*: linked library block, no edits (chain)
     - *Modified*: linked block plus edits here (pencil + count)
     - *Custom*: exists only in this blueprint (filled dot)
     - *From included blueprint*: provided by an included blueprint, untouched (layers +
       included blueprint name)
     - *Overrides included blueprint*: this blueprint replaces or edits what the included blueprint
       provides (layers + pencil)
   - **Status** (what needs attention): blank, AI suggestion to review,
     update available, conflict, failing check.

The same origin markers repeat at field level inside each editor, so the
outline and the detail always agree.

**Selection links visibly to the content.** The selected row turns solid
blue, and a blue bridge crosses the gap into a blue-bordered frame around the
main pane. That frame's header repeats the selected row: type, name, origin,
status, plus the slot's tabs (Rules · Inheritance · Ask AI · History) and
actions. Filtered views (e.g. *Needs you*) light up the matching rows and use
the same blue in the frame header.

---

## 3. AI assistance (everywhere)

Principles:

1. **Propose, never apply.** Every AI output is an `ai_suggestion` with a diff
   and a short "why". Accepting it creates the block version or binds the
   slot. Rejecting it is recorded, so the next suggestion learns from it.
2. **Reuse before create.** When filling a slot, the AI searches the team's
   block library first: "Use *GitHub egress v3* (used by 18 blueprints)". It only
   drafts a new block when nothing fits.
3. **Schema-valid by construction.** Generation uses structured outputs, with
   the block type's JSON Schema (from the vendored spec schema) as the output
   format. The compiler then validates the result, and any errors go back to
   the model for up to 2 repair turns before the user sees anything.
4. **Checks close the loop.** After any change, checks are re-evaluated. The
   AI is told which checks still fail and proposes what would fix them.
5. **Never silently widen.** A suggestion that widens the permission surface
   (spec §7.4) is labelled, needs explicit accept, and is called out again at
   generate time.
6. **Provenance for AI too.** Block versions record `authored_by`
   (user / ai), `accepted_by`, model, and suggestion ID. The blueprint carries
   that summary.

Where AI shows up:

| Moment | What the AI does |
|---|---|
| **Start a blueprint** | Turns the goal and sources into kind, agent, outline, slots and checks. It reads the repo (languages, package managers, start commands, CI, like sbx-scan) and shows its evidence. |
| **Fill a blank** | Offers 1–3 options for the slot (library blocks first, or a new draft), each with what it does to the checks. |
| **Ask about a block** | An "Ask AI" bar on every slot and block, e.g. "also allow read-only GitLab". The AI returns a diff. If the block is shared it asks: fork for this blueprint, or publish a new version for everyone (with the widening count)? |
| **Field help** | Explains a field in context ("what does `proxyManaged` do here?") and autocompletes hosts, paths and env names. |
| **Network** | Turns intent into rules ("agent can read our Jira, can't post to Slack") and writes policy tests for them. Explains why a URL is allowed or blocked. |
| **Agent context** | Drafts AGENTS.md / CLAUDE.md from the repo and goal, and suggests skills. |
| **Generate a rev** | AI review of the permission-surface diff (risk notes on each widening), release notes, and a change summary for PRs. |
| **Approve** | The independent **AI reviewer** (§14) approves, requests changes, or escalates widenings and promotions. It's the default approver where there's only one Maintainer. |
| **Library hygiene** | Finds near-duplicate blocks, suggests merges, and writes block descriptions. Search accepts plain language ("something that lets the agent reach our npm mirror"). |
| **Import** | Splits a pasted kit YAML or repo into blocks, matching existing blocks by content hash or meaning. |
| **Troubleshoot** | Takes a pasted `sbx env run` or `sbx kit validate` error and suggests a fix as a diff. |

Implementation:

- `internal/ai` services wrap the official Go SDK
  (`github.com/anthropics/anthropic-sdk-go`): planner, slot filler, block
  editor, check writer, rev reviewer, explainer, library curator, importer.
  Each service sits behind an interface with a `fake` implementation that
  replays recorded fixtures, so tests are deterministic and free.
- **Model:** `claude-opus-5-5` everywhere, with effort tuned per route: `low`
  for inline explain and autocomplete, `medium` for slot fills and edits,
  `high` for goal planning and rev review. Measure before considering a
  cheaper model for the inline routes (that's our call to make later). Enable
  the server-side refusal fallback (`fallbacks: "default"`).
- **Structured outputs:** each block type's JSON Schema is the output format,
  so drafts come back schema-valid, and the compiler validates them again
  with the spec module.
- **Prompt caching:** a frozen system prefix holds the vendored spec excerpts,
  the capability schemas and the block-type catalogue, cached at a 1h TTL.
  Team library summaries are a second cached block. Per-request content goes
  after the breakpoints.
- **Streaming:** SDK stream → `http.Flusher` SSE handler → Vue, so drafts
  appear as they're written.
- **Background:** library hygiene, bulk "regenerate after a shared block
  changed" and included blueprint rebases run as River jobs. Message Batches can be used
  for big bulk runs.
- **Cost guard:** per-team monthly AI budget, usage logged per call
  (`ai_calls`). Budgets count billable tokens (input + cache writes + output,
  plus a tenth of cache reads) rather than money, so they don't depend on a
  price list per provider.

---

## 4. Compiler

`internal/blueprints/compiler` is a pure function from (blueprint, resolved links,
included blueprints, overrides) to output. It builds on the spec's own Go module
(`github.com/docker/sandbox-kit-spec/v3`). The sbx runtime imports that same
`spec` package, so Kit Maker and sbx can't disagree about what a kit means.

1. Resolve included blueprints (§2.5) and block links (pinned or latest), and apply
   overrides (`internal/blueprints/patch`).
2. Render fragments for each block type into the spec module's kit types,
   then run the singleton mergers and dedup-key checks.
3. Inject provenance (§5).
4. Validate with the spec module, which is the same check `sbx kit validate`
   runs. Editors also get field-level errors from the vendored JSON Schemas,
   checked server-side with `santhosh-tekuri/jsonschema` and client-side with
   `ajv`.
5. Emit YAML with the `# syntax=docker/sandbox-kit:3` header, the spec's key
   order, and source-block comments.
6. Compute the **permission surface** (spec §7.4) with the spec module's own
   surface code, and diff it against the previous blueprint.
7. **Evaluate checks**: network checks run against the merged policy plus the
   chosen base preset; tool, credential and setup checks run against the
   compiled declarations.
8. Compute the digest, then hand off to publishing: the spec module's
   `assemble` packages for content-free kits, or a build runner (§1.2).

The live preview and check results come from the same compiler
(`POST /blueprints/{blueprint}/preview`, debounced). CI also runs the spec repo's
`kit-tck` conformance suite against published blueprints.

---

## 5. Provenance: `blueprint_id` and `sandbox_id`

The kit's top level is `additionalProperties: false`, so `blueprintId:` can't
be added as a key. The blueprint ID is carried in three ways:

- a **YAML header comment**, for humans;
- **`args.kitmakerBlueprint`** with `default: bp_…@vN` and
  `env: KITMAKER_BLUEPRINT_ID`. This is the primary machine-readable carrier.
  It doesn't widen the surface, so it never triggers an update prompt;
- a **`lifecycle@1` file** at `/etc/kitmaker/blueprint.json`, holding the
  blueprint ID, version, digest, and AI-authorship summary.

Later, when we publish OCI images, a Dockerfile `LABEL io.kitmaker.blueprint-id`
and signed attestations get added. A custom capability was rejected as a
carrier: every publish would change its config, and the update gate would
then demand approval each time.

IDs: `bp_<ULID>` (one per blueprint, stable across versions; a version is
`bp_…@vN`, and publishing an identical digest reuses the existing version),
`prj_<ULID>` for projects, `sbx_<ULID>` for sandboxes. The project isn't in the
image, because one blueprint version serves many projects; it comes from the
repo's `sbxenv.yaml` at registration.

**Sandbox registration:** an opt-in host-side `lifecycle.postCreate` hook in
`sbxenv.yaml` POSTs `{blueprint@version, digest, project, $SBX_SANDBOX_NAME}`
with an enrollment token (read from the `sbx secret` store). Tokens are issued
**per person per project** when someone downloads a blueprint in the app, so
registration says who created the sandbox; that, not the registry pull, is the
"downloaded" event (pulls are logged too, but only name the pull credential).
The hook then uses `sbx exec` to write `/etc/kitmaker/sandbox.json`, and never
blocks create. Blueprints run without an environment file can use an optional
in-sandbox startup check-in instead, which needs a bounded network rule.
Provenance is for traceability, not trust.

### 5.1 Activity and right-sizing

The logs Kit Maker keeps answer, for any sandbox: which blueprint version and
blocks it ran, who made that version (blueprint history plus the publisher),
who created the sandbox and for which project, and what network access it used
and didn't use.

- **Source.** sbx already records network decisions: `sbx policy log --json`
  (checked on sbx 0.48.0) gives, per sandbox name and host, allowed or blocked,
  a request count and first/last seen. Blocked entries name the rule and
  reason; allowed entries don't, so Kit Maker matches each allowed host to the
  blueprint's rules itself with the same evaluator the network checks use.
  Filesystem logs are listed by sbx but not supported yet.
- **Collection is the open problem.** The log keys sandboxes by sbx name, so
  registration maps that name to `sbx_…`. `postCreate` runs once, so something
  on the host must also upload on stop or on a schedule (a small `kitmaker`
  reporter, or an sbx hook if one appears). Needs an experiment before we
  build on it, like the credential-binding one (§1.1).
- **Right-sizing.** Per project, over a window (say 30 days of sandboxes): a
  rule no traffic matched is a candidate to cut; a blocked host that keeps
  appearing is a candidate to add or to confirm as denied. Kit Maker offers a
  trimmed block (or a project-only edit) shown with the usual red "removed"
  diff, and never applies it on its own. Usage is per project because one
  blueprint can serve projects with different needs.

---

## 6. Data model (Postgres)

The full schema, with a Mermaid ER diagram, is in
[docs/database.md](docs/database.md). Summary (`blueprints`,
`blueprint_includes` and `blueprint_revisions` exist today; the rest is planned):

```
blueprint_includes(id, blueprint_id, child_blueprint_id, track[stable|latest|pinned], pinned_version_id, position)  -- sub-blueprints
slots.origin_blueprint_id NULL  -- set when the slot comes from an included blueprint
slots.overrides_slot_id NULL    -- set when this blueprint's slot overrides an included one
integrations(id, kind, driver, config_encrypted, is_default, verified_at)          -- per-deployment drivers
teams, users, team_user(role)

projects(id prj_ulid, team_id, name, description, blueprint_id,
       track[stable|latest|pinned], pinned_version_id NULL)
project_repos(project_id, repo, path)                    -- where its sbxenv.yaml lives

goals(id, blueprint_id, statement text, sources jsonb, created_by)
checks(id, blueprint_id, type, spec jsonb, expectation, status, last_evaluated_at,
       authored_by[user|ai], suggestion_id)
check_slot(check_id, slot_id)

blueprints(id bp_ulid, team_id, slug, display_name,
     kind[workload|mixin|set|environment], registration_enabled, network_preset)
blueprint_revisions(blueprint_id, seq, actor, action, slot_key, summary,
       snapshot jsonb, yaml)                              -- edit history (§2.5a), today kit_revisions
slots(id, blueprint_id, key, label, section, required bool, position,
      state[blank|suggested|filled|optional], optional_capability bool, group_name NULL)
block_links(id, slot_id, block_id, track[latest|pinned], pinned_version_id NULL,
      base_version_id,                                   -- version the overrides were last rebased on
      overrides jsonb, overrides_hash,                   -- patch ops keyed by item _k
      rebase_state[clean|update_available|conflict], pending_rebase jsonb NULL)

blocks(id, team_id, type, slug, name, description, tags, visibility,
       parent_block_id NULL, parent_track, parent_overrides jsonb NULL,   -- forks inherit
       current_version_id, embedding vector NULL)        -- pgvector for semantic search
block_versions(id, block_id, version, payload jsonb, payload_hash, changelog,
       authored_by[user|ai], accepted_by, suggestion_id, created_at)   -- immutable

ai_suggestions(id, team_id, blueprint_id NULL, slot_id NULL, block_id NULL,
       kind[outline|fill|edit|checks|review|explain|import|dedupe],
       prompt text, proposal jsonb, diff jsonb, rationale, evidence jsonb,
       widens bool, status[proposed|accepted|rejected|superseded],
       model, input_tokens, output_tokens, cached_tokens, created_by, decided_by)

blueprint_versions(id, blueprint_id, version, digest, descriptor_yaml, files jsonb,
       permission_surface jsonb, surface_diff jsonb, widened bool,
       checks_snapshot jsonb, ai_review jsonb, release_notes, published_by)
blueprint_version_blocks(blueprint_version_id, block_version_id, overrides_hash, position)

published_images(blueprint_version_id, repository, digest, pushed_at)  -- docker.io/acme/sbx-kit-acme-shop@sha256:…
channels(blueprint_id, name[latest|stable|…], blueprint_version_id, promoted_by, promoted_at)
repo_references(id, project_id NULL, blueprint_id, repo, path, ref_kind[channel|version|digest], ref_value,
       resolved_version_id, bindings_ok bool, last_seen_at, source[github_scan|registration])
downloads(id, blueprint_version_id, project_id NULL, user_id, kind[repo_reference|file|pull],
       created_at)                                       -- §5: who downloaded what

sandboxes(id sbx_ulid, blueprint_version_id, project_id NULL, user_id NULL, sandbox_name,
       host_fingerprint_hash, sbx_version, registered_at, last_seen_at, status)
sandbox_network_activity(sandbox_id, host, decision[allowed|blocked], matched_rule NULL,
       reason NULL, count, first_seen, last_seen)        -- §5.1, from sbx policy log
enrollment_tokens(id, project_id, user_id, token_hash, revoked_at)   -- per person per project
```

---

## 7. App structure

```
cmd/kitmaker/              # one binary: serve | worker | migrate | seed | dev
internal/
  app/                     # config (env), wiring, driver registry
  http/                    # chi router, gonertia (Inertia) pages, SSE, JSON API for sbx registration
  auth/                    # local accounts + OIDC (go-oidc), sessions (scs + pgx store), CSRF
  ent/schema/              # Ent schemas → generated models, edges, hooks, privacy rules
  blueprints/
    blocktypes/            # per type: JSON Schema, renderer, defaults, dedup key, slot templates
    compiler/              # included blueprints + links + overrides → spec kit → YAML, surface, digest
    patch/                 # override ops keyed by _k, 3-way rebase
    checks/                # evaluators (network uses merged policy + preset)
    provenance/            # IDs, provenance injection, registration
  ai/                      # services + prompts/ + fake/
  jobs/                    # River workers and periodic jobs (the scheduler)
  drivers/
    registry/              # OCI push via oras-go
    buildrunner/           # BuildKit client, Docker Build Cloud, GitHub Actions
    git/                   # GitHub App: scan references, open PRs
    storage/               # gocloud.dev/blob: S3, Azure Blob, GCS, local file
    secrets/               # gocloud.dev/secrets: AWS KMS, Azure Key Vault, GCP KMS, local key
    ai/                    # anthropic, bedrock, vertex, fake
migrations/                # Atlas versioned SQL + atlas.sum, embedded in the binary
web/                       # Vue 3 + TS + Vite + shadcn-vue, Inertia pages, built assets embedded via embed.FS
  pages/                   # Blueprints/{Index,New,Show}, Blocks/*, Projects/*, Sandboxes/*
  components/              # Outline (persistent nav), Slot, ChecksPanel, YamlPanel, SurfaceDiff, ai/*
deploy/
  compose/                 # single-computer stack
  helm/kitmaker/           # one chart for EKS, AKS, GKE, OpenShift
spec/                      # vendored JSON Schemas + network presets, pinned commit (Go module pinned in go.mod)
```

---

## 8. Key flows

1. **Start from a goal.** Type the goal, attach a repo or blueprint → the AI streams
   kind, agent, outline and checks → land in the builder with 4–6 blanks left.
2. **Fill the blanks.** The outline shows progress ("3 blanks · 2 failing
   checks"). Open a blank, pick an AI option or a library block, or write your
   own. Checks update live.
3. **Modify with AI.** On any filled slot, type into Ask AI → see the diff →
   choose *this blueprint only* (fork) or *new shared version* (shows how many blueprints
   are affected and how many widen).
4. **Generate a revision.** The AI review flags widenings with risk notes, the
   checks snapshot must be green (or explicitly waived), release notes are
   drafted → version `vN` of `bp_…` minted → image pushed as `vN` and `latest` → optionally
   promote to `stable`. Repos following that channel pick it up on their next
   sandbox create.
5. **Install into a repo (once).** "Use in a repo" → choose follow
   (`stable`/`latest`) or pin → the app opens a PR adding the `sbxenv.yaml`
   reference (plus SANDBOX.md) → from then on the blueprint is edited in the cloud,
   not in the repo.
6. **Edit an inherited block.** Edit a linked block inside a blueprint → the changes
   are kept as overrides and the link stays → when the library publishes v4,
   the blueprint rebases (automatically if clean, AI-assisted if it conflicts) →
   optionally fork into a new library block, or propose the edits upstream.
7. **Shared block update.** Publish v5 → the where-used list shows affected
   blueprints, with AI pre-reviewing each one (including rebasing their overrides)
   → bulk regenerate the safe ones, queue the widening ones for review.
8. **Trace.** Paste a `bp_…`/`sbx_…` ID (or `$KITMAKER_BLUEPRINT_ID`) to see
   its lineage: block versions, who or what authored them, blueprint, and
   sandboxes.

---

## 10. Risks we design around

- **Spec churn.** The kit spec is experimental, so the vendored schema is
  pinned, upstream is diffed on a schedule, and each block version stores
  its schema version.
- **AI correctness.** Structured outputs, compiler validation with repair
  turns, checks, a human accept, and an independent AI reviewer. Widening
  is never automatic.
- **AI cost.** Prompt caching, effort tuned per route, team budgets.
- **Override rebase complexity.** Stable item keys and typed ops keep merges
  meaningful. If a rebase can't be resolved, the link stays on the old base,
  and nothing is changed silently.

---

## 11. Build & deploy: Go, any customer cloud, testable on one machine

**Decision: Go.** Three reasons. sbx and the kit spec are Go, and Kit Maker
imports the same `spec` module the runtime uses. Our company already runs a
lot of Go. And a single static binary is the easiest thing to put inside a
customer's AWS, Azure or OpenShift account and get through a security
review.

**Recommendation: one binary and image, two backing services, everything
else behind a driver. Ship a Docker Compose stack for a single computer and
one Helm chart for every Kubernetes, OpenShift included.**

### 11.1 The binary and image

- **One binary, several roles:**
  - `kitmaker serve`: HTTP, Inertia pages, SSE AI streams, sandbox
    registration API.
  - `kitmaker worker`: River jobs for AI, compile, publish, builds and
    rebases, plus periodic jobs (stale sandboxes, upstream schema diff,
    reference scans).
  - `kitmaker migrate`: Atlas migrations embedded in the binary.
  - `kitmaker seed`: starter library from the spec examples.
  - `kitmaker dev`: serve + worker in one process, for a laptop.
- **The Vue build is embedded** (`embed.FS`), so the image is
  `gcr.io/distroless/static:nonroot` plus the binary: no shell, no package
  manager, almost nothing for scanners to flag. It runs as non-root with any
  UID (OpenShift `restricted-v2` works unmodified), has a read-only root
  filesystem, listens on 8080, logs JSON to stdout, and exposes `/healthz`
  and `/readyz`. Multi-arch (amd64 + arm64), signed (cosign), with an SBOM.
- **Two image variants** (§11.8): `kitmaker` (distroless, external Postgres) and
  `kitmaker-allinone` (bundled Postgres, for Docker sbx, `docker run` and small teams).
- **FIPS:** a build variant uses Go's native FIPS 140-3 module
  (`GOFIPS140`) for customers who need it.
- **Config:** env vars cover only bootstrap: DB URL, storage URL, the
  encryption key or KMS key URI, OIDC, and the URL the deployment is reached
  at. Drivers are configured in the admin UI and stored encrypted (§1.2).
  Secrets come from Kubernetes Secrets, the External Secrets Operator, or a
  CSI driver (Key Vault, Secrets Manager).

### 11.2 Laravel conveniences, in Go

| Laravel | Kit Maker (Go) |
|---|---|
| Migrations | **Atlas** versioned migrations generated from the Ent schema (`atlas migrate diff`), with `atlas migrate lint` in CI blocking destructive changes, embedded and applied by `kitmaker migrate` |
| Eloquent models and relations | **Ent**: schema as code, typed queries, edges with eager loading, hooks (≈ observers), privacy rules (≈ policies) |
| Queues, Horizon, scheduler | **River**: Postgres-backed jobs with transactional enqueue, retries, periodic jobs, and River UI as the dashboard |
| Inertia | **gonertia**: same Vue pages and props as the mockups |
| Form requests / validation | `go-playground/validator` for input, JSON Schema for block payloads |
| Auth, sessions, CSRF | `go-oidc`, `scs` sessions in Postgres, Go's built-in cross-origin protection |
| Storage facade | `gocloud.dev/blob` (S3, Azure Blob, GCS, file) |
| Encryption | envelope encryption via `gocloud.dev/secrets` (KMS or a local key) |
| artisan | `kitmaker <command>` (Cobra) |
| Seeders and factories | `kitmaker seed` plus test factories over Ent |
| Pest | `go test` + `testcontainers-go`, plus Playwright for the UI |

### 11.3 Keep the required dependencies to two

| Required | Why it's enough |
|---|---|
| **PostgreSQL 16+ with pgvector** | System of record, River job queue, sessions, and semantic search. No Redis to install, back up or secure. |
| **Object storage** | S3 API, Azure Blob or GCS through one interface. Holds bundles, build contexts and context files. |

Registry, build runner, AI, git host and identity are **drivers** (§1.2),
each with a local implementation so nothing needs the internet in tests.

### 11.4 Where each piece runs

| Concern | Single computer | AWS | Azure | OpenShift | Other / GCP |
|---|---|---|---|---|---|
| App runtime | `docker compose up`, or `kitmaker dev` natively | EKS + Helm | AKS + Helm | Helm (Route, restricted SCC); operator later | GKE / any k8s + Helm |
| Postgres + pgvector | `pgvector/pgvector:pg16` container | RDS / Aurora PostgreSQL | Azure Database for PostgreSQL Flexible Server | CloudNativePG or Crunchy PGO | Cloud SQL / AlloyDB |
| Object storage | MinIO, or a local folder | S3 | Blob Storage | ODF / NooBaa (S3) | GCS |
| Kit registry | zot on `localhost:5000` | ECR | ACR | Quay / internal registry | Artifact Registry, Docker Hub |
| Build runner | `moby/buildkit` container | in-cluster BuildKit, Build Cloud or GitHub Actions | same | OpenShift Builds/Shipwright, or external | same |
| AI | `fake` driver or Anthropic API | Anthropic API or Bedrock | Anthropic API (Foundry once the Go SDK supports it) | Anthropic API, or the cloud driver it sits next to | Vertex AI |
| Identity | local accounts (Keycloak optional) | Cognito / any OIDC | Entra ID | OpenShift OAuth / Keycloak | any OIDC |
| Secrets / KMS | local key file | Secrets Manager + KMS | Key Vault | Secrets / Vault | Secret Manager + KMS |

Remember `kit.allowedSources` (§1.2). Any registry other than Docker Hub has
to be allowed on developer machines. The local stack uses `localhost:5000`,
the same throwaway registry the kit spec's examples assume.

### 11.5 Single-computer testability

- **`deploy/compose` runs the whole product:** kitmaker (serve + worker),
  Postgres+pgvector, MinIO, zot, buildkitd and Mailpit, plus a seed job (spec
  examples as a starter library, one demo blueprint). `make up` →
  http://localhost:8080. For fast iteration, `make dev` runs
  `kitmaker dev` natively, with Vite hot reload and air for Go, against the
  same containers.
- **A real end-to-end test on a laptop.** Generate a blueprint, publish it to zot
  (built by local BuildKit if it has a Dockerfile), then run `sbx env run`
  on the same machine with `localhost:5000` in `kit.allowedSources`. The
  postCreate hook registers the sandbox back to `localhost:8080`, so the full
  loop runs locally.
- **Tests run against real services.** `go test` uses `testcontainers-go` to
  start Postgres, MinIO, zot and buildkitd per package, with no SQLite
  divergence on jsonb or pgvector. The AI `fake` driver replays fixtures.
  Each driver has a contract suite run against its local implementation.
  Published blueprints go through the spec's `kit-tck`. Playwright covers the main
  flows.
- **The chart is tested locally too.** CI installs the Helm chart on kind
  (and k3d), and a nightly job runs it on OpenShift Local (CRC) to catch
  SCC and Route problems before a customer does.

### 11.6 Operations

- **Upgrades.** The image and chart are semver'd together. Migrations are
  expand/contract, so rolling deploys work. A Helm pre-upgrade hook runs
  `kitmaker migrate`.
- **Air-gapped or restricted egress.** The image and chart can be mirrored
  to the customer's registry. With the Bedrock or Vertex AI driver and an
  internal registry, Kit Maker needs no outbound internet. Telemetry is off
  by default.
- **Observability.** OpenTelemetry traces and metrics (River queue depth, AI
  latency and tokens, build and publish durations), JSON logs, and a status
  page in the admin UI.
- **Optional reference IaC.** Terraform modules for the managed pieces (RDS
  + S3 + ECR + KMS; Azure PG Flexible + Blob + ACR + Key Vault) for
  customers who want a starting point. The chart doesn't depend on them.

### 11.7 Options we considered

TypeScript with AdonisJS was the runner-up: it's the most Laravel-like and
has the biggest hiring pool, but it would need a Go helper for the spec
logic. Python/Django has great built-in tooling but packages worse.
Java/Spring wins enterprise approval but is heavy for a UI-led product.
PHP/Laravel is the most productive of all, but the most likely to be
rejected by enterprise security reviews, and it can't import the spec
module.

### 11.8 Image variants

| Variant | Contents | Use it for |
|---|---|---|
| `kitmaker` | Go binary on distroless static, non-root, read-only root fs | Helm on EKS, AKS, OpenShift and GKE, with managed or operator-run Postgres |
| `kitmaker-allinone` | The same binary plus Postgres 16 + pgvector on a slim Debian base. `kitmaker standalone` supervises Postgres as a child process, runs migrations, then serves. Data lives in `/data` (Postgres, blobs, keys). | Running inside **Docker sbx**, `docker run -p 8080:8080 -v kitmaker:/data`, evaluations, small teams |

Both are built from one Dockerfile (multi-stage), multi-arch and signed. The
all-in-one variant uses the local-folder storage driver
(`gocloud.dev/blob/fileblob`) and needs no other services.

### 11.9 Kit Maker as a Docker sbx kit

Kit Maker ships as its own v3 workload kit,
`docker.io/docker/sbx-kit-kitmaker` (placeholder namespace). One command
runs the whole product in a sandbox:

```
sbx run --kit docker.io/docker/sbx-kit-kitmaker:latest
```

It follows the spec repo's WordPress example of a long-running service
kit:

| Capability | Why |
|---|---|
| `long-running@1` | Keeps serving after the last client session closes |
| `port@1` (8080, `http`) | Publishes the UI. sbx picks the host port, loopback by default |
| `volume@1` `/data` | Postgres, blobs and keys survive recreation |
| `network-policy@2` | Egress only to what this deployment enables: the AI API, the enabled identity providers (`login.microsoftonline.com`, `*.okta.com`, `accounts.google.com`, …), GitHub, registries |
| `credential@1` (`anthropic`, `github`, proxy-managed) | **The Anthropic and GitHub keys never enter the container.** The sbx proxy injects them on the way out, so Kit Maker running in sbx is safer than Kit Maker anywhere else. |
| `kit-registry@1` | Publishes generated kits **straight into the host's sbx image store**. `sbx run --kit <ref>` on the same machine works with no external registry. |
| `resources@1` (optional) | e.g. 2 CPU, 4 GB |
| args | `adminEmail` (bootstrap), `baseUrl`, `authProviders`; secrets for OIDC client secrets go through `sbx secret` |

Notes and limits:

- The spec has **no multi-container topology yet** (the WordPress example
  calls it out), which is why the sbx kit uses `kitmaker-allinone` with
  Postgres in the same container.
- **Builds inside sbx:** content-free kits are assembled in-process and
  pushed through `kit-registry@1`. Kits with Dockerfile content need a build
  runner, either a remote BuildKit or Docker Build Cloud, or sbx's
  experimental nested Docker.
- **Identity providers in sbx** need a redirect URI the browser can reach.
  The published port is loopback with an ephemeral host port, so OIDC in sbx
  suits single-user or demo use. Team deployments run the container behind
  a real hostname.
- **We dogfood it:** the Kit Maker repo gets its own `sbxenv.yaml`, like the
  spec repo has, so an agent develops Kit Maker inside a sandbox made by Kit
  Maker.

---

## 12. Identity & sign-in

Each deployment turns on any combination of providers. All of the following ship in v1. The login page shows
only what's enabled, in the admin's order.

| Provider | How | Notes |
|---|---|---|
| **Username / password** | Built-in. argon2id hashes, password policy, lockout, reset by email (SMTP driver) | TOTP and **passkeys (WebAuthn)** for MFA. Admins can require MFA. Invite-only by default, with optional open signup limited to allowed email domains. |
| **Microsoft Entra ID** | OIDC, single- or multi-tenant | Groups come from the `groups` claim. When a user is in too many groups for the token, they're fetched from Microsoft Graph (needs `GroupMember.Read.All`). App roles are supported too. |
| **Okta** | OIDC (org or custom authorization server) | `groups` claim with a filter; SCIM push supported. |
| **Google** | OIDC | `hd` claim restricts to Workspace domains. Google ID tokens carry **no groups**, so group membership comes from the Admin SDK Directory API or Cloud Identity Groups API via directory sync (§13). |
| **Generic OIDC** | Any compliant IdP | Keycloak, Auth0, Ping, Dex, and OpenShift via Dex. |
| **SAML 2.0** | SP-initiated and IdP-initiated SSO, signed assertions, metadata URL import | Group attributes map to teams the same way as OIDC claims. For enterprises that mandate SAML. |
| **LDAP / Active Directory** | Bind authentication (LDAPS or StartTLS) plus group lookup | For on-prem directories with no OIDC front end. Group membership feeds teams through directory sync (§13). Usually only reachable from inside the customer network. |

Common behaviour:

- **Identities link to one user.** A person can sign in with several
  providers. Linking happens only on a verified email from a trusted
  provider, or explicitly by the user.
- **Provisioning:** just-in-time on first login (default role: none until a
  team grants something), or pre-provisioned only (SCIM, the API, or an
  admin invite). Chosen per provider.
- **Deprovisioning:** SCIM `active=false`, a failed directory sync, or a
  callout that says "unknown user" suspends the user and revokes sessions
  and tokens.
- **Sessions:** server-side in Postgres, with idle and absolute timeouts,
  "sign out everywhere", and admin revoke. Secure cookies, CSRF protection
  and a strict CSP.
- **Machine access:** personal access tokens (scoped, expiring) and
  **service accounts** (non-human users that belong to teams), for CI and
  the API. The sbx registration hook keeps its own write-only enrollment
  tokens (§5).
- **Break-glass:** one local Owner account created at bootstrap
  (`KITMAKER_BOOTSTRAP_ADMIN`). It can be disabled after SSO works and
  re-enabled from the CLI with database access.

## 13. Teams & where membership comes from

Teams are the unit of ownership and access. Every blueprint, block, project,
integration and AI budget belongs to a team. A team's members can come from
several **sources at once**, and every membership row records its source,
so the UI can say *"Sam is a Maintainer via Entra group Platform-Eng"*.

| Source | Direction | How it works |
|---|---|---|
| **Manual** | In the app | Team admins add and remove people and set roles. |
| **IdP claim rules** | At sign-in | Rules map token claims to team + role, e.g. `groups contains "Platform-Eng" → platform: maintainer`. Re-evaluated on every login and token refresh. |
| **Push API: SCIM 2.0** | IdP → Kit Maker | Standard `/scim/v2/Users` and `/Groups`. Entra and Okta provision users and groups. Groups map to teams. |
| **Push API: REST** | Any system → Kit Maker | `PUT /api/v1/teams/{team}/members` (full replace) and `PATCH` (add/remove), with a service-account token. For HR systems, ticketing, or scripts. |
| **Directory sync (pull)** | Kit Maker → directory, on a schedule | Microsoft Graph, Okta API, Google Admin SDK / Cloud Identity, LDAP/AD. A River periodic job, default every 15 min, with a diff preview before first activation. |
| **Real-time callout** | Kit Maker → customer endpoint, live | See below. |

**Real-time callout.** Kit Maker calls a customer-owned HTTPS endpoint to
ask *"which teams and roles does this person have right now?"*:

```
POST https://authz.acme.internal/kitmaker/memberships
{ "user": {"id": "usr_…", "email": "sam@acme.com", "idp": "entra", "sub": "…", "groups": ["…"]},
  "deployment": "kitmaker.acme.internal" }
→ 200 { "memberships": [ {"team": "platform", "role": "maintainer"}, {"team": "payments", "role": "viewer"} ],
        "ttl": 300 }
```

- **When it's called:** at sign-in, when the cached answer expires (the
  endpoint's `ttl`, capped by the deployment), and *forced fresh* before
  sensitive actions (publish, promote to `stable`, approve a widening,
  manage a team).
- **Security:** requests are signed (HMAC with a shared secret, or mTLS),
  carry a timestamp and nonce, and the target URL must be on an admin
  allowlist (SSRF protection).
- **Failure modes** (per deployment): *fail closed* (drop callout-sourced
  access), or *last known good* for up to N minutes and then fail closed.
  Every call is in the audit log with its latency and result.
- **Decided:** the callout is a *membership lookup with TTL*, not a
  per-request authorization decision. Kit Maker keeps making the decisions
  itself, so it stays fast and predictable. (An external decision point,
  e.g. OpenID AuthZEN, stays a far-future option.)

**Combining sources.** A team's membership is the union of its sources.
Externally sourced rows are read-only in the UI, so you change them where
they come from. Team admins can still **exclude** a person locally (deny
wins) or **pin** an extra manual member, and both show as overrides. When a
person gets roles from several sources, the highest role applies, and every
source is shown.

**Team structure (decided): teams nest.** A child team inherits **view**
access to everything its parent can see, and never write access. So
*Engineering > Platform > Payments* lets Payments read Platform's blocks,
but only Platform's own Maintainers can change them.

## 14. RBAC

Every action on every object goes through one permission check. The same
check filters lists and search results, AI context included.

**Roles are permission sets, and custom roles ship in v1.** Every feature,
screen action, API endpoint and background job is gated by a named
**permission**. A role is a named set of permissions, at one of two scopes:

- **Deployment roles** apply everywhere: settings, identity, integrations,
  audit, creating teams.
- **Team roles** apply to everything a team owns, and to anything shared
  with it at that role.

Admins create custom roles in the role editor, which lists every permission
grouped by area, with a plain-language description of each. Built-in roles
are read-only presets that can be cloned as a starting point.

**Built-in presets**

| Scope | Role | In short |
|---|---|---|
| Deployment | **Owner** | Every permission, including role management. Break-glass account. |
| Deployment | **Admin** | Identity, integrations, teams (**only admins create teams**), roles, policies, guardrails, settings. |
| Deployment | **Auditor** | Read everything, plus audit view and export. No writes. |
| Deployment | **Member** | Sign in, manage own tokens. Sees only what teams grant. |
| Team | **Viewer** | View blueprints, blocks, versions, references, sandboxes. |
| Team | **Contributor** | Viewer + create and edit blueprints and blocks, use AI, request reviews. |
| Team | **Maintainer** | Contributor + generate, publish, promote, approve, publish shared block versions, open reference PRs. |
| Team | **Team admin** | Maintainer + membership, membership sources, sharing, team-level policies. |

**Permission catalogue** (v1; every new feature adds its permissions here)

| Area | Permissions |
|---|---|
| Blueprints | `blueprint.view` `blueprint.create` `blueprint.edit` `blueprint.archive` `blueprint.transfer` `blueprint.share` `blueprint.checks.edit` `blueprint.include` `blueprint.history.view` `blueprint.download` |
| Versions & channels | `blueprint.publish` `blueprint.promote` `blueprint.channel.manage` `blueprint.version.view` |
| Approvals | `approval.request` `approval.decide.widening` `approval.decide.promote` `approval.decide.shared_block` `approval.self_justify` `blueprint.policy.manage` |
| Collections | `collection.view` `collection.create` `collection.edit` `collection.blueprints.manage` `collection.policy.manage` `collection.share` `collection.archive` |
| Blocks | `block.view` `block.use` `block.create` `block.edit` `block.publish_version` `block.fork` `block.share` `block.archive` `block.import` `library.hygiene.apply` |
| Inheritance | `link.rebase.apply` `link.conflict.resolve` `link.revert` `block.propose_upstream` |
| Guardrails | `guardrail.view` `guardrail.manage` `guardrail.exception.request` `guardrail.exception.approve` |
| Build & publish | `build.run` `build.logs.view` `image.publish` |
| References & sandboxes | `reference.view` `reference.open_pr` `reference.pin` `sandbox.view` `sandbox.revoke` `enrollment_token.manage` |
| AI | `ai.use` `ai.review.request` `ai.review.view` `ai.prompts.view` `ai.budget.view` `ai.budget.manage` `ai.policy.manage` |
| Teams | `team.view` `team.create` *(deployment scope, admins only)* `team.edit` `team.archive` `team.members.manage` `team.sources.manage` `team.roles.assign` |
| Identity & access | `user.view` `user.invite` `user.suspend` `auth.providers.manage` `scim.manage` `directory_sync.manage` `callout.manage` `token.create` `service_account.manage` `role.manage` `role.assign.deployment` |
| Integrations | `integration.view` `integration.use` `integration.manage` `integration.test` |
| Platform | `settings.manage` `branding.manage` `notifications.manage` `webhook.manage` `audit.view` `audit.export` `backup.run` `backup.restore` `status.view` |

**Rules that keep custom roles safe**

- **No escalation.** You can only create, edit or assign a role whose
  permissions you hold yourself. Only Owners can grant `role.manage`.
- **Scope check.** Deployment-scope permissions (`team.create`,
  `auth.providers.manage`, `settings.manage`, …) can only go in deployment
  roles. Team roles can't carry them.
- **Dangerous combinations are flagged** in the editor, e.g. a role with
  both `blueprint.publish` and `approval.self_justify`, or `guardrail.manage` on a
  team role.
- **Roles are referenced by key** everywhere: claim rules, SCIM group
  mappings, callout responses (`{"team": "platform", "role":
  "release-manager"}`) and approval policies (approver role). An unknown key
  maps to no access and raises an admin alert.
- **Changes are audited and versioned.** Editing a role shows how many
  people it affects before saving.

**Every function is mappable, and the code enforces it.** Permissions live
in one Go registry (`internal/authz/permissions.go`): key, scope,
description and area. Every HTTP route, Inertia action, API endpoint, River
job and AI action declares the permission it needs at registration. A test
fails the build if any route or job has no permission. The role editor, API
token scopes and docs are generated from the same registry, so a new
feature can't ship without being assignable to a role.

**Ownership and sharing**

- Each object has an **owner team**. Owners can **share** it with other
  teams at a role (e.g. the *Platform* team shares the *Deny prod* block
  with everyone at `use`).
- Library visibility: *team only* (default), *shared with chosen teams*,
  or *everyone in the deployment* (view + use; edits stay with the owner).
- **Cross-team impact:** publishing a new version of a block or included blueprint
  that other teams' blueprints follow notifies those owners. If it **widens**
  their permission surface, their blueprints wait for *their* Maintainer's
  approval, not the publisher's.

**Collections.** Blueprints can be grouped into **collections**, e.g. *Payments
blueprints* or *Customer-facing agents*. A collection is owned by a team, can be
shared like any object, and is where policy for a group of blueprints lives.

**Approvals and separation of duties: one setting at three levels.**

| Level | Set by | Example |
|---|---|---|
| **Global** (deployment) | Admin | "Widening needs 1 approval; the author can't approve. Promoting to `stable` needs 1 approval." |
| **Collection** | Collection owner's Team admin | "Payments kits: promoting to `stable` needs 2 approvals, one from Security." |
| **Blueprint** | Blueprint owner's Team admin | "acme-shop: widening needs no approval while it's in draft." |

- **The most specific level wins**, for each action separately: widening,
  promote to `stable`, publish shared block version, guardrail exception.
- **Locks:** a higher level can **lock** a setting so lower levels can only
  make it stricter, never looser. (e.g. Global locks "author can't approve
  widenings", so no collection or blueprint can turn it off.)
- **Settings per action:**
  - *Who can approve*: **AI reviewer**, **another person** (role, and
    optionally a required team such as *Security*), or both.
  - How many approvals are needed (0–N).
  - Whether the author may approve their own change.
  - How much risk the AI reviewer may approve on its own (*low*, *medium*
    or *high*).
  - Whether self-approval with a written justification is allowed when no
    other eligible person exists.

**AI reviewer.** Many companies will have one Maintainer, so a human
two-person rule can't be the default. Instead, the AI reviewer is a
first-class approver.

- **It's independent of the author.** It runs as a separate call with its
  own prompt, and sees none of the authoring conversation or earlier AI
  suggestions. Its input is the change itself: the permission-surface diff,
  the compiled blueprint, the goal and its checks, the guardrails that apply, and
  the blueprint's history.
- **Deterministic gates run first.** Failing checks or a guardrail
  violation block the change before the AI is even asked, so the AI can't
  approve its way past them.
- **It gives a structured verdict:** *approve*, *request changes* (with
  concrete fixes the author can apply), or *escalate to a human*. Each
  verdict carries a risk level and per-finding rationale. It escalates
  when the risk is above the policy's limit, or always for certain
  changes: new credentials, a new inject domain on an existing credential
  (sbx doesn't gate it, §1.1), wildcard hosts, `privileged@1`, host mounts,
  and removing a deny rule.
- **Recorded like any approver.** The approval names the AI reviewer, the
  model (`claude-opus-5-5`), the review-prompt version and the policy it
  applied. It appears in the blueprint's authorship record and the audit
  log.
- **A human always presses publish.** Even when the AI drafted the change
  and the AI approved it, the person publishing is recorded and accountable.
  The AI never publishes, promotes or merges by itself.
- **With no second person:** if the AI escalates and the author is the only
  eligible approver, they can self-approve **only by typing a
  justification**. That approval is flagged in the audit log and the weekly
  admin digest.

**Out-of-the-box global defaults** (any level can change them, unless a
level above locks them):

| Action | Default |
|---|---|
| Widening | 1 approval from **the AI reviewer or another Maintainer**. The author may self-approve with a justification only when the AI escalates and no one else is eligible. The AI may approve **low** risk on its own. |
| Promote to `stable` | Same as widening, and all checks must pass. |
| Publish a shared block version | AI reviewer runs the where-used check (§2.2). Each affected blueprint's own policy then applies to that blueprint. |
| Guardrail exception | 1 approval from an **Admin**. The AI may review but can't approve. |

When a policy can't be met (e.g. "2 human approvals" on a team with one
Maintainer), the policy editor warns as it's saved, not at the moment
someone tries to publish.
- The blueprint's **Review & generate** screen shows the **effective policy and
  where each part comes from** ("2 approvals, from collection *Payments
  blueprints*; author can't approve, locked globally"), using the same origin
  markers as the outline.
- **Mandatory guardrails** use the same cascade. A global or collection
  rule like "every blueprint must include *Deny cloud metadata*" makes the
  compiler refuse blueprints without it, unless an approved guardrail exception
  exists.

**AI respects RBAC.** AI calls run as the requesting user. Library search
(pgvector), context assembly, and "reuse a block" suggestions only see what
that user can `view` or `use`. AI never proposes a block the user can't
link.

**API tokens** carry scopes that are intersected with the owner's current
permissions. A token never outlives or exceeds the person behind it.

**Implementation.** Role → permission sets are stored in Postgres and cached
in memory, with the permission registry in code. Checks live in one `authz`
package, enforced as Ent privacy rules on
mutations and as SQL predicates for list filtering ("teams this user can
`view` through any source"). There's no extra service to deploy. An
external PDP (AuthZEN, §13) can be plugged in later. Custom roles are an
open question.

**Audit.** Every mutation and every denied attempt is recorded: actor,
membership source used, decision, before/after where relevant. It can be
exported to a SIEM (OTel logs, syslog, or S3/Blob objects).
