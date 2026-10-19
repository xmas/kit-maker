-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "failed_logins" bigint NOT NULL DEFAULT 0, ADD COLUMN "locked_until" timestamptz NULL, ADD COLUMN "session_version" bigint NOT NULL DEFAULT 1;
-- Create "sessions" table
CREATE TABLE "sessions" ("token" character varying NOT NULL, "data" bytea NOT NULL, "expiry" timestamptz NOT NULL, PRIMARY KEY ("token"));
-- Create index "sessions_expiry_idx" to table: "sessions"
CREATE INDEX "sessions_expiry_idx" ON "sessions" ("expiry");
