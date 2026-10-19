-- Create "role_assignments" table
CREATE TABLE "role_assignments" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "role_key" character varying NOT NULL, "principal_type" character varying NOT NULL, "principal_id" character varying NOT NULL, PRIMARY KEY ("id"));
-- Create index "roleassignment_principal_type_principal_id_role_key" to table: "role_assignments"
CREATE UNIQUE INDEX "roleassignment_principal_type_principal_id_role_key" ON "role_assignments" ("principal_type", "principal_id", "role_key");
