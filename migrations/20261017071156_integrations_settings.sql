-- Create "deployment_settings" table
CREATE TABLE "deployment_settings" ("key" character varying NOT NULL, "value_sealed" bytea NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("key"));
-- Create "integrations" table
CREATE TABLE "integrations" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "kind" character varying NOT NULL, "driver" character varying NOT NULL, "name" character varying NOT NULL, "config_sealed" bytea NOT NULL, "is_default" boolean NOT NULL DEFAULT false, "verified_at" timestamptz NULL, "last_error" text NOT NULL DEFAULT '', PRIMARY KEY ("id"));
-- Create index "integration_kind_name" to table: "integrations"
CREATE UNIQUE INDEX "integration_kind_name" ON "integrations" ("kind", "name");
