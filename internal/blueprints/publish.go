package blueprints

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/blueprintversion"
)

// Pusher builds and pushes a blueprint version's kit image and returns the
// repository (as people pull it) and the manifest digest (internal/publish).
type Pusher func(ctx context.Context, slug string, version int, yaml string) (repository, digest string, err error)

// Version is one published version.
type Version struct {
	Version     int       `json:"version"`
	Repository  string    `json:"repository"`
	Digest      string    `json:"digest"`
	PublishedAt time.Time `json:"publishedAt"`
	PublishedBy string    `json:"publishedBy,omitempty"`
	// Unchanged is set when Publish found nothing new since this version.
	Unchanged bool `json:"unchanged,omitempty"`
}

// Ref is the pinned image reference.
func (v Version) Ref() string { return v.Repository + "@" + v.Digest }

func versionOf(r *ent.BlueprintVersion) *Version {
	return &Version{Version: r.Version, Repository: r.Repository, Digest: r.Digest, PublishedAt: r.CreatedAt, PublishedBy: r.PublishedBy}
}

// LatestVersion is the newest published version, or nil.
func (s *Service) LatestVersion(ctx context.Context, blueprintID string) (*Version, error) {
	r, err := s.Ent.BlueprintVersion.Query().Where(blueprintversion.BlueprintID(blueprintID)).
		Order(ent.Desc(blueprintversion.FieldVersion)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return versionOf(r), nil
}

// ErrNotValid means the blueprint doesn't compile to a valid kit yet.
var ErrNotValid = errors.New("this blueprint isn't a valid kit yet")

// Publish compiles the blueprint as its next version and pushes it. When
// nothing changed since the last version it pushes nothing and returns
// that version with Unchanged set.
func (s *Service) Publish(ctx context.Context, blueprintID, userID string, push Pusher) (*Version, error) {
	latest, err := s.LatestVersion(ctx, blueprintID)
	if err != nil {
		return nil, err
	}
	next := 1
	if latest != nil {
		next = latest.Version + 1
		// Compiled under the last version's number, an unchanged blueprint
		// gives the same YAML byte for byte.
		same, err := s.load(ctx, blueprintID, nil, latest.Version)
		if err != nil {
			return nil, err
		}
		prev, err := s.Ent.BlueprintVersion.Query().Where(blueprintversion.BlueprintID(blueprintID), blueprintversion.Version(latest.Version)).Only(ctx)
		if err != nil {
			return nil, err
		}
		if same.Compiled.Valid && same.Compiled.YAML == prev.Yaml {
			latest.Unchanged = true
			return latest, nil
		}
	}
	l, err := s.load(ctx, blueprintID, nil, next)
	if err != nil {
		return nil, err
	}
	if !l.Compiled.Valid {
		return nil, fmt.Errorf("%w: %s", ErrNotValid, l.Compiled.Error)
	}
	repo, digest, err := push(ctx, l.Blueprint.Slug, next, l.Compiled.YAML)
	if err != nil {
		return nil, err
	}
	q := s.Ent.BlueprintVersion.Create().SetBlueprintID(blueprintID).SetVersion(next).
		SetRepository(repo).SetDigest(digest).SetYaml(l.Compiled.YAML)
	if userID != "" {
		q.SetPublishedBy(userID)
	}
	r, err := q.Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, blueprintID, "publish", "", fmt.Sprintf("Published v%d (%s)", next, digest[:min(len(digest), 19)])); err != nil {
		return nil, err
	}
	return versionOf(r), nil
}
