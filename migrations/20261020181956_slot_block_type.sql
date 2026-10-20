-- Modify "slots" table
ALTER TABLE "slots" ADD COLUMN "block_type" character varying NOT NULL DEFAULT '';
-- Existing slots: the linked block's type, else the type their section used to imply.
UPDATE "slots" SET "block_type" = "blocks"."type" FROM "block_links", "blocks"
  WHERE "block_links"."slot_id" = "slots"."id" AND "blocks"."id" = "block_links"."block_id";
UPDATE "slots" SET "block_type" = CASE "section" WHEN 'network' THEN 'network-rules' WHEN 'credentials' THEN 'credential' ELSE 'raw-capability' END
  WHERE "block_type" = '';
