# Kit Maker

Builds **blueprints**, the designs for **Docker Sandboxes Kit v3** kits, from reusable,
inheritable blocks: state a goal, fill in the blanks, publish a blueprint version as an OCI kit
image, and reference it from a repo's `sbxenv.yaml`. Each customer deploys their own copy (Docker
container, Helm, or as a Docker sbx kit).

This file describes what exists. Open work, plans and known gaps live in beads (`bd ready`,
`bd show <id>`), never here: when work lands, update this file to the new state and close the
bead. Why it is shaped this way is [DESIGN.md](DESIGN.md) (section numbers are cited by beads).
Decisions are `decision` beads. The schema reference is [docs/database.md](docs/database.md).
UI mockups: https://claude.ai/artifact/JxCrWdo1hg9XaFNLfPXLZG · architecture page:
https://claude.ai/artifact/LgYQGcs5jUMVPtxnrdPXPF

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:1105d646 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/core-concepts/sync-concepts.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->

## Build & test

```bash
make up         # build + start the stack in Docker → http://localhost:8080 (ports in deploy/compose/.env)
make restart    # recreate kitmaker + worker after editing deploy/compose/kitmaker.env
make cli ARGS="seed --help"   # run any kitmaker command inside the stack
make dev        # the everyday test version on :8080, signed in as the bootstrap admin; hot reload
make test       # go test ./... (testcontainers: needs Docker)
make generate   # regenerate Ent code after editing internal/ent/schema
make migrate-diff name=<name>   # new Atlas migration from the Ent schema
make certs      # serve the local zot over HTTPS (mkcert) so sbx can run published blueprints
make helm-lint  # lint + render the chart
make down       # stop (keeps volumes)
```

`make dev` and `make up` both serve http://localhost:8080 and share the same database, so
switching between them keeps your data and session. `make dev` stops the app containers and runs
`kitmaker dev` natively under air (`.air.toml`, pinned via `go run`, nothing to install): Go, SQL
and fixture changes rebuild and restart in a few seconds, and a change that doesn't compile keeps
the last good build running and prints the error. Vue/CSS changes apply without a reload. Use
`make up` to check the production image. In the desktop app, `.claude/launch.json` starts
`make dev` as "kit-maker dev". `make dev` needs no sign-in: `kitmaker dev --dev-user`
(`KITMAKER_DEV_USER`, defaulted by the Makefile to `KITMAKER_BOOTSTRAP_ADMIN`; empty turns it off)
treats every request without a session as that account (`auth.AutoSignIn`). It refuses a
non-localhost base URL, and `serve` never reads it.

Local settings and secrets go in `deploy/compose/kitmaker.env` (git-ignored; `make env` copies
`kitmaker.env.example` and appends a generated `KITMAKER_SECRETS_URL` and
`KITMAKER_BOOTSTRAP_PASSWORD`). Wiring to the stack's own containers lives in `compose.yaml`.

## What exists

- **One binary** `cmd/kitmaker` (Cobra): `serve`, `worker`, `migrate`, `seed`, `dev`, plus admin
  admin commands `user list|create|set-password|suspend|activate|sign-out`,
  `team create|list|add-member|remove-member|set-parent`, `role assign|revoke|list|permissions`,
  `integration list|set|test|drivers`, `ai ping|usage|budget` and `secrets new-key`. Config is `KITMAKER_*` env
  (`internal/app/config.go`); `KITMAKER_DATABASE_URL`, `KITMAKER_STORAGE_URL` and
  `KITMAKER_SECRETS_URL` are required (`make env` generates a local keeper key).
- **Secrets & integrations** `internal/secrets` seals values with a gocloud keeper (`base64key://`,
  `awskms://`, `azurekeyvault://`, `gcpkms://`). `internal/drivers` registers per-deployment drivers
  (registry: dockerhub, ghcr, ecr, acr, quay, zot; build_runner: none, buildkit, build_cloud,
  github_actions; git_host: github_app; ai: fake, anthropic, bedrock, vertex, registered from
  `internal/ai`), each with field specs,
  validation and (where possible) a live test. `internal/settings` stores sealed integrations and
  deployment settings. Seed creates `local-zot`, `local-buildkit` and `fake` defaults.
