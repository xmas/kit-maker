-- Sign-in without passwords: an emailed code or link, or a passkey.
DROP TABLE IF EXISTS login_failures;

-- Everyone who has signed in (any verified email, unless ALLOWED_EMAILS
-- narrows it).
CREATE TABLE users (
  email TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

-- An emailed sign-in: id is the SHA-256 of the link's token; code_hash is
-- the SHA-256 of the 6-digit code with the id. Used or expired rows go.
CREATE TABLE login_tokens (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  code_hash TEXT NOT NULL,
  next TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  expires_at TEXT NOT NULL
);
CREATE INDEX login_tokens_email ON login_tokens (email);

-- Passkeys (WebAuthn credentials), each belonging to one email.
CREATE TABLE passkeys (
  id TEXT PRIMARY KEY,           -- credential id, base64url
  email TEXT NOT NULL,
  public_key TEXT NOT NULL,      -- COSE key, base64url
  counter INTEGER NOT NULL,
  transports TEXT,               -- JSON array
  created_at TEXT NOT NULL,
  last_used_at TEXT
);
CREATE INDEX passkeys_email ON passkeys (email);

-- Rate limits: emails sent per address and per IP.
CREATE TABLE auth_events (
  kind TEXT NOT NULL,            -- send-email | send-ip
  key TEXT NOT NULL,
  at TEXT NOT NULL
);
CREATE INDEX auth_events_key ON auth_events (kind, key, at);
