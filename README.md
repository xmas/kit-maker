# Kit Maker

Build Docker Sandboxes kits from reusable blocks.

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

Same address and the same data, but Go rebuilds on save and the UI hot-reloads.

```bash
make test
```

Run `make` to see every target.