- **HTTP** `internal/http`: chi, `/healthz`, `/readyz` (database, migrations, storage), `/login`,
  `/logout`, `/account/sign-out-everywhere`, the Blueprints index, new blueprint (`GET /blueprints/new`,
  `POST /blueprints`; old `/kits/…` links redirect), the blueprint builder (`blueprint.go`: `GET /blueprints/{id}[/slots/{key}]`, `PUT …/slots/{key}`
  saves a payload as overrides, `POST …/preview` compiles an unsaved draft (JSON, nothing saved),
  `POST …/revert`, `…/link`, `…/unlink`, `…/explain` streams AI text
  as server-sent events via `sse` in `ai.go`; `blueprint_edit.go`: goal, add/remove slots, `…/custom`
  starts a block private to the blueprint, `…/update` moves a link to the block's latest version,
  `…/relink` switches a filled slot to another block, `…/save-as-block` turns block + edits into a
  new library block, `…/resync`, checks; `history.go`: `GET /blueprints/{id}/history` (the History view),
  `history.json[?slot=]`, `revisions/{rev}/yaml`, `POST revisions/{rev}/restore` with an optional
  `slotKey`, `POST /blocks/{id}/versions/{n}/restore`); `run.go`: `GET /blueprints/{id}/run` (the Run dialog's
  data), `POST …/publish` (`blueprints.Publish` + `internal/publish`: BuildKit runs the spec's
  `docker/sandbox-kit:3` frontend on the compiled YAML, stamped `version: "N"`, and pushes
  `<registry>/<namespace>/sbx-kit-<slug>:vN` and `:latest` to the default registry integration;
  an unchanged blueprint publishes nothing; versions in `blueprint_versions`), `…/sbxenv.yaml`
  (follows `:latest` with `pullPolicy: always` once published, else a local folder) and
  `…/download` (`<slug>.tar.gz` with only `<slug>/<slug>.yaml`, which `sbx run ./<slug>/`
  builds itself). sbx pulls only from `kit.allowedSources`, so the registry prefix must be there
  and the block library (`blocks.go`: `/blocks`, `/blocks/{id}`, `PUT` details, `POST …/versions`),
  rendered through gonertia. Strict CSP and security headers (`headers.go`); CSRF via `http.CrossOriginProtection`.
- **RBAC** `internal/authz`: the permission catalogue (`permissions.go`, ~100 keys with scope and a
  view flag), built-in roles (`roles.go`), and `Subject` (deployment roles held directly or through a
  team, team roles minus exclusions, view-only inheritance from parent teams; `Can`, `CanAnywhere`,
  `TeamsWith` for list filtering). Every route is registered with `s.handle(r, method, pattern,
  guard, h)`; `TestEveryRouteHasAGuard` fails on any route added another way. Teams and role
  assignments: `internal/teams`. The bootstrap account is the deployment Owner.
- **Sign-in** `internal/auth`: local accounts (argon2id, min length 12, lockout after 5 failures
  for 15 min), scs sessions stored in Postgres (idle 2h, absolute 12h), `session_version` for sign
  out everywhere, Inertia validation errors flashed through the session. The bootstrap Owner gets
  `KITMAKER_BOOTSTRAP_PASSWORD` on seed when it has no password. No OIDC/SAML/LDAP yet.
- **Frontend** `web/`: Vue 3 + TS + Vite + shadcn-vue, built into `web/dist` and embedded with
  `embed.FS`. `components/AppShell.vue` is the signed-in frame (nav, user, sign out);
  `pages/Auth/Login.vue`, `pages/Blueprints/Index.vue`, `pages/Blueprints/New.vue`, `pages/Blocks/Index.vue`,
  `pages/Blocks/Show.vue` (reuses the blueprint editors), and the builder `pages/Blueprints/Show.vue`: outline
  on the left, the slot editor in the middle, the blueprint YAML on the right with the selected slot's
  lines highlighted (`compiled.spans`). While the draft is unsaved the editor fetches a preview as
  you type and the YAML shows it against the saved version (`lib/diff.ts`): Result (new green,
  changed amber, removed marked) or Changes (−/+). Saved blueprint edits show as coloured
  gutter bars from `compiled.marks`. The network editor marks edited rows (green added, amber
  changed, red removed) and lists every edit in a callout with a per-edit Undo. The YAML sidebar is resizable (drag its left edge, arrow keys,
  double-click resets; about 40% wide by default, and dragging can widen it past that, folding the
  outline), wraps long lines (toggle), and clicking a line opens the slot that produced it. Either sidebar can be hidden (a hidden outline opens as a
  hover flyout; state in localStorage). On a narrow window the page hides them itself so the
  editor keeps 720px for its header: the outline first, then the YAML (columns stack only below
  768px). The outline scrolls on its own below its
  header (screen height) and keeps the selected row in view. Built from
  `components/blueprint/` (Outline: a tree of every section with each slot shown by its block's name, an add row per
  block type and remove on hover, plus the selection bridge; SlotFrame, NetworkRulesEditor,
  CredentialEditor, RawEditor, BlankSlot, ExplainPanel, YamlPanel, ChecksPanel; prop shapes in
  `lib/blueprint.ts`). Light theme, Docker blue `#1D63ED`, violet only for AI (`web/styles.css`).
