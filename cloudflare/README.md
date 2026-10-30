# Kit Maker on Cloudflare (free tier)

The same Kit Maker UI as the server edition, deployed as a Cloudflare Pages
site with no server to run:

- **The engine runs in the browser.** `cmd/kitwasm` compiles `internal/lite`
  (the blueprint compiler, patch ops, checks, conflicts and history from
  `internal/blueprints`, behind the same routes and Inertia props as
  `internal/http`) to `kitmaker.wasm`. `web/lite/boot.ts` loads it and sends
  the app's requests to it instead of a server.
- **Pages Functions + D1 store the data.** `functions/api/*` keep blocks,
  blueprints and history as JSON documents in one D1 database, with
  optimistic locking (`migrations/`).
- **No passwords.** Anyone signs in by proving they own an email address:
  they get a 6-digit code and a sign-in link (either works once, for 15
  minutes), sent through [Resend](https://resend.com). Once signed in, the
  key button in the header adds a passkey (Touch ID, Windows Hello, a phone
  or security key) for next time. `ALLOWED_EMAILS` can narrow who may sign
  in; without it, any verified email can.
- **The starter content is static.** `public/starter.json` is the release's
  block library and sample blueprints (`make cf-starter`). A document saved
  in D1 replaces the starter document with the same id.
- **No backend? It still works.** Opened without the Functions (for example
  `make cf-build` and any static host), everything is saved in the browser
  and the header says "Saved in this browser".

What it doesn't do: publish to a registry (no BuildKit here; the Run dialog
offers the local copy and its `sbxenv.yaml`), teams and roles (everyone
signed in can edit, so with sign-up open to any email, anyone who signs in
can change the shared blueprints: set `ALLOWED_EMAILS` to keep it to your
people), or the admin CLI.

Free-tier fit: Pages (unlimited static requests, 100k Function requests a
day), D1 (5 GB, 5M rows read and 100k written a day). The engine is about
2.7 MB compressed and is cached after the first visit.

## Install

You need a Cloudflare account, Node 22, Go (the version in `go.mod`) and
Docker only if you regenerate the starter content. From the repository root:

```bash
make cf-build
```

Then, in `cloudflare/`:

1. **Sign in to Cloudflare.**

   ```bash
   cd cloudflare && npm install && npx wrangler login
   ```

2. **Create the database** and paste the `database_id` it prints into
   `wrangler.toml`. The id isn't secret, so commit it.

   ```bash
   npx wrangler d1 create kitmaker
   npx wrangler d1 migrations apply kitmaker --remote
   ```

3. **Create the Pages project.**

   ```bash
   npx wrangler pages project create kitmaker --production-branch=main
   ```

4. **Set up sign-in email.** Create a free [Resend](https://resend.com)
   account, add the domain `ezdata.xyz` under Domains, and add the DNS records
   it shows to the ezdata.xyz zone in Cloudflare (with the zone on
   Cloudflare, Resend can add them for you). Then create an API key with
   sending access.

5. **Set the secrets.** Each command prompts for the value:

   ```bash
   npx wrangler pages secret put JWT_SECRET --project-name=kitmaker         # openssl rand -base64 48
   npx wrangler pages secret put RESEND_API_KEY --project-name=kitmaker     # the Resend API key
   npx wrangler pages secret put MAIL_FROM --project-name=kitmaker          # Kit Maker <login@ezdata.xyz>
   npx wrangler pages secret put ALLOWED_EMAILS --project-name=kitmaker     # optional: @ezdata.xyz,friend@example.com
   npx wrangler pages secret put ANTHROPIC_API_KEY --project-name=kitmaker  # optional: AI features
   ```

   `ALLOWED_EMAILS` takes addresses and `@domain` entries; leave it unset to
   let any verified email in. Remove a secret with
   `npx wrangler pages secret delete NAME --project-name=kitmaker`.

6. **Deploy.**

   ```bash
   npx wrangler pages deploy dist --project-name=kitmaker --branch=main
   ```

   It prints a `https://kitmaker.pages.dev` address.

7. **Put it on ezdata.xyz.** In the Cloudflare dashboard, open Workers &
   Pages → kitmaker → Custom domains → Set up a domain, and enter the host
   (for example `kitmaker.ezdata.xyz`, or `ezdata.xyz` itself). If the
   ezdata.xyz zone is on the same Cloudflare account, the DNS record and
   certificate are created for you. Nothing in the app names the host, so no
   rebuild is needed. Passkeys belong to the host they were made on, so pick
   the final host before people add them.

## Automatic deployments

`.github/workflows/cloudflare.yml` deploys on every push to the
`cloudflare` branch, as the msdgc site does: it tests the engine, runs
`make cf-build`, applies D1 migrations, then deploys to production.
Set it up once:

1. Create an API token in the Cloudflare dashboard (My Profile → API Tokens
   → Create token → Custom). Give it **Account · Cloudflare Pages · Edit**
   and **Account · D1 · Edit** for your account.
2. In the GitHub repository, open Settings → Secrets and variables → Actions,
   and add `CLOUDFLARE_API_TOKEN` (the token) and `CLOUDFLARE_ACCOUNT_ID`
   (the id on the dashboard's Workers & Pages overview).
3. Make sure `wrangler.toml` has your real `database_id`, then push to
   `cloudflare`. You can also run the workflow by hand from the Actions tab.

Secrets set with `wrangler pages secret put` stay in Cloudflare, and deploys
don't touch them.

## Develop

```bash
make cf-dev      # build, then the site + Functions + a local D1 on http://localhost:8788
make cf-starter  # regenerate public/starter.json after changing internal/seed (needs Docker)
```

`make cf-dev` reads settings from `cloudflare/.dev.vars` (git-ignored). It
needs only `JWT_SECRET=<any local string>`: without `RESEND_API_KEY`, sign-in
emails on localhost are printed in the wrangler output instead of sent.
Passkeys work on `localhost` in Chrome and Safari.

## Notes

- Starter IDs come from slugs (`blk_github-egress`, `bp_acme-shop`), so a new
  release's `starter.json` keeps working with blueprints already saved in D1.
  If someone published a new version of a starter block, their saved copy
  wins over the release's newer starter version of it.
- Other people's changes show up when you next open a page (it checks D1 at
  most every few seconds). If two people save the same blueprint at once, the
  second save is refused, the page reloads their version and asks them to
  make the change again.
