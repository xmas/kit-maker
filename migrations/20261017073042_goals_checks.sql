-- Modify "kits" table
ALTER TABLE "kits" ADD COLUMN "description" text NOT NULL DEFAULT '';
-- Create "checks" table
CREATE TABLE "checks" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "label" character varying NOT NULL, "type" character varying NOT NULL, "spec" jsonb NOT NULL, "expect" character varying NOT NULL, "position" bigint NOT NULL DEFAULT 0, "authored_by" character varying NOT NULL DEFAULT 'user', "kit_id" character varying NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "checks_kits_checks" FOREIGN KEY ("kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "check_kit_id_position" to table: "checks"
CREATE INDEX "check_kit_id_position" ON "checks" ("kit_id", "position");
-- Create "goals" table
CREATE TABLE "goals" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "statement" text NOT NULL, "sources" jsonb NULL, "kit_id" character varying NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "goals_kits_goal" FOREIGN KEY ("kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "goals_kit_id_key" to table: "goals"
CREATE UNIQUE INDEX "goals_kit_id_key" ON "goals" ("kit_id");
