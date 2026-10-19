-- Create "ai_calls" table
CREATE TABLE "ai_calls" ("id" character varying NOT NULL, "created_at" timestamptz NOT NULL, "team_id" character varying NULL, "user_id" character varying NULL, "kit_id" character varying NULL, "route" character varying NOT NULL, "provider" character varying NOT NULL, "model" character varying NULL, "input_tokens" bigint NOT NULL DEFAULT 0, "output_tokens" bigint NOT NULL DEFAULT 0, "cache_read_tokens" bigint NOT NULL DEFAULT 0, "cache_write_tokens" bigint NOT NULL DEFAULT 0, "status" character varying NOT NULL, "error" text NULL, "duration_ms" bigint NOT NULL DEFAULT 0, PRIMARY KEY ("id"));
-- Create index "aicall_team_id_created_at" to table: "ai_calls"
CREATE INDEX "aicall_team_id_created_at" ON "ai_calls" ("team_id", "created_at");
