# Atlas project config. Migrations are generated from the Ent schema and
# applied at runtime by `kitmaker migrate` (they're embedded in the binary).
#
#   make migrate-diff name=add_widgets   # atlas migrate diff --env local
#   make migrate-lint                    # blocks destructive changes in CI

env "local" {
  src = "ent://internal/ent/schema"
  # Throwaway dev database Atlas uses to compute diffs (needs Docker).
  dev = "docker://pgvector/pgvector/pg16/dev?search_path=public"

  migration {
    dir = "file://migrations"
  }

  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }

  lint {
    destructive {
      error = true
    }
  }
}
