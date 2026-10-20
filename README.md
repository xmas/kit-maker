# Kit Maker

Build blueprints for Docker Sandboxes kits from reusable blocks.

## Run it

You need Docker Desktop. For hot reload you also need Go 1.27 and Node 24.

```bash
make up
```

Open http://localhost:8080 and sign in as `admin@kitmaker.local`. The
password is `KITMAKER_BOOTSTRAP_PASSWORD` in `deploy/compose/kitmaker.env`,
which `make up` creates on first run.

## Develop

```bash
make dev
```

Same address and the same data, but Go rebuilds on save, the UI hot-reloads, and
you're signed in as the bootstrap admin without a password. Set `KITMAKER_DEV_USER=`
(empty) in `kitmaker.env` to sign in normally.

To run published blueprints with sbx locally, serve the local registry over HTTPS (sbx only
pulls over HTTPS) and allow it as a kit source:

```bash
make certs
```

Then trust the CA once (`mkcert -install`), restart with `make dev`, and add
`localhost:<ZOT_PORT>/blueprints/` to `sbx settings get kit.allowedSources`.

```bash
make test
```

Run `make` to see every target.
