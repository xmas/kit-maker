package dbmigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ariga.io/atlas/sql/migrate"
)

// The revisions table uses the same location and columns as the Atlas CLI,
// so `atlas migrate status --url …` reads what `kitmaker migrate` wrote.
const (
	revisionSchema = "atlas_schema_revisions"
	revisionTable  = "atlas_schema_revisions"
)

const createRevisionTable = `
CREATE SCHEMA IF NOT EXISTS ` + revisionSchema + `;
CREATE TABLE IF NOT EXISTS ` + revisionSchema + `.` + revisionTable + ` (
	version          character varying NOT NULL PRIMARY KEY,
	description      character varying NOT NULL,
	type             bigint NOT NULL DEFAULT 2,
	applied          bigint NOT NULL DEFAULT 0,
	total            bigint NOT NULL DEFAULT 0,
	executed_at      timestamptz NOT NULL,
	execution_time   bigint NOT NULL,
	error            text NULL,
	error_stmt       text NULL,
	hash             character varying NOT NULL,
	partial_hashes   jsonb NULL,
	operator_version character varying NOT NULL
)`

// revisions implements migrate.RevisionReadWriter on database/sql.
type revisions struct {
	db *sql.DB
}

var _ migrate.RevisionReadWriter = (*revisions)(nil)

func (r *revisions) init(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, createRevisionTable)
	return err
}

func (r *revisions) Ident() *migrate.TableIdent {
	return &migrate.TableIdent{Schema: revisionSchema, Name: revisionTable}
}

const revisionColumns = `version, description, type, applied, total, executed_at, execution_time,
	coalesce(error, ''), coalesce(error_stmt, ''), hash, partial_hashes, operator_version`

func (r *revisions) ReadRevisions(ctx context.Context) ([]*migrate.Revision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+revisionColumns+` FROM `+revisionSchema+`.`+revisionTable+` ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revs []*migrate.Revision
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		revs = append(revs, rev)
	}
	return revs, rows.Err()
}

func (r *revisions) ReadRevision(ctx context.Context, version string) (*migrate.Revision, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+revisionColumns+` FROM `+revisionSchema+`.`+revisionTable+` WHERE version = $1`, version)
	rev, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, migrate.ErrRevisionNotExist
	}
	return rev, err
}

func (r *revisions) WriteRevision(ctx context.Context, rev *migrate.Revision) error {
	partial, err := json.Marshal(rev.PartialHashes)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO `+revisionSchema+`.`+revisionTable+`
	(version, description, type, applied, total, executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), nullif($9, ''), $10, $11, $12)
ON CONFLICT (version) DO UPDATE SET
	description = excluded.description, type = excluded.type, applied = excluded.applied,
	total = excluded.total, executed_at = excluded.executed_at, execution_time = excluded.execution_time,
	error = excluded.error, error_stmt = excluded.error_stmt, hash = excluded.hash,
	partial_hashes = excluded.partial_hashes, operator_version = excluded.operator_version`,
		rev.Version, rev.Description, int64(rev.Type), rev.Applied, rev.Total, rev.ExecutedAt,
		int64(rev.ExecutionTime), rev.Error, rev.ErrorStmt, rev.Hash, partial, rev.OperatorVersion)
	return err
}

func (r *revisions) DeleteRevision(ctx context.Context, version string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM `+revisionSchema+`.`+revisionTable+` WHERE version = $1`, version)
	return err
}

type scanner interface{ Scan(...any) error }

func scanRevision(s scanner) (*migrate.Revision, error) {
	var (
		rev      migrate.Revision
		typ      int64
		execTime int64
		partial  []byte
	)
	err := s.Scan(&rev.Version, &rev.Description, &typ, &rev.Applied, &rev.Total, &rev.ExecutedAt,
		&execTime, &rev.Error, &rev.ErrorStmt, &rev.Hash, &partial, &rev.OperatorVersion)
	if err != nil {
		return nil, err
	}
	rev.Type = migrate.RevisionType(typ)
	rev.ExecutionTime = time.Duration(execTime)
	if len(partial) > 0 {
		if err := json.Unmarshal(partial, &rev.PartialHashes); err != nil {
			return nil, fmt.Errorf("decode partial hashes for %s: %w", rev.Version, err)
		}
	}
	return &rev, nil
}