- **Data** `internal/ent/schema`: teams, users, team_memberships, blocks, block_versions, blueprints,
  blueprint_includes, slots, block_links, goals, checks, blueprint_revisions, ai_calls, integrations, deployment_settings,
  sessions, role_assignments. Atlas versioned migrations in `migrations/` are embedded and
  applied by `kitmaker migrate` (`internal/dbmigrate`).
- **Jobs** `internal/jobs`: River on Postgres, one example job, one periodic job.
- **Blocks** `internal/blocks`: create a block (version 1 from `blocktypes.Starter` unless a
  payload is given), edit name/description, `Publish` a new immutable version (skipped when the
  content, ignoring `_k`, is unchanged), list and detail with history and the blueprints using it.
- **History** `internal/blueprints/history.go`: every blueprint change goes through `Service.change`, which
  records a `blueprint_revisions` row after it (full `Snapshot` of goal, slots, links and overrides, plus
  the compiled YAML); the first change also records a "Starting point". Public mutators wrap the
  lowercase implementations. `RestoreKit` / `RestoreSlot` apply a snapshot and are recorded too;
  `Resync` drops a slot's edits and moves it to the block's latest version. Blueprint-only blocks are no
  longer deleted (unlinked and unlisted), so snapshots can bring them back. Library blocks restore
  an old version as the newest (`blocks.RestoreVersion`).
- **Blueprints** `internal/blueprints`: `Service.Load` resolves a blueprint's slots (link → block version → apply
  overrides), sets each row's origin, compiles and evaluates checks; `SaveSlot` diffs an edited
  payload against the library version and stores patch ops. `patch/` holds the ops
  (`set|unset|add|remove|replace`, list items keyed by `_k`; ops whose target is gone become
  conflicts). `blocktypes/` registers a block type for every v3 field and capability (top-level
  About, Dockerfile, Included kits, Dependencies, Arguments; network rules; credentials, OAuth, SSH
  agent; install/startup steps, files, interactive; agent launch/context/sessions/ACP/skills;
  resources, port, volume, host mount, USB, git identity, long-running, kit registry, privileged;
  raw capability), each with a section, a description, a starter (from the spec's kitchen-sink
  example; `TestEveryStarterCompiles`) and abilities for filtering (`abilities.go`). A capability
  block's payload is its config plus entry-level `optional`/`description`; slots remember their
  type (`slots.block_type`, `SlotType`). `compiler/` merges network blocks into one
  `network-policy@2` and setup blocks into one `lifecycle@1`, flags a second singleton
  (resources, sessions, interactive, args name, scalar top-level field, Dockerfile vs kits) as a
  problem, names each entry after its block, writes YAML with a `# slot <key> · <block> vN` comment above each slot's lines
  (`Output.Spans` gives each slot's line ranges). With `Block.Base` set it also annotates the
  blueprint's edits (`# + added in this blueprint`, `# ~ changed in this blueprint`, `# - removed in this blueprint: …`;
  `Output.Marks` gives the marked lines). `conflicts.go` finds blocks working against each
  other (`Output.Conflicts`: blocked, widened, duplicate, gap), each with fixes that hide one entry
  of one block in this blueprint (`HideEntry`, `POST …/slots/{key}/hide`). It validates with
  `github.com/docker/sandbox-kit-spec/v3/spec` (the package sbx uses). `checks/` evaluates network
  checks against the merged policy (deny wins, then allow, else blocked). `edit.go`: `Create` (goal
  plus a starter "Network access" slot), `UpdateGoal`, `AddSlot` (the block type picks the
  section), `RemoveSlot` (deletes a private block with it), `StartCustomBlock`, `UpdateLink`
  (re-applies overrides on the new base; edits whose target is gone show as conflicts),
  `AddCheck`/`RemoveCheck`.
- **AI** `internal/ai`: `Client` (`Generate`, `Stream`) over `anthropic-sdk-go` beta messages for
  the Anthropic API, Bedrock and Vertex: model `claude-opus-5-5`, effort per `Route`, structured
  output when `Request.Schema` is set, cache breakpoints on `System` (1h) and `Context` (5m),
  server-side refusal fallback on the first-party API. `Fake` replays `fixtures/<route>.json`
  (first `match` found in the prompt) or exact recordings named by `Request.Key()` from
  `KITMAKER_AI_FIXTURES`; `Recorder` writes those. `Metered` writes an `ai_calls` row per call and
  refuses calls over the team's monthly budget (billable tokens, deployment setting `ai.budget`).
  `App.AI` (`Provider`) resolves the default AI integration on every call. Prompts: `prompts.go`
  (explain; `SearchRequest` for `POST /blocks/search`, which falls back to keyword ranking and
  says so; `DraftRequest` for `POST …/slots/{key}/draft` and "Start with AI" on an empty slot).
  Connect a model with `make cli ARGS="integration set ai anthropic --config api_key=… --default --test"`.
