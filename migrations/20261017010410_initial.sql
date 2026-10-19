-- Create "blocks" table
CREATE TABLE "blocks" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "type" character varying NOT NULL, "slug" character varying NOT NULL, "name" character varying NOT NULL, "description" text NOT NULL DEFAULT '', "tags" jsonb NULL, "visibility" character varying NOT NULL DEFAULT 'team', "parent_track" character varying NULL, "parent_overrides" jsonb NULL, "embedding" vector(1024) NULL, "parent_block_id" character varying NULL, "parent_pinned_version_id" character varying NULL, "current_version_id" character varying NULL, "team_id" character varying NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "blocks_blocks_forks" FOREIGN KEY ("parent_block_id") REFERENCES "blocks" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "block_team_id_slug" to table: "blocks"
CREATE UNIQUE INDEX "block_team_id_slug" ON "blocks" ("team_id", "slug");
-- Create index "block_type" to table: "blocks"
CREATE INDEX "block_type" ON "blocks" ("type");
-- Create "block_links" table
CREATE TABLE "block_links" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "track" character varying NOT NULL DEFAULT 'latest', "overrides" jsonb NULL, "overrides_hash" character varying NOT NULL DEFAULT '', "rebase_state" character varying NOT NULL DEFAULT 'clean', "pending_rebase" jsonb NULL, "block_id" character varying NOT NULL, "base_version_id" character varying NOT NULL, "pinned_version_id" character varying NULL, "slot_id" character varying NOT NULL, PRIMARY KEY ("id"));
-- Create index "block_links_slot_id_key" to table: "block_links"
CREATE UNIQUE INDEX "block_links_slot_id_key" ON "block_links" ("slot_id");
-- Create "block_versions" table
CREATE TABLE "block_versions" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "version" bigint NOT NULL, "payload" jsonb NOT NULL, "payload_hash" character varying NOT NULL, "schema_version" character varying NOT NULL, "changelog" text NOT NULL DEFAULT '', "authored_by" character varying NOT NULL, "ai_suggestion_id" character varying NULL, "block_id" character varying NOT NULL, "accepted_by_user_id" character varying NULL, PRIMARY KEY ("id"));
-- Create index "blockversion_block_id_version" to table: "block_versions"
CREATE UNIQUE INDEX "blockversion_block_id_version" ON "block_versions" ("block_id", "version");
-- Create "kits" table
CREATE TABLE "kits" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "slug" character varying NOT NULL, "display_name" character varying NOT NULL, "kind" character varying NOT NULL, "image_repository" character varying NULL, "network_preset" character varying NULL, "registration_enabled" boolean NOT NULL DEFAULT false, "team_id" character varying NOT NULL, PRIMARY KEY ("id"));
-- Create index "kit_team_id_slug" to table: "kits"
CREATE UNIQUE INDEX "kit_team_id_slug" ON "kits" ("team_id", "slug");
-- Create "kit_includes" table
CREATE TABLE "kit_includes" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "track" character varying NOT NULL DEFAULT 'stable', "pinned_blueprint_id" character varying NULL, "position" bigint NOT NULL DEFAULT 0, "kit_id" character varying NOT NULL, "child_kit_id" character varying NOT NULL, PRIMARY KEY ("id"));
-- Create index "kitinclude_child_kit_id" to table: "kit_includes"
CREATE INDEX "kitinclude_child_kit_id" ON "kit_includes" ("child_kit_id");
-- Create index "kitinclude_kit_id_child_kit_id" to table: "kit_includes"
CREATE UNIQUE INDEX "kitinclude_kit_id_child_kit_id" ON "kit_includes" ("kit_id", "child_kit_id");
-- Create "slots" table
CREATE TABLE "slots" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "key" character varying NOT NULL, "label" character varying NOT NULL DEFAULT '', "section" character varying NOT NULL, "state" character varying NOT NULL DEFAULT 'blank', "required" boolean NOT NULL DEFAULT false, "position" bigint NOT NULL DEFAULT 0, "optional_capability" boolean NOT NULL DEFAULT false, "group_name" character varying NULL, "kit_id" character varying NOT NULL, "origin_kit_id" character varying NULL, "overrides_slot_id" character varying NULL, PRIMARY KEY ("id"), CONSTRAINT "slots_slots_overridden_by" FOREIGN KEY ("overrides_slot_id") REFERENCES "slots" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "slot_kit_id_key" to table: "slots"
CREATE UNIQUE INDEX "slot_kit_id_key" ON "slots" ("kit_id", "key");
-- Create index "slots_overrides_slot_id_key" to table: "slots"
CREATE UNIQUE INDEX "slots_overrides_slot_id_key" ON "slots" ("overrides_slot_id");
-- Create "teams" table
CREATE TABLE "teams" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "name" character varying NOT NULL, "slug" character varying NOT NULL, "parent_team_id" character varying NULL, PRIMARY KEY ("id"), CONSTRAINT "teams_teams_children" FOREIGN KEY ("parent_team_id") REFERENCES "teams" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "teams_slug_key" to table: "teams"
CREATE UNIQUE INDEX "teams_slug_key" ON "teams" ("slug");
-- Create "team_memberships" table
CREATE TABLE "team_memberships" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "role" character varying NOT NULL, "pinned" boolean NOT NULL DEFAULT false, "excluded" boolean NOT NULL DEFAULT false, "expires_at" timestamptz NULL, "team_id" character varying NOT NULL, "user_id" character varying NOT NULL, PRIMARY KEY ("id"));
-- Create index "teammembership_team_id_user_id" to table: "team_memberships"
CREATE INDEX "teammembership_team_id_user_id" ON "team_memberships" ("team_id", "user_id");
-- Create index "teammembership_user_id" to table: "team_memberships"
CREATE INDEX "teammembership_user_id" ON "team_memberships" ("user_id");
-- Create "users" table
CREATE TABLE "users" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "email" character varying NOT NULL, "display_name" character varying NOT NULL DEFAULT '', "password_hash" character varying NULL, "status" character varying NOT NULL DEFAULT 'active', "is_service_account" boolean NOT NULL DEFAULT false, "last_login_at" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");
-- Modify "blocks" table
ALTER TABLE "blocks" ADD CONSTRAINT "blocks_block_versions_current_version" FOREIGN KEY ("current_version_id") REFERENCES "block_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "blocks_block_versions_parent_pinned_version" FOREIGN KEY ("parent_pinned_version_id") REFERENCES "block_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "blocks_teams_blocks" FOREIGN KEY ("team_id") REFERENCES "teams" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Modify "block_links" table
ALTER TABLE "block_links" ADD CONSTRAINT "block_links_block_versions_base_version" FOREIGN KEY ("base_version_id") REFERENCES "block_versions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "block_links_block_versions_pinned_version" FOREIGN KEY ("pinned_version_id") REFERENCES "block_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "block_links_blocks_links" FOREIGN KEY ("block_id") REFERENCES "blocks" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "block_links_slots_link" FOREIGN KEY ("slot_id") REFERENCES "slots" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Modify "block_versions" table
ALTER TABLE "block_versions" ADD CONSTRAINT "block_versions_blocks_versions" FOREIGN KEY ("block_id") REFERENCES "blocks" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "block_versions_users_accepted_block_versions" FOREIGN KEY ("accepted_by_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "kits" table
ALTER TABLE "kits" ADD CONSTRAINT "kits_teams_kits" FOREIGN KEY ("team_id") REFERENCES "teams" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Modify "kit_includes" table
ALTER TABLE "kit_includes" ADD CONSTRAINT "kit_includes_kits_included_by" FOREIGN KEY ("child_kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "kit_includes_kits_includes" FOREIGN KEY ("kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Modify "slots" table
ALTER TABLE "slots" ADD CONSTRAINT "slots_kits_origin_slots" FOREIGN KEY ("origin_kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "slots_kits_slots" FOREIGN KEY ("kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Modify "team_memberships" table
ALTER TABLE "team_memberships" ADD CONSTRAINT "team_memberships_teams_memberships" FOREIGN KEY ("team_id") REFERENCES "teams" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "team_memberships_users_memberships" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
