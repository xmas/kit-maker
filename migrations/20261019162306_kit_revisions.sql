-- Create "kit_revisions" table
CREATE TABLE "kit_revisions" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "seq" bigint NOT NULL, "action" character varying NOT NULL, "slot_key" character varying NULL, "summary" text NOT NULL, "snapshot" jsonb NOT NULL, "yaml" text NOT NULL DEFAULT '', "kit_id" character varying NOT NULL, "actor_user_id" character varying NULL, PRIMARY KEY ("id"), CONSTRAINT "kit_revisions_kits_revisions" FOREIGN KEY ("kit_id") REFERENCES "kits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT "kit_revisions_users_kit_revisions" FOREIGN KEY ("actor_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "kitrevision_kit_id_seq" to table: "kit_revisions"
CREATE UNIQUE INDEX "kitrevision_kit_id_seq" ON "kit_revisions" ("kit_id", "seq");
-- Create index "kitrevision_kit_id_slot_key" to table: "kit_revisions"
CREATE INDEX "kitrevision_kit_id_slot_key" ON "kit_revisions" ("kit_id", "slot_key");