- **Storage** `internal/drivers/storage`: `gocloud.dev/blob` (S3/MinIO, file).
- **Seed** `internal/seed`: Platform team, the bootstrap Owner, demo blocks (github-egress,
  deny-cloud-metadata, packagist-acme, github-token, packagist-token) and the "Acme Shop" blueprint with
  a goal, six checks, two overrides on github-egress and two required blanks, plus toolchain
  blocks with install/runtime allow/deny rules and a "Polyglot CI" blueprint that mixes them, so one
  block's YAML lines are split across the policy (`demo.go`; created once, then left alone).
  Every spec example (`examples/`, embedded) becomes a blueprint whose parts are library blocks
  (identical parts shared; `examples.go`, `TestExamplesCompile`), and nine sample profile
  blueprints (developer, docs-only, internal-only, isolated, ML research, ops agent, support,
  read-only validation…) are built from shared blocks (`profiles.go`, `TestProfilesCompile`).
  The library also gets "Ubuntu 24.04 (public)", a Dockerfile block on the official
  `ubuntu:24.04` image with the platform floor (bash, curl, git, CA store, `agent` uid 1000), for
  workloads that can't use the examples' `dhi.io` bases without a login.
  Idempotent; a block gets a new version when its seeded payload changes. All of this is the
  starter content every release ships: starter blocks are public (every team sees and can link
  them), re-seeding on upgrade publishes changed starter blocks as new versions (`starterBlock`),
  and `kitmaker seed --samples=false` (`KITMAKER_SEED_SAMPLES=false`, Helm `seed.samples`) loads
  only the bootstrap Owner, team and integrations.
- **Deploy**: distroless multi-stage `Dockerfile` (non-root, any UID, read-only root fs),
  `deploy/compose` (Postgres+pgvector, MinIO, zot, BuildKit, Mailpit; host ports picked by
  `ports.sh`), starter Helm chart `deploy/helm/kitmaker` (serve + worker, migrate pre-upgrade job, seed
  post-install/post-upgrade job, optional Route).
- Not yet: custom roles, AI suggestions (threads, accept/reject), 3-way rebase with conflict
  resolution, included blueprints, forks, sharing a blueprint's private block to the library, typed forms beyond
  network and credential. Admin screens are CLI-only.

## Conventions

- Go module `github.com/docker/kit-maker`, Go 1.27. Keep one binary; roles are subcommands.
- Schema changes: edit `internal/ent/schema`, `make generate`, `make migrate-diff name=…`. Never
  hand-edit generated Ent code or applied migrations.
- Tests run against real Postgres (testcontainers), never SQLite: jsonb and pgvector matter.
- Every route must go through `s.handle` with a guard; add new permissions to
  `internal/authz/permissions.go` (and the right presets in `roles.go`). Handlers acting on one
  object call `s.allowed(w, r, perm, ownerTeamID)`; list queries filter with `TeamsWith`.
- UI: the outline is navigation and status indicators only; actions live on the detail page
  (rare ones in the frame's "⋯" `ActionMenu`). The slot frame is compact and keeps the tables
  still while editing: one-line header (status chip opens a popover with link details, Save as new
  block, Resync; `ExplainPopover` floats the AI answer), a fixed-height edits strip whose list
  opens over the content, and a save bar pinned to the bottom of the screen. Popovers close via
  `lib/useDismiss.ts`. Every destructive action that loses work awaits `confirm()` from
  `web/lib/confirm.ts` (rendered by `ConfirmDialog` in `AppShell`), never `window.confirm`;
  removing an empty or unedited slot (the header's trash button) doesn't ask. The builder also
  asks before leaving a slot with unsaved edits. Search is fuzzy over every field (`lib/fuzzy.ts`,
  `SearchBox` with Ask AI); the app header is pinned.
- Words (decision `kit-maker-hcw`): the app says **blueprint** everywhere (code, routes, UI, logs).
  "Kit" names only the spec's format: inside YAML and sbx commands we generate, spec types and
  capabilities, and the one explanation on the Blueprints page. The workload is a **project**.
- Kit semantics come from the spec package. Don't reimplement validation, merging or the
  permission surface.
