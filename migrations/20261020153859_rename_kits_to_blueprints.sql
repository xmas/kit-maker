-- Rename kits to blueprints (decision kit-maker-hcw). Hand-written so the rows
-- survive: Atlas would drop and re-create the columns. Blueprint IDs move from
-- the kit_ prefix to bp_.

-- Drop the foreign keys that point at kits (re-added below under new names).
ALTER TABLE "kit_includes" DROP CONSTRAINT "kit_includes_kits_included_by", DROP CONSTRAINT "kit_includes_kits_includes";
ALTER TABLE "slots" DROP CONSTRAINT "slots_kits_origin_slots", DROP CONSTRAINT "slots_kits_slots";
ALTER TABLE "kit_revisions" DROP CONSTRAINT "kit_revisions_kits_revisions";
ALTER TABLE "checks" DROP CONSTRAINT "checks_kits_checks";
ALTER TABLE "goals" DROP CONSTRAINT "goals_kits_goal";

-- Tables, primary keys and the constraints that stay.
ALTER TABLE "kits" RENAME TO "blueprints";
ALTER INDEX "kits_pkey" RENAME TO "blueprints_pkey";
ALTER TABLE "blueprints" RENAME CONSTRAINT "kits_teams_kits" TO "blueprints_teams_blueprints";
ALTER INDEX "kit_team_id_slug" RENAME TO "blueprint_team_id_slug";

ALTER TABLE "kit_includes" RENAME TO "blueprint_includes";
ALTER INDEX "kit_includes_pkey" RENAME TO "blueprint_includes_pkey";
ALTER TABLE "blueprint_includes" RENAME COLUMN "kit_id" TO "blueprint_id";
ALTER TABLE "blueprint_includes" RENAME COLUMN "child_kit_id" TO "child_blueprint_id";
ALTER TABLE "blueprint_includes" RENAME COLUMN "pinned_blueprint_id" TO "pinned_version_id";
ALTER INDEX "kitinclude_child_kit_id" RENAME TO "blueprintinclude_child_blueprint_id";
ALTER INDEX "kitinclude_kit_id_child_kit_id" RENAME TO "blueprintinclude_blueprint_id_child_blueprint_id";

ALTER TABLE "kit_revisions" RENAME TO "blueprint_revisions";
ALTER INDEX "kit_revisions_pkey" RENAME TO "blueprint_revisions_pkey";
ALTER TABLE "blueprint_revisions" RENAME COLUMN "kit_id" TO "blueprint_id";
ALTER TABLE "blueprint_revisions" RENAME CONSTRAINT "kit_revisions_users_kit_revisions" TO "blueprint_revisions_users_blueprint_revisions";
ALTER INDEX "kitrevision_kit_id_seq" RENAME TO "blueprintrevision_blueprint_id_seq";
ALTER INDEX "kitrevision_kit_id_slot_key" RENAME TO "blueprintrevision_blueprint_id_slot_key";

ALTER TABLE "slots" RENAME COLUMN "kit_id" TO "blueprint_id";
ALTER TABLE "slots" RENAME COLUMN "origin_kit_id" TO "origin_blueprint_id";
ALTER INDEX "slot_kit_id_key" RENAME TO "slot_blueprint_id_key";

ALTER TABLE "checks" RENAME COLUMN "kit_id" TO "blueprint_id";
ALTER INDEX "check_kit_id_position" RENAME TO "check_blueprint_id_position";

ALTER TABLE "goals" RENAME COLUMN "kit_id" TO "blueprint_id";
ALTER INDEX "goals_kit_id_key" RENAME TO "goals_blueprint_id_key";

ALTER TABLE "ai_calls" RENAME COLUMN "kit_id" TO "blueprint_id";

-- IDs: kit_<ULID> becomes bp_<ULID> everywhere it is stored.
UPDATE "blueprints" SET "id" = 'bp_' || substr("id", 5) WHERE "id" LIKE 'kit\_%';
UPDATE "blueprint_includes" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';
UPDATE "blueprint_includes" SET "child_blueprint_id" = 'bp_' || substr("child_blueprint_id", 5) WHERE "child_blueprint_id" LIKE 'kit\_%';
UPDATE "blueprint_revisions" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';
UPDATE "slots" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';
UPDATE "slots" SET "origin_blueprint_id" = 'bp_' || substr("origin_blueprint_id", 5) WHERE "origin_blueprint_id" LIKE 'kit\_%';
UPDATE "checks" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';
UPDATE "goals" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';
UPDATE "ai_calls" SET "blueprint_id" = 'bp_' || substr("blueprint_id", 5) WHERE "blueprint_id" LIKE 'kit\_%';

-- Foreign keys under the names Ent gives them.
ALTER TABLE "blueprint_includes" ADD CONSTRAINT "blueprint_includes_blueprints_included_by" FOREIGN KEY ("child_blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "blueprint_includes_blueprints_includes" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
ALTER TABLE "blueprint_revisions" ADD CONSTRAINT "blueprint_revisions_blueprints_revisions" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
ALTER TABLE "slots" ADD CONSTRAINT "slots_blueprints_origin_slots" FOREIGN KEY ("origin_blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "slots_blueprints_slots" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
ALTER TABLE "checks" ADD CONSTRAINT "checks_blueprints_checks" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
ALTER TABLE "goals" ADD CONSTRAINT "goals_blueprints_goal" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION;
