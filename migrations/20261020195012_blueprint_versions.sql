-- Create "blueprint_versions" table
CREATE TABLE "blueprint_versions" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "version" bigint NOT NULL, "repository" character varying NOT NULL, "digest" character varying NOT NULL, "yaml" text NOT NULL, "published_by" character varying NULL, "blueprint_id" character varying NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "blueprint_versions_blueprints_versions" FOREIGN KEY ("blueprint_id") REFERENCES "blueprints" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "blueprintversion_blueprint_id_version" to table: "blueprint_versions"
CREATE UNIQUE INDEX "blueprintversion_blueprint_id_version" ON "blueprint_versions" ("blueprint_id", "version");
